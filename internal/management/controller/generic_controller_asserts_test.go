package controller

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/onsi/gomega"
)

type postgresObjectManager interface {
	GetStatusApplied() *bool
	GetStatusMessage() string
	SetObservedGeneration(gen int64)
	GetClientObject() client.Object
}

type (
	objectMutatorFunc             func(obj client.Object)
	postgresExpectationsFunc      func()
	updatedObjectExpectationsFunc func(newObj client.Object)
	reconciliation                struct {
		objectMutator             objectMutatorFunc
		postgresExpectations      postgresExpectationsFunc
		updatedObjectExpectations updatedObjectExpectationsFunc
		expectMissingObject       bool
	}
)

type postgresReconciliationTester struct {
	cli                       client.Client
	reconcileFunc             func(ctx context.Context, req ctrl.Request) (ctrl.Result, error)
	objectMutator             objectMutatorFunc
	postgresExpectations      postgresExpectationsFunc
	updatedObjectExpectations updatedObjectExpectationsFunc
	expectMissingObject       bool
	reconciliations           []reconciliation
}

func (pr *postgresReconciliationTester) setObjectMutator(objectMutator objectMutatorFunc) {
	pr.objectMutator = objectMutator
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
		objectMutator:             pr.objectMutator,
		postgresExpectations:      pr.postgresExpectations,
		updatedObjectExpectations: pr.updatedObjectExpectations,
		expectMissingObject:       pr.expectMissingObject,
	})

	pr.objectMutator = nil
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

		if r.objectMutator != nil {
			r.objectMutator(wrapper.GetClientObject())
		}

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
