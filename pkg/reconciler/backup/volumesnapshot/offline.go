/*
Copyright © contributors to CloudNativePG, established as
CloudNativePG a Series of LF Projects, LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
*/

package volumesnapshot

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/strings/slices"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

type offlineExecutor struct {
	cli      client.Client
	recorder record.EventRecorder
}

func newOfflineExecutor(cli client.Client, recorder record.EventRecorder) *offlineExecutor {
	return &offlineExecutor{cli: cli, recorder: recorder}
}

func (o *offlineExecutor) finalize(
	ctx context.Context,
	cluster *apiv1.Cluster,
	backup *apiv1.Backup,
	targetPod *corev1.Pod,
) (*ctrl.Result, error) {
	return nil, EnsurePodIsUnfenced(ctx, o.cli, o.recorder, cluster, backup, targetPod)
}

func (o *offlineExecutor) prepare(
	ctx context.Context,
	cluster *apiv1.Cluster,
	backup *apiv1.Backup,
	targetPod *corev1.Pod,
) (*ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)

	// Handle cold snapshots
	contextLogger.Debug("Checking pre-requisites")
	if err := o.ensurePodIsFenced(ctx, cluster, backup, targetPod.Name); err != nil {
		return nil, err
	}

	if res, err := o.waitForPodToBeFenced(ctx, targetPod); res != nil || err != nil {
		return res, err
	}

	return nil, nil
}

// waitForPodToBeFenced waits for the target Pod to be shut down
func (o *offlineExecutor) waitForPodToBeFenced(
	ctx context.Context,
	targetPod *corev1.Pod,
) (*ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)

	var pod corev1.Pod
	err := o.cli.Get(ctx, types.NamespacedName{Name: targetPod.Name, Namespace: targetPod.Namespace}, &pod)
	if err != nil {
		return nil, err
	}
	ready := utils.IsPodReady(pod)
	if ready {
		contextLogger.Info("Waiting for target Pod to not be ready, retrying", "podName", targetPod.Name)
		return &ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}
	return nil, nil
}

// ensurePodIsFenced checks if the preconditions for the execution of this step are
// met or not. If they are not met, it will return an error
func (o *offlineExecutor) ensurePodIsFenced(
	ctx context.Context,
	cluster *apiv1.Cluster,
	backup *apiv1.Backup,
	targetPodName string,
) error {
	contextLogger := log.FromContext(ctx)

	fencedInstances, err := utils.GetFencedInstances(cluster.Annotations)
	if err != nil {
		return fmt.Errorf("could not check if cluster is fenced: %v", err)
	}

	if slices.Equal(fencedInstances.ToList(), []string{targetPodName}) {
		// We already requested the target Pod to be fenced
		return nil
	}

	if fencedInstances.Len() != 0 {
		return errors.New("cannot execute volume snapshot on a cluster that has fenced instances")
	}

	if targetPodName == cluster.Status.CurrentPrimary || targetPodName == cluster.Status.TargetPrimary {
		contextLogger.Warning(
			"Cold Snapshot Backup targets the primary. Primary will be fenced",
			"targetBackup", backup.Name, "targetPod", targetPodName,
		)
	}
	if err = utils.NewFencingMetadataExecutor(o.cli).
		AddFencing().
		ForInstance(targetPodName).
		Execute(ctx, client.ObjectKeyFromObject(cluster), cluster); err != nil {
		return err
	}

	// The list of fenced instances is empty, so we need to request
	// fencing for the target pod
	contextLogger.Info("Fencing Pod", "podName", targetPodName)
	o.recorder.Eventf(backup, "Normal", "FencePod",
		"Fencing Pod %v", targetPodName)

	return nil
}
