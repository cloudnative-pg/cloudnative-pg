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
	objectMutatorFunc[T client.Object]             func(obj T)
	postgresExpectationsFunc                       func()
	updatedObjectExpectationsFunc[T client.Object] func(newObj T)
	reconciliation[T client.Object]                struct {
		objectMutator             objectMutatorFunc[T]
		postgresExpectations      postgresExpectationsFunc
		updatedObjectExpectations updatedObjectExpectationsFunc[T]
		expectMissingObject       bool
	}
)

type postgresReconciliationTester[T client.Object] struct {
	cli                       client.Client
	reconcileFunc             func(ctx context.Context, req ctrl.Request) (ctrl.Result, error)
	objectMutator             objectMutatorFunc[T]
	postgresExpectations      postgresExpectationsFunc
	updatedObjectExpectations updatedObjectExpectationsFunc[T]
	expectMissingObject       bool
	reconciliations           []reconciliation[T]
}

func (pr *postgresReconciliationTester[T]) setObjectMutator(objectMutator objectMutatorFunc[T]) {
	pr.objectMutator = objectMutator
}

func (pr *postgresReconciliationTester[T]) setPostgresExpectations(
	postgresExpectations postgresExpectationsFunc,
) {
	pr.postgresExpectations = postgresExpectations
}

func (pr *postgresReconciliationTester[T]) setUpdatedObjectExpectations(
	updatedObjectExpectations updatedObjectExpectationsFunc[T],
) {
	pr.updatedObjectExpectations = updatedObjectExpectations
}

func (pr *postgresReconciliationTester[T]) setExpectMissingObject() {
	pr.expectMissingObject = true
}

func (pr *postgresReconciliationTester[T]) reconcile() {
	if pr.postgresExpectations == nil && pr.updatedObjectExpectations == nil && !pr.expectMissingObject {
		return
	}

	pr.reconciliations = append(pr.reconciliations, reconciliation[T]{
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

func (pr *postgresReconciliationTester[T]) assert(
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
			r.objectMutator(wrapper.GetClientObject().(T))
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
			r.updatedObjectExpectations(wrapper.GetClientObject().(T))
		}
	}
}
