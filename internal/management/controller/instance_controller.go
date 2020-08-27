/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package controller

import (
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/common/log"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/util/retry"

	apiv1alpha1 "gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/api/v1alpha1"
	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/pkg/utils"
)

// Reconcile is the main reconciliation loop for the instance
func (r *InstanceReconciler) Reconcile(event *watch.Event) error {
	r.log.Info("Reconciliation loop", "eventType", event.Type)

	object, err := objectToUnstructured(event.Object)
	if err != nil {
		return errors.Wrap(err, "Error while decoding runtime.Object data from watch")
	}

	targetPrimary, err := utils.GetTargetPrimary(object)
	if err != nil {
		return err
	}

	if targetPrimary == r.instance.PodName {
		return r.reconcilePrimary(object)
	}

	return r.reconcileReplica()
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

	r.log.Info("I'm the target primary, wait for the wal_receiver to be terminated")

	err = r.waitForWalReceiverDown()
	if err != nil {
		return err
	}

	r.log.Info("I'm the target primary, wait for every pending WAL record to be applied")

	err = r.waitForApply()

	r.log.Info("I'm the target primary, promoting my instance")

	// I must promote my instance here
	err = r.instance.PromoteAndWait()
	if err != nil {
		return errors.Wrap(err, "Error promoting instance")
	}

	// Now I'm the primary, need to inform the operator
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		r.log.Info("Setting myself as the current primary")
		err = utils.SetCurrentPrimary(cluster, r.instance.PodName)
		if err != nil {
			return err
		}

		_, err = r.client.
			Resource(apiv1alpha1.ClusterGVK).
			Namespace(r.instance.Namespace).
			UpdateStatus(cluster, metav1.UpdateOptions{})
		if err != nil {
			log.Error(err, "Error while setting current primary, retrying")
		}

		// If we have a conflict, let's replace the cluster info
		// with one more fresh
		if apierrors.IsConflict(err) {
			var errRefresh error
			cluster, errRefresh = r.client.
				Resource(apiv1alpha1.ClusterGVK).
				Namespace(r.instance.Namespace).
				Get(r.instance.ClusterName, metav1.GetOptions{})

			if errRefresh != nil {
				log.Error(errRefresh, "Error while refreshing cluster info")
			}
		}
		return err
	})

	return err
}

// Reconciler replica logic
func (r *InstanceReconciler) reconcileReplica() error {
	isPrimary, err := r.instance.IsPrimary()
	if err != nil {
		return err
	}

	if !isPrimary {
		// All right
		return nil
	}

	// I was the primary, but now I'm not the primary anymore.
	// Here we need to invoke a fast shutdown on the instance, and wait the the pod
	// restart to demote as a replica of the new primary
	return r.instance.Shutdown()
}

// objectToUnstructured convert a runtime Object into an unstructured one
func objectToUnstructured(object runtime.Object) (*unstructured.Unstructured, error) {
	data, err := runtime.DefaultUnstructuredConverter.ToUnstructured(object)
	if err != nil {
		return nil, err
	}

	return &unstructured.Unstructured{Object: data}, nil
}

// waitForApply wait for every transaction log to be applied
func (r *InstanceReconciler) waitForApply() error {
	// TODO: exponential backoff
	for {
		lag, err := r.instance.GetWALApplyLag()
		if err != nil {
			return err
		}

		if lag <= 0 {
			break
		}

		r.log.Info("Still need to apply transaction log info, waiting for 2 seconds",
			"lag", lag)
		time.Sleep(time.Second * 1)
	}

	return nil
}

// waitForWalReceiverDown wait until the wal receiver is down, and it's used
// to grab all the WAL files from a replica
func (r *InstanceReconciler) waitForWalReceiverDown() error {
	// TODO: exponential backoff
	for {
		status, err := r.instance.IsWALReceiverActive()
		if err != nil {
			return err
		}

		if !status {
			break
		}

		r.log.Info("WAL receiver is still active, waiting for 2 seconds")
		time.Sleep(time.Second * 1)
	}

	return nil
}
