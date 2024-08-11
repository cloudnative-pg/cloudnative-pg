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

package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/certs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/resources/instance"
)

// PublicationReconciler reconciles a Publication object
type PublicationReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	instanceClient instance.Client
}

// publicationReconciliationInterval is the time between the
// publication reconciliation loop failures
const publicationReconciliationInterval = 30 * time.Second

// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=publications,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=publications/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=publications/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Publication object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.18.4/pkg/reconcile
func (r *PublicationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	contextLogger, ctx := log.SetupLogger(ctx)

	contextLogger.Debug("Reconciliation loop start")
	defer func() {
		contextLogger.Debug("Reconciliation loop end")
	}()

	// Get the publication object
	var publication apiv1.Publication
	if err := r.Client.Get(ctx, client.ObjectKey{
		Namespace: req.Namespace,
		Name:      req.Name,
	}, &publication); err != nil {
		// This is a deleted object, there's nothing
		// to do.
		if apierrs.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// If everything is reconciled, we're done here
	if publication.Generation == publication.Status.ObservedGeneration {
		return ctrl.Result{}, nil
	}

	// Let's get the corresponding Cluster object
	var cluster apiv1.Cluster
	if err := r.Client.Get(ctx, client.ObjectKey{
		Namespace: publication.Namespace,
		Name:      publication.Spec.ClusterRef.Name,
	}, &cluster); err != nil {
		if apierrs.IsNotFound(err) {
			return r.failedReconciliation(
				ctx,
				&publication,
				errClusterNotPresent,
			)
		}
		return ctrl.Result{}, err
	}

	if cluster.IsReplica() {
		return r.failedReconciliation(
			ctx,
			&publication,
			errClusterIsReplica,
		)
	}

	if cluster.Status.CurrentPrimary != cluster.Status.TargetPrimary ||
		len(cluster.Status.CurrentPrimary) == 0 {
		return r.failedReconciliation(
			ctx,
			&publication,
			errClusterPrimaryNotStable,
		)
	}

	// Get the reference to the primary pod
	var primaryPod corev1.Pod
	if err := r.Client.Get(ctx, client.ObjectKey{
		Namespace: publication.Namespace,
		Name:      cluster.Status.CurrentPrimary,
	}, &primaryPod); err != nil {
		return r.failedReconciliation(
			ctx,
			&publication,
			fmt.Errorf("while getting primary pod: %w", err),
		)
	}

	// Store in the context the TLS configuration required communicating with the Pods
	ctx, err := certs.NewTLSConfigForContext(
		ctx,
		r.Client,
		cluster.GetServerCASecretObjectKey(),
	)
	if err != nil {
		return ctrl.Result{}, err
	}

	publicationRequest := instance.PgPublication{
		Name:       publication.Spec.Name,
		Owner:      publication.Spec.Owner,
		Parameters: publication.Spec.Parameters,
		Target:     toPublicationTarget(publication.Spec.Target),
	}

	if err := r.instanceClient.PostPublication(
		ctx,
		&primaryPod,
		publication.Spec.DbName,
		publicationRequest,
	); err != nil {
		return r.failedReconciliation(
			ctx,
			&publication,
			err,
		)
	}

	return r.succeededRenconciliation(
		ctx,
		&publication,
	)
}

func toPublicationTarget(publicationTarget apiv1.PublicationTarget) instance.PgPublicationTarget {
	return instance.PgPublicationTarget{
		AllTables: toPublicationAllTables(publicationTarget.AllTables),
		Objects:   toPublicationTargetObject(publicationTarget.Objects),
	}
}

func toPublicationTargetObject(publicationTargetObject []apiv1.PublicationTargetObject) []instance.PgPublicationTargetObject {
	result := make([]instance.PgPublicationTargetObject, len(publicationTargetObject))
	for i := range publicationTargetObject {
		result[i] = instance.PgPublicationTargetObject{
			Schema:          publicationTargetObject[i].Schema,
			TableExpression: publicationTargetObject[i].TableExpression,
		}
	}
	return result
}

func toPublicationAllTables(publicationTargetAllTables *apiv1.PublicationTargetAllTables) *instance.PgPublicationTargetAllTables {
	if publicationTargetAllTables == nil {
		return nil
	}

	return &instance.PgPublicationTargetAllTables{}
}

// NewPublicationReconciler creates a new publication reconciler
func NewPublicationReconciler(
	mgr manager.Manager,
) *PublicationReconciler {
	return &PublicationReconciler{
		Client:         mgr.GetClient(),
		instanceClient: instance.NewStatusClient(),
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *PublicationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&apiv1.Publication{}).
		Complete(r)
}

// failedReconciliation marks the reconciliation as failed and logs the corresponding error
func (r *PublicationReconciler) failedReconciliation(
	ctx context.Context,
	publication *apiv1.Publication,
	err error,
) (ctrl.Result, error) {
	oldPublication := publication.DeepCopy()
	publication.Status.Error = err.Error()
	publication.Status.Ready = false

	var statusError *instance.StatusError
	if errors.As(err, &statusError) {
		// The body line of the instance manager contain the human
		// readable error
		publication.Status.Error = statusError.Body
	}

	if err := r.Client.Status().Patch(ctx, publication, client.MergeFrom(oldPublication)); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{
		RequeueAfter: databaseReconciliationInterval,
	}, nil
}

// succeededReconciliation marks the reconciliation as succeeded
func (r *PublicationReconciler) succeededRenconciliation(
	ctx context.Context,
	publication *apiv1.Publication,
) (ctrl.Result, error) {
	oldPublication := publication.DeepCopy()
	publication.Status.Error = ""
	publication.Status.Ready = true
	publication.Status.ObservedGeneration = publication.Generation

	if err := r.Client.Status().Patch(ctx, publication, client.MergeFrom(oldPublication)); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{
		RequeueAfter: publicationReconciliationInterval,
	}, nil
}
