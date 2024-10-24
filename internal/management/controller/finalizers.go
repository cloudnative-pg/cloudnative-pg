package controller

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type finalizerReconciler[T client.Object] struct {
	cli           client.Client
	finalizerName string
	onRemoveFunc  func(ctx context.Context, resource T) error
}

func newFinalizerReconciler[T client.Object](
	cli client.Client,
	finalizerName string,
	onRemoveFunc func(ctx context.Context, resource T) error,
) *finalizerReconciler[T] {
	return &finalizerReconciler[T]{
		cli:           cli,
		finalizerName: finalizerName,
		onRemoveFunc:  onRemoveFunc,
	}
}

func (f finalizerReconciler[T]) reconcile(ctx context.Context, resource T) error {
	// add finalizer in non-deleted publications if not present
	if resource.GetDeletionTimestamp().IsZero() {
		if !controllerutil.AddFinalizer(resource, f.finalizerName) {
			return nil
		}
		return f.cli.Update(ctx, resource)
	}

	// the publication is being deleted but no finalizer is present, we can quit
	if !controllerutil.ContainsFinalizer(resource, f.finalizerName) {
		return nil
	}

	if err := f.onRemoveFunc(ctx, resource); err != nil {
		return err
	}

	// remove our finalizer from the list and update it.
	controllerutil.RemoveFinalizer(resource, f.finalizerName)
	return f.cli.Update(ctx, resource)
}
