package controller

import (
	"context"
	"fmt"

	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type reconcilerer interface {
	Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error)
}

type postgresObjectManager interface {
	client.Object
	GetStatusApplied() *bool
	GetStatusMessage() string
	// GetName() string
}

func assertObjectWasReconciled[T postgresObjectManager](
	ctx context.Context,
	r reconcilerer,
	obj T,
	newObj T,
	fakeClient client.Client,
	postgresExpectations func(),
	updatedObjectExpectations func(newObj T),
) {
	gomega.Expect(obj.GetFinalizers()).To(gomega.BeEmpty())

	postgresExpectations()

	// Reconcile and get the updated object
	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
	}})
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	err = fakeClient.Get(ctx, client.ObjectKey{
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
	}, newObj)

	errstr := fmt.Sprintf("err: %#v\n", err)
	_ = errstr
	kind := obj.GetObjectKind().GroupVersionKind()
	gomega.Expect(kind).NotTo(gomega.BeNil())

	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	updatedObjectExpectations(newObj)
}
