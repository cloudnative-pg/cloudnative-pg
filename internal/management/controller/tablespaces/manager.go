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

package tablespaces

import (
	"context"

	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
)

// TablespaceReconciler is a Kubernetes controller that ensures Tablespaces
// are created in Postgres
type TablespaceReconciler struct {
	instance *postgres.Instance
	client   client.Client
}

// NewTablespaceReconciler creates a new TablespaceReconciler
func NewTablespaceReconciler(instance *postgres.Instance, client client.Client) *TablespaceReconciler {
	controller := &TablespaceReconciler{
		instance: instance,
		client:   client,
	}
	return controller
}

// SetupWithManager sets up the controller with the Manager.
func (r *TablespaceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&apiv1.Cluster{}).
		Named("instance-tablespaces").
		Complete(r)
}

// GetCluster gets the managed cluster through the client
func (r *TablespaceReconciler) GetCluster(ctx context.Context) (*apiv1.Cluster, error) {
	var cluster apiv1.Cluster
	err := r.GetClient().Get(ctx,
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

// GetClient returns the dynamic client that is being used for a certain reconciler
func (r *TablespaceReconciler) GetClient() client.Client {
	return r.client
}

// Instance returns the PostgreSQL instance that this reconciler is working on
func (r *TablespaceReconciler) Instance() *postgres.Instance {
	return r.instance
}
