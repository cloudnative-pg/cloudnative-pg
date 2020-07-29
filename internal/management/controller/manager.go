/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

// Package controller contains the function in PGK that reacts to events in
// the cluster.
package controller

import (
	"time"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	utilnet "k8s.io/apimachinery/pkg/util/net"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"

	apiv1alpha1 "gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/api/v1alpha1"
	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/pkg/management/log"
	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/pkg/management/postgres"
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
func (r *InstanceReconciler) Run() {
	for {
		// Retry with exponential back-off unless it is a connection refused error
		err := retry.OnError(retry.DefaultBackoff, func(err error) bool {
			r.log.Error(err, "Error calling Watch", "cluster", r.instance.ClusterName)
			return !utilnet.IsConnectionRefused(err)
		}, r.Watch)
		if err != nil {
			// If this is "connection refused" error, it means that most likely apiserver is not responsive.
			// If that's the case wait and resend watch request.
			time.Sleep(time.Second)
		}
	}
}

// Watch contains the main reconciler loop
func (r *InstanceReconciler) Watch() error {
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

		err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
			return r.Reconcile(&event)
		})
		if err != nil {
			r.log.Error(err, "Reconciliation error")
		}
	}

	r.instanceWatch.Stop()
	return nil
}

// Stop stops the controller
func (r *InstanceReconciler) Stop() {
	r.instanceWatch.Stop()
}
