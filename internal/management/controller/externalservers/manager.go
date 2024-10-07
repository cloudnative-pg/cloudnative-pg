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

package externalservers

import (
	"context"

	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
)

// Reconciler is a Kubernetes controller that ensures pgpass file for external servers is synchronized
type Reconciler struct {
	instance *postgres.Instance
	client   client.Client
}

// NewReconciler creates a new ExternalServerReconciler
func NewReconciler(instance *postgres.Instance, client client.Client) *Reconciler {
	controller := &Reconciler{
		instance: instance,
		client:   client,
	}
	return controller
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&apiv1.Cluster{}).
		Named("instance-external-server").
		Complete(r)
}

// getCluster gets the managed cluster through the client
func (r *Reconciler) getCluster(ctx context.Context) (*apiv1.Cluster, error) {
	var cluster apiv1.Cluster
	err := r.client.Get(ctx,
		types.NamespacedName{
			Namespace: r.instance.GetNamespaceName(),
			Name:      r.instance.GetClusterName(),
		},
		&cluster)
	if err != nil {
		return nil, err
	}

	return &cluster, nil
}
