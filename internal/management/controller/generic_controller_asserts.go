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
	GetStatusApplied() *bool
	GetStatusMessage() string
	SetObservedGeneration(gen int64)
	GetClientObject() client.Object
}

type postgresReconciliationTester struct {
	cli                       client.Client
	reconcileFunc             func(ctx context.Context, req ctrl.Request) (ctrl.Result, error)
	postgresExpectations      func()
	updatedObjectExpectations func(newObj client.Object)
}

func (pr *postgresReconciliationTester) setPostgresExpectations(
	postgresExpectations func(),
) {
	pr.postgresExpectations = postgresExpectations
}

func (pr *postgresReconciliationTester) setUpdatedObjectExpectations(
	updatedObjectExpectations func(newObj client.Object),
) {
	pr.updatedObjectExpectations = updatedObjectExpectations
}

func (pr *postgresReconciliationTester) assert(
	ctx context.Context,
	wrapper postgresObjectManager,
) {
	obj := wrapper.GetClientObject()
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

	newObj := obj.DeepCopyObject().(client.Object)
	err = pr.cli.Get(ctx, client.ObjectKey{
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
	}, newObj)
	g.Expect(err).ToNot(g.HaveOccurred())

	if pr.updatedObjectExpectations != nil {
		pr.updatedObjectExpectations(newObj)
	}
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
func assertObjectReconciledAfterDeletion(
	ctx context.Context,
	r reconcile.Reconciler,
	wrapper postgresObjectManager,
	newWrapper postgresObjectManager,
	fakeClient client.Client,
	postgresExpectations func(),
) {
	obj := wrapper.GetClientObject()
	g.Expect(obj.GetFinalizers()).To(g.BeEmpty())

	postgresExpectations()

	// Reconcile and get the updated object
	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
	}})
	g.Expect(err).ToNot(g.HaveOccurred())

	newObj := newWrapper.GetClientObject()
	err = fakeClient.Get(ctx, client.ObjectKey{
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
	}, newObj)
	g.Expect(err).ToNot(g.HaveOccurred())

	// plain successful reconciliation, finalizers have been created
	g.Expect(newWrapper.GetStatusApplied()).Should(g.HaveValue(g.BeTrue()))
	g.Expect(newWrapper.GetStatusMessage()).Should(g.BeEmpty())
	g.Expect(newObj.GetFinalizers()).NotTo(g.BeEmpty())

	// the next 2 lines are a hacky bit to make sure the next reconciler
	// call doesn't skip on account of Generation == ObservedGeneration.
	// See fake.Client known issues with `Generation`
	// https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/client/fake@v0.19.0#NewClientBuilder
	newWrapper.SetObservedGeneration(2)
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
