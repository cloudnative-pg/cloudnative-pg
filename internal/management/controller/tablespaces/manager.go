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
		Complete(r)
}

// GetCluster gets the managed cluster through the client
func (r *TablespaceReconciler) GetCluster(ctx context.Context) (*apiv1.Cluster, error) {
	var cluster apiv1.Cluster
	err := r.GetClient().Get(ctx,
		types.NamespacedName{
			Namespace: r.instance.Namespace,
			Name:      r.instance.ClusterName,
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
