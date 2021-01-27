/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package controller contains the function in PGK that reacts to events in
// the cluster.
package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilnet "k8s.io/apimachinery/pkg/util/net"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres"
)

// InstanceReconciler can reconcile the status of the PostgreSQL cluster with
// the one of this PostgreSQL instance. Also the configuration in the
// ConfigMap is applied when needed
type InstanceReconciler struct {
	client              dynamic.Interface
	instance            *postgres.Instance
	log                 logr.Logger
	instanceWatch       watch.Interface
	configMapWatch      watch.Interface
	serverSecretWatch   watch.Interface
	caSecretWatch       watch.Interface
	postgresSecretWatch watch.Interface
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

	ctx := context.Background()

	// This is an example of how to watch a certain object
	// https://github.com/kubernetes/kubernetes/issues/43299
	r.instanceWatch, err = r.client.
		Resource(apiv1.ClusterGVK).
		Namespace(r.instance.Namespace).
		Watch(ctx, metav1.ListOptions{
			FieldSelector: fields.OneTermEqualSelector("metadata.name", r.instance.ClusterName).String(),
		})
	if err != nil {
		return fmt.Errorf("error watching cluster: %w", err)
	}

	r.configMapWatch, err = r.client.
		Resource(schema.GroupVersionResource{
			Group:    "",
			Version:  "v1",
			Resource: "configmaps",
		}).
		Namespace(r.instance.Namespace).
		Watch(ctx, metav1.ListOptions{
			FieldSelector: fields.OneTermEqualSelector("metadata.name", r.instance.ClusterName).String(),
		})
	if err != nil {
		return fmt.Errorf("error watching configmap: %w", err)
	}

	r.serverSecretWatch, err = r.client.
		Resource(schema.GroupVersionResource{
			Group:    "",
			Version:  "v1",
			Resource: "secrets",
		}).
		Namespace(r.instance.Namespace).
		Watch(ctx, metav1.ListOptions{
			FieldSelector: fields.OneTermEqualSelector(
				"metadata.name", r.instance.ClusterName+apiv1.ServerSecretSuffix).String(),
		})
	if err != nil {
		return fmt.Errorf("error watching certificate secret: %w", err)
	}

	r.caSecretWatch, err = r.client.
		Resource(schema.GroupVersionResource{
			Group:    "",
			Version:  "v1",
			Resource: "secrets",
		}).
		Namespace(r.instance.Namespace).
		Watch(ctx, metav1.ListOptions{
			FieldSelector: fields.OneTermEqualSelector(
				"metadata.name", r.instance.ClusterName+apiv1.CaSecretSuffix).String(),
		})
	if err != nil {
		return fmt.Errorf("error watching CA secret: %w", err)
	}

	r.postgresSecretWatch, err = r.client.
		Resource(schema.GroupVersionResource{
			Group:    "",
			Version:  "v1",
			Resource: "secrets",
		}).
		Namespace(r.instance.Namespace).
		Watch(ctx, metav1.ListOptions{
			FieldSelector: fields.OneTermEqualSelector(
				"metadata.name", r.instance.ClusterName+apiv1.ReplicationSecretSuffix).String(),
		})
	if err != nil {
		return fmt.Errorf("error watching 'postgres' user secret: %w", err)
	}

	instanceChannel := r.instanceWatch.ResultChan()
	configMapChannel := r.configMapWatch.ResultChan()
	secretChannel := r.serverSecretWatch.ResultChan()
	caSecretChannel := r.caSecretWatch.ResultChan()
	postgresSecretChannel := r.postgresSecretWatch.ResultChan()

	for {
		var event watch.Event
		var ok bool

		select {
		case event, ok = <-instanceChannel:
		case event, ok = <-configMapChannel:
		case event, ok = <-secretChannel:
		case event, ok = <-caSecretChannel:
		case event, ok = <-postgresSecretChannel:
		}

		if !ok {
			break
		}

		err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
			return r.Reconcile(ctx, &event)
		})
		if err != nil {
			r.log.Error(err, "Reconciliation error")
		}
	}

	r.instanceWatch.Stop()
	r.configMapWatch.Stop()
	r.serverSecretWatch.Stop()
	r.caSecretWatch.Stop()
	r.postgresSecretWatch.Stop()
	return nil
}

// Stop stops the controller
func (r *InstanceReconciler) Stop() {
	r.instanceWatch.Stop()
}

// GetClient returns the client that is being used for a certain reconciler
func (r *InstanceReconciler) GetClient() dynamic.Interface {
	return r.client
}
