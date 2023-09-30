/*
Copyright The CloudNativePG Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package persistentvolumeclaim

import (
	"context"
	"time"

	"golang.org/x/exp/slices"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
)

// CreateInstancePVCs creates the expected pvcs for the instance
func CreateInstancePVCs(
	ctx context.Context,
	c client.Client,
	cluster *apiv1.Cluster,
	source *StorageSource,
	serial int,
) error {
	_, err := reconcileSingleInstanceMissingPVCs(ctx, c, cluster, serial, nil, source)
	return err
}

// reconcileMultipleInstancesMissingPVCs evaluate multiple instances that may miss some PVCs.
// It will work on the first instance where the PVCs should be reconciled, leaving the next
// ones for the other reconciliation loops.
func reconcileMultipleInstancesMissingPVCs(
	ctx context.Context,
	c client.Client,
	cluster *apiv1.Cluster,
	runningInstances []corev1.Pod,
	pvcs []corev1.PersistentVolumeClaim,
) (ctrl.Result, error) {
	var result ctrl.Result
	for idx := range runningInstances {
		serial, err := specs.GetNodeSerial(runningInstances[idx].ObjectMeta)
		if err != nil {
			return ctrl.Result{}, err
		}
		res, err := reconcileSingleInstanceMissingPVCs(ctx, c, cluster, serial, pvcs, nil)
		if err != nil {
			return res, err
		}
		if !res.IsZero() {
			result = res
		}
	}

	return result, nil
}

// reconcileSingleInstanceMissingPVCs reconcile an instance missing PVCs
func reconcileSingleInstanceMissingPVCs(
	ctx context.Context,
	c client.Client,
	cluster *apiv1.Cluster,
	serial int,
	pvcs []corev1.PersistentVolumeClaim,
	source *StorageSource,
) (ctrl.Result, error) {
	var shouldReconcile bool
	instanceName := specs.GetInstanceName(cluster.Name, serial)
	for _, expectedPVC := range getExpectedPVCsFromCluster(cluster, instanceName) {
		if slices.ContainsFunc(pvcs, func(pvc corev1.PersistentVolumeClaim) bool { return expectedPVC.name == pvc.Name }) {
			continue
		}

		conf, err := getStorageConfiguration(cluster, expectedPVC.role)
		if err != nil {
			return ctrl.Result{}, err
		}

		pvcSource, err := source.ForRole(expectedPVC.role)
		if err != nil {
			return ctrl.Result{}, err
		}

		createConfiguration := expectedPVC.toCreateConfiguration(serial, conf, pvcSource)

		if err := createIfNotExists(ctx, c, cluster, createConfiguration); err != nil {
			return ctrl.Result{}, err
		}

		shouldReconcile = true
	}

	if shouldReconcile {
		return ctrl.Result{RequeueAfter: time.Second}, nil
	}

	return ctrl.Result{}, nil
}
