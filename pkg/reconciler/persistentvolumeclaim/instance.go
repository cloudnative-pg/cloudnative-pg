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
	return reconcileInstanceMissingPVCs(ctx, c, cluster, serial, nil)
}

func reconcileInstancesMissingPVCs(
	ctx context.Context,
	c client.Client,
	cluster *apiv1.Cluster,
	instances []corev1.Pod,
	pvcs []corev1.PersistentVolumeClaim,
) (ctrl.Result, error) {
	var shouldReconcile bool
	for idx := range instances {
		instance := instances[idx]
		serial, err := specs.GetNodeSerial(instance.ObjectMeta)
		if err != nil {
			return ctrl.Result{}, err
		}
		if err := reconcileInstanceMissingPVCs(ctx, c, cluster, serial, pvcs); err != nil {
			return ctrl.Result{}, err
		}
	}
	if shouldReconcile {
		return ctrl.Result{RequeueAfter: time.Second}, nil
	}

	return ctrl.Result{}, nil
}

// reconcileInstanceMissingPVCs reconcile an instance missing PVCs
func reconcileInstanceMissingPVCs(
	ctx context.Context,
	c client.Client,
	cluster *apiv1.Cluster,
	serial int,
	pvcs []corev1.PersistentVolumeClaim,
) error {
	instanceName := specs.GetInstanceName(cluster.Name, serial)
	expectedPVCs := getExpectedPVCs(cluster, instanceName)

	for _, expectedPVC := range expectedPVCs {
		if hasPVC(pvcs, expectedPVC.name) {
			continue
		}

		conf, err := getStorageConfiguration(expectedPVC.role, cluster)
		if err != nil {
			return err
		}

		if err := create(
			ctx,
			c,
			cluster,
			&CreateConfiguration{
				Status:     expectedPVC.expectedStatus,
				NodeSerial: serial,
				Role:       expectedPVC.role,
				Storage:    conf,
			},
		); err != nil {
			return err
		}
	}

	return nil
}
