/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package controller

import (
	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"

	apiv1alpha1 "github.com/2ndquadrant/cloud-native-postgresql/api/v1alpha1"
	"github.com/2ndquadrant/cloud-native-postgresql/pkg/management/log"
	"github.com/2ndquadrant/cloud-native-postgresql/pkg/management/postgres"
)

// InstanceReconciler can reconcile the status of the PostgreSQL cluster with
// the one of this PostgreSQL instance
type InstanceReconciler struct {
	client        dynamic.Interface
	instance      *postgres.Instance
	log           logr.Logger
	instanceWatch watch.Interface
}

// NewInstanceReconciler create a new instance reconciler
func NewInstanceReconciler(instance *postgres.Instance) (*InstanceReconciler, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}

	client, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return &InstanceReconciler{
		instance: instance,
		log:      log.Log,
		client:   client,
	}, nil
}

// Run runs the reconciliation loop for this resource
func (r *InstanceReconciler) Run() error {
	var err error

	// This is an example of how to watch a certain object
	// https://github.com/kubernetes/kubernetes/issues/43299
	r.instanceWatch, err = r.client.
		Resource(apiv1alpha1.ClusterGVK).
		Namespace(r.instance.Namespace).
		Watch(metav1.ListOptions{
			FieldSelector: fields.OneTermEqualSelector("metadata.name", r.instance.ClusterName).String(),
		})
	if err != nil {
		return err
	}

	channel := r.instanceWatch.ResultChan()
	for {
		event, ok := <-channel
		if !ok {
			break
		}

		err = r.Reconcile(&event)
		if err != nil {
			r.log.Error(err, "Reconciliation error")
			// TODO Retry with exponential back-off
		}
	}

	return nil
}

// Stop stops the controller
func (r *InstanceReconciler) Stop() {
	r.instanceWatch.Stop()
}
