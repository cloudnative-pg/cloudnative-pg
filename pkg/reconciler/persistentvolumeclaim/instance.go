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

package persistentvolumeclaim

import (
	"context"
	"slices"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
)

// CreateInstancePVCs creates the expected pvcs for the instance and returns the
// UID of the PGDATA PVC (the one named after the instance itself), so callers
// building the bootstrap pod can scope its completion marker to this volume.
func CreateInstancePVCs(
	ctx context.Context,
	c client.Client,
	cluster *apiv1.Cluster,
	source *StorageSource,
	serial int,
) (types.UID, error) {
	reconciled, err := reconcileSingleInstanceMissingPVCs(ctx, c, cluster, serial, nil, source)
	return reconciled.pgDataUID, err
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
		reconciled, err := reconcileSingleInstanceMissingPVCs(ctx, c, cluster, serial, pvcs, nil)
		if err != nil {
			return ctrl.Result{}, err
		}
		if reconciled.created {
			result = ctrl.Result{RequeueAfter: time.Second}
		}
	}

	return result, nil
}

// missingPVCsReconciliation is the outcome of reconciling the PVCs an instance
// is missing.
type missingPVCsReconciliation struct {
	// created is true when at least one PVC was created, so the caller has to
	// requeue and let the next loop observe it.
	created bool

	// pgDataUID is the UID of the PGDATA PVC (the one named after the instance
	// itself) when this call created it, so a fresh bootstrap path can learn the
	// UID without a separate Get. It stays the zero UID when the PGDATA PVC was
	// not among the ones this call touched, for example when it was already
	// listed as present.
	pgDataUID types.UID
}

// reconcileSingleInstanceMissingPVCs creates the PVCs an instance is missing.
func reconcileSingleInstanceMissingPVCs(
	ctx context.Context,
	c client.Client,
	cluster *apiv1.Cluster,
	serial int,
	pvcs []corev1.PersistentVolumeClaim,
	source *StorageSource,
) (missingPVCsReconciliation, error) {
	var reconciliation missingPVCsReconciliation
	instanceName := specs.GetInstanceName(cluster.Name, serial)
	for _, expectedPVC := range getExpectedPVCsFromCluster(cluster, instanceName) {
		// Continue if the expectedPVC is in present in the current PVC list
		if slices.ContainsFunc(pvcs, func(pvc corev1.PersistentVolumeClaim) bool { return expectedPVC.name == pvc.Name }) {
			continue
		}

		conf, err := expectedPVC.calculator.GetStorageConfiguration(cluster)
		if err != nil {
			return missingPVCsReconciliation{}, err
		}

		pvcSource, err := expectedPVC.calculator.GetSource(source)
		if err != nil {
			return missingPVCsReconciliation{}, err
		}

		createConfiguration := expectedPVC.toCreateConfiguration(serial, conf, pvcSource)

		pvc, err := createIfNotExists(ctx, c, cluster, createConfiguration)
		if err != nil {
			return missingPVCsReconciliation{}, err
		}
		if expectedPVC.name == instanceName {
			reconciliation.pgDataUID = pvc.UID
		}

		reconciliation.created = true
	}

	return reconciliation, nil
}
