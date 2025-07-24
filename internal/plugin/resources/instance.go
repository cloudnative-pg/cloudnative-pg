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

package resources

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/exec"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/webserver/client/remote"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/url"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// https://www.postgresql.org/docs/current/app-pg-ctl.html
type pgCtlStatusExitCode int

const (
	pgCtlStatusStopped               pgCtlStatusExitCode = 3
	pgCtlStatusNoAccessibleDirectory pgCtlStatusExitCode = 4
)

// GetInstancePods gets all the pods belonging to a given cluster
// returns an array with all the instances, the primary instance and any error encountered.
func GetInstancePods(ctx context.Context, clusterName string) ([]corev1.Pod, corev1.Pod, error) {
	var pods corev1.PodList
	if err := plugin.Client.List(ctx, &pods, client.InNamespace(plugin.Namespace)); err != nil {
		return nil, corev1.Pod{}, err
	}

	var managedPods []corev1.Pod
	var primaryPod corev1.Pod
	for idx := range pods.Items {
		for _, owner := range pods.Items[idx].OwnerReferences {
			if owner.Kind == apiv1.ClusterKind && owner.Name == clusterName {
				managedPods = append(managedPods, pods.Items[idx])
				if specs.IsPodPrimary(pods.Items[idx]) {
					primaryPod = pods.Items[idx]
				}
			}
		}
	}
	return managedPods, primaryPod, nil
}

// ExtractInstancesStatus extracts the instance status from the given pod list
func ExtractInstancesStatus(
	ctx context.Context,
	cluster *apiv1.Cluster,
	config *rest.Config,
	filteredPods []corev1.Pod,
) (postgres.PostgresqlStatusList, []error) {
	result := postgres.PostgresqlStatusList{
		IsReplicaCluster: cluster.IsReplica(),
		CurrentPrimary:   cluster.Status.CurrentPrimary,
	}
	var errs []error

	for idx := range filteredPods {
		instanceStatus := getInstanceStatusFromPod(
			ctx, config, filteredPods[idx])
		result.Items = append(result.Items, instanceStatus)
		if instanceStatus.Error != nil {
			errs = append(errs, instanceStatus.Error)
		}
	}

	return result, errs
}

func getInstanceStatusFromPod(
	ctx context.Context,
	config *rest.Config,
	pod corev1.Pod,
) postgres.PostgresqlStatus {
	var result postgres.PostgresqlStatus

	statusResult, err := kubernetes.NewForConfigOrDie(config).
		CoreV1().
		Pods(pod.Namespace).
		ProxyGet(
			remote.GetStatusSchemeFromPod(&pod).ToString(),
			pod.Name,
			strconv.Itoa(int(url.StatusPort)),
			url.PathPgStatus,
			nil,
		).
		DoRaw(ctx)
	if err != nil {
		result.AddPod(pod)
		result.Error = fmt.Errorf(
			"failed to get status by proxying to the pod, you might lack permissions to get pods/proxy: %w",
			err)
		return result
	}

	if err := json.Unmarshal(statusResult, &result); err != nil {
		result.Error = fmt.Errorf("can't parse pod output")
	}

	result.AddPod(pod)

	return result
}

// IsInstanceRunning returns a boolean indicating if the given instance is running and any error encountered
func IsInstanceRunning(
	ctx context.Context,
	pod corev1.Pod,
) (bool, error) {
	contextLogger := log.FromContext(ctx).WithName("plugin.IsInstanceRunning")
	timeout := time.Second * 10
	clientInterface := kubernetes.NewForConfigOrDie(plugin.Config)
	stdout, stderr, err := utils.ExecCommand(
		ctx,
		clientInterface,
		plugin.Config,
		pod,
		specs.PostgresContainerName,
		&timeout,
		"pg_ctl", "status")
	if err == nil {
		return true, nil
	}

	var codeExitError exec.CodeExitError
	if errors.As(err, &codeExitError) {
		switch pgCtlStatusExitCode(codeExitError.Code) {
		case pgCtlStatusStopped:
			return false, nil
		case pgCtlStatusNoAccessibleDirectory:
			return false, fmt.Errorf("could not check instance status: no accessible data directory")
		}
	}

	contextLogger.Debug("encountered an error while getting instance status",
		"stdout", stdout,
		"stderr", stderr,
		"err", err,
	)

	return false, err
}
