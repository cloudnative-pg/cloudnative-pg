package controller

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	. "github.com/onsi/gomega"
)

type postgresObjectManager interface {
	GetStatusApplied() *bool
	GetStatusMessage() string
	SetObservedGeneration(gen int64)
	GetClientObject() client.Object
}

type (
	postgresExpectationsFunc      func()
	updatedObjectExpectationsFunc func(newObj client.Object)
	reconciliation                struct {
		postgresExpectations      postgresExpectationsFunc
		updatedObjectExpectations updatedObjectExpectationsFunc
		expectMissingObject       bool
	}
)

type postgresReconciliationTester struct {
	cli                       client.Client
	reconcileFunc             func(ctx context.Context, req ctrl.Request) (ctrl.Result, error)
	postgresExpectations      postgresExpectationsFunc
	updatedObjectExpectations updatedObjectExpectationsFunc
	expectMissingObject       bool
	reconciliations           []reconciliation
}

func (pr *postgresReconciliationTester) setPostgresExpectations(
	postgresExpectations postgresExpectationsFunc,
) {
	pr.postgresExpectations = postgresExpectations
}

func (pr *postgresReconciliationTester) setUpdatedObjectExpectations(
	updatedObjectExpectations updatedObjectExpectationsFunc,
) {
	pr.updatedObjectExpectations = updatedObjectExpectations
}

func (pr *postgresReconciliationTester) setExpectMissingObject() {
	pr.expectMissingObject = true
}

func (pr *postgresReconciliationTester) reconcile() {
	if pr.postgresExpectations == nil && pr.updatedObjectExpectations == nil && !pr.expectMissingObject {
		return
	}

	pr.reconciliations = append(pr.reconciliations, reconciliation{
		postgresExpectations:      pr.postgresExpectations,
		updatedObjectExpectations: pr.updatedObjectExpectations,
		expectMissingObject:       pr.expectMissingObject,
	})

	pr.postgresExpectations = nil
	pr.updatedObjectExpectations = nil
	pr.expectMissingObject = false
}

func (pr *postgresReconciliationTester) assert(
	ctx context.Context,
	wrapper postgresObjectManager,
) {
	obj := wrapper.GetClientObject()
	Expect(obj.GetFinalizers()).To(BeEmpty())

	pr.reconcile()
	for _, r := range pr.reconciliations {
		if r.postgresExpectations != nil {
			r.postgresExpectations()
		}

		// Reconcile and get the updated object
		_, err := pr.reconcileFunc(ctx, ctrl.Request{NamespacedName: types.NamespacedName{
			Namespace: obj.GetNamespace(),
			Name:      obj.GetName(),
		}})
		Expect(err).ToNot(HaveOccurred())

		err = pr.cli.Get(ctx, client.ObjectKey{
			Namespace: obj.GetNamespace(),
			Name:      obj.GetName(),
		}, wrapper.GetClientObject())
		if r.expectMissingObject {
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		} else {
			Expect(err).ToNot(HaveOccurred())
		}

		if r.updatedObjectExpectations != nil {
			r.updatedObjectExpectations(wrapper.GetClientObject())
		}
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
	fakeClient client.Client,
	postgresExpectations func(),
) {
	tester := postgresReconciliationTester{
		reconcileFunc: r.Reconcile,
		cli:           fakeClient,
	}
	tester.setPostgresExpectations(postgresExpectations)
	tester.setUpdatedObjectExpectations(func(newObj client.Object) {
		// Plain successful reconciliation, finalizers have been created
		Expect(newObj.GetFinalizers()).NotTo(BeEmpty())
		// TODO check the message and the applied status

		// The next 2 lines are a hacky bit to make sure the next reconciler
		// call doesn't skip on account of Generation == ObservedGeneration.
		// See fake.Client known issues with `Generation`
		// https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/client/fake@v0.19.0#NewClientBuilder
		newObj.SetGeneration(newObj.GetGeneration() + 1)
		Expect(fakeClient.Update(ctx, newObj)).To(Succeed())

		// We now look at the behavior when we delete the Database object
		Expect(fakeClient.Delete(ctx, newObj)).To(Succeed())
	})
	tester.reconcile()
	tester.setExpectMissingObject()
	tester.assert(ctx, wrapper)
}
