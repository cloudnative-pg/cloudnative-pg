package controller

import (
	"context"

	g "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type postgresObjectManager interface {
	client.Object
	GetStatusApplied() *bool
	GetStatusMessage() string
	SetObservedGeneration(gen int64)
}

type postgresReconciliationTester[T postgresObjectManager] struct {
	cli                       client.Client
	reconcileFunc             func(ctx context.Context, req ctrl.Request) (ctrl.Result, error)
	postgresExpectations      func()
	updatedObjectExpectations func(newObj T)
}

func (pr *postgresReconciliationTester[T]) setPostgresExpectations(
	postgresExpectations func(),
) *postgresReconciliationTester[T] {
	pr.postgresExpectations = postgresExpectations
	return pr
}

func (pr *postgresReconciliationTester[T]) setUpdatedObjectExpectations(
	updatedObjectExpectations func(newObj T),
) *postgresReconciliationTester[T] {
	pr.updatedObjectExpectations = updatedObjectExpectations
	return pr
}

func (pr *postgresReconciliationTester[T]) assert(
	ctx context.Context,
	obj T,
) {
	g.Expect(obj.GetFinalizers()).To(g.BeEmpty())

	if pr.postgresExpectations != nil {
		pr.postgresExpectations()
	}

	// Reconcile and get the updated object
	_, err := pr.reconcileFunc(ctx, ctrl.Request{NamespacedName: types.NamespacedName{
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
	}})
	g.Expect(err).ToNot(g.HaveOccurred())

	newObj := obj.DeepCopyObject().(T)
	err = pr.cli.Get(ctx, client.ObjectKey{
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
	}, newObj)
	g.Expect(err).ToNot(g.HaveOccurred())

	if pr.updatedObjectExpectations != nil {
		pr.updatedObjectExpectations(newObj)
	}
}

// assertObjectWasReconciled reconciles the object and retrieves its update
// from kubernetes
//
// NOTE: in the `newObj` argument, simply pass an empty struct of the type T
// you are testing (e.g. &apiv1.Database{}), as this will be populated in the
// kubernetes Get() calls
func assertObjectWasReconciled[T postgresObjectManager](
	ctx context.Context,
	r reconcile.Reconciler,
	obj T,
	newObj T,
	fakeClient client.Client,
	postgresExpectations func(),
	updatedObjectExpectations func(newObj T),
) {
	g.Expect(obj.GetFinalizers()).To(g.BeEmpty())

	postgresExpectations()

	// Reconcile and get the updated object
	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
	}})
	g.Expect(err).ToNot(g.HaveOccurred())

	err = fakeClient.Get(ctx, client.ObjectKey{
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
	}, newObj)
	g.Expect(err).ToNot(g.HaveOccurred())

	updatedObjectExpectations(newObj)
}

// assertObjectReconciledAfterDeletion goes through the whole lifetime of an object
//
//   - first reconciliation (creates finalizers)
//   - object gets Deleted in kubernetes
//   - a second reconciliation deletes the finalizers and *may* perform DROPs in Postgres
//
// NOTE: in the `newObj` argument, simply pass an empty struct of the type T
// you are testing (e.g. &apiv1.Database{}), as this will be populated in the
// kubernetes Get() calls
func assertObjectReconciledAfterDeletion[T postgresObjectManager](
	ctx context.Context,
	r reconcile.Reconciler,
	obj T,
	newObj T,
	fakeClient client.Client,
	postgresExpectations func(),
) {
	g.Expect(obj.GetFinalizers()).To(g.BeEmpty())

	postgresExpectations()

	// Reconcile and get the updated object
	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
	}})
	g.Expect(err).ToNot(g.HaveOccurred())

	err = fakeClient.Get(ctx, client.ObjectKey{
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
	}, newObj)
	g.Expect(err).ToNot(g.HaveOccurred())

	// plain successful reconciliation, finalizers have been created
	g.Expect(newObj.GetStatusApplied()).Should(g.HaveValue(g.BeTrue()))
	g.Expect(newObj.GetStatusMessage()).Should(g.BeEmpty())
	g.Expect(newObj.GetFinalizers()).NotTo(g.BeEmpty())

	// the next 2 lines are a hacky bit to make sure the next reconciler
	// call doesn't skip on account of Generation == ObservedGeneration.
	// See fake.Client known issues with `Generation`
	// https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/client/fake@v0.19.0#NewClientBuilder
	newObj.SetObservedGeneration(2)
	g.Expect(fakeClient.Status().Update(ctx, newObj)).To(g.Succeed())

	// We now look at the behavior when we delete the Database object
	g.Expect(fakeClient.Delete(ctx, obj)).To(g.Succeed())

	// the Database object is Deleted, but its finalizer prevents removal from
	// the API
	err = fakeClient.Get(ctx, client.ObjectKey{
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
	}, newObj)
	g.Expect(err).ToNot(g.HaveOccurred())
	g.Expect(newObj.GetDeletionTimestamp()).NotTo(g.BeZero())
	g.Expect(newObj.GetFinalizers()).NotTo(g.BeEmpty())

	// Reconcile and get the updated object
	_, err = r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
	}})
	g.Expect(err).ToNot(g.HaveOccurred())

	err = fakeClient.Get(ctx, client.ObjectKey{
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
	}, newObj)

	// verify object has been deleted
	g.Expect(err).To(g.HaveOccurred())
	g.Expect(apierrors.IsNotFound(err)).To(g.BeTrue())
}
