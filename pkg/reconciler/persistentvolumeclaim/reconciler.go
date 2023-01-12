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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
)

// Reconcile reconciles the PVCs
func Reconcile(
	ctx context.Context,
	c client.Client,
	cluster *apiv1.Cluster,
	instances []corev1.Pod,
	pvcs []corev1.PersistentVolumeClaim,
) (ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)

	if err := reconcileOperatorLabels(ctx, c, instances, pvcs); err != nil {
		return ctrl.Result{}, fmt.Errorf("cannot update role labels on pvcs: %w", err)
	}

	if err := reconcileClusterLabels(ctx, c, cluster, pvcs); err != nil {
		return ctrl.Result{}, fmt.Errorf("cannot update cluster labels on pvcs: %w", err)
	}

	if err := reconcileClusterAnnotations(ctx, c, cluster, pvcs); err != nil {
		return ctrl.Result{}, fmt.Errorf("cannot update annotations on pvcs: %w", err)
	}

	if res, err := reconcileInstancesMissingPVCs(ctx, c, cluster, instances, pvcs); !res.IsZero() || err != nil {
		return res, err
	}

	if err := reconcileResourceRequests(ctx, c, cluster, pvcs); err != nil {
		if apierrs.IsConflict(err) {
			contextLogger.Debug("Conflict error while reconciling PVCs", "error", err)
			return ctrl.Result{Requeue: true}, nil
		}

		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}
