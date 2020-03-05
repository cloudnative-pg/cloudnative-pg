/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package controller

import (
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"

	apiv1alpha1 "github.com/2ndquadrant/cloud-native-postgresql/api/v1alpha1"
)

// Reconcile is the main reconciliation loop for the instance
func (r *InstanceReconciler) Reconcile(event *watch.Event) error {
	// Nothing I can do, here
	if event.Type != watch.Modified {
		return nil
	}

	object, err := objectToUnstructured(event.Object)
	if err != nil {
		return errors.Wrap(err, "Error while decoding runtime.Object data from watch")
	}

	targetPrimary, err := getTargetPrimary(object)
	if err != nil {
		return err
	}

	if targetPrimary == r.instance.PodName {
		return r.reconcilePrimary(object)
	}

	return nil
}

// Reconciler primary logic
func (r *InstanceReconciler) reconcilePrimary(cluster *unstructured.Unstructured) error {
	isPrimary, err := r.instance.IsPrimary()
	if err != nil {
		return err
	}

	if isPrimary {
		// All right
		return nil
	}

	r.log.Info("I'm the target primary, promoting my instance")

	// I must promote my instance here
	err = r.instance.PromoteAndWait()
	if err != nil {
		return errors.Wrap(err, "Error promoting instance")
	}

	// Now I'm the primary
	r.log.Info("Setting myself as the current primary")
	err = setCurrentPrimary(cluster, r.instance.PodName)
	if err != nil {
		return err
	}

	_, err = r.client.
		Resource(apiv1alpha1.ClusterGVK).
		Namespace(r.instance.Namespace).
		UpdateStatus(cluster, metav1.UpdateOptions{})
	if err != nil {
		return err
	}

	return nil
}

// objectToUnstructured convert a runtime Object into an unstructured one
func objectToUnstructured(object runtime.Object) (*unstructured.Unstructured, error) {
	data, err := runtime.DefaultUnstructuredConverter.ToUnstructured(object)
	if err != nil {
		return nil, err
	}

	return &unstructured.Unstructured{Object: data}, nil
}
