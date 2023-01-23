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
	serial int,
) error {
	_, err := reconcileInstanceMissingPVCs(ctx, c, cluster, serial, nil)
	return err
}

func reconcileInstancesMissingPVCs(
	ctx context.Context,
	c client.Client,
	cluster *apiv1.Cluster,
	instances []corev1.Pod,
	pvcs []corev1.PersistentVolumeClaim,
) (ctrl.Result, error) {
	var result ctrl.Result
	for idx := range instances {
		serial, err := specs.GetNodeSerial(instances[idx].ObjectMeta)
		if err != nil {
			return ctrl.Result{}, err
		}
		res, err := reconcileInstanceMissingPVCs(ctx, c, cluster, serial, pvcs)
		if err != nil {
			return res, err
		}
		if !res.IsZero() {
			result = res
		}
	}

	return result, nil
}

// reconcileInstanceMissingPVCs reconcile an instance missing PVCs
func reconcileInstanceMissingPVCs(
	ctx context.Context,
	c client.Client,
	cluster *apiv1.Cluster,
	serial int,
	pvcs []corev1.PersistentVolumeClaim,
) (ctrl.Result, error) {
	var shouldReconcile bool
	instanceName := specs.GetInstanceName(cluster.Name, serial)
	for _, expectedPVC := range getExpectedPVCs(cluster, instanceName) {
		if slices.ContainsFunc(pvcs, func(pvc corev1.PersistentVolumeClaim) bool { return expectedPVC.name == pvc.Name }) {
			continue
		}

		conf, err := getStorageConfiguration(cluster, expectedPVC.role)
		if err != nil {
			return ctrl.Result{}, err
		}

		createConfiguration := expectedPVC.toCreateConfiguration(serial, conf)

		if err := create(ctx, c, cluster, createConfiguration); err != nil {
			return ctrl.Result{}, err
		}
		shouldReconcile = true
	}

	if shouldReconcile {
		return ctrl.Result{RequeueAfter: time.Second}, nil
	}

	return ctrl.Result{}, nil
}
