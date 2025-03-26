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

package tablespaces

import (
	"context"
	"database/sql"

	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
)

// instanceInterface represents the behavior required for the reconciler for
// instance operations
type instanceInterface interface {
	GetNamespaceName() string
	GetClusterName() string
	GetSuperUserDB() (*sql.DB, error)
	IsPrimary() (bool, error)
	IsReady() error
	CanCheckReadiness() bool
}

// TablespaceReconciler is a Kubernetes controller that ensures Tablespaces
// are created in Postgres
type TablespaceReconciler struct {
	instance       instanceInterface
	storageManager tablespaceStorageManager
	client         client.Client
}

// NewTablespaceReconciler creates a new TablespaceReconciler
func NewTablespaceReconciler(instance *postgres.Instance, client client.Client) *TablespaceReconciler {
	controller := &TablespaceReconciler{
		instance:       instance,
		client:         client,
		storageManager: instanceTablespaceStorageManager{},
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
