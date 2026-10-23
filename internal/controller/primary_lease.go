/*
Copyright © contributors to CloudNativePG, established as
CloudNativePG a Series of LF Projects, LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
*/

package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
	coordinationv1 "k8s.io/api/coordination/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

// primaryLeasePredicate enqueues the parent Cluster only when the owned Lease
// is deleted. Renew/holder updates happen every few seconds and would otherwise
// trigger a reconcile storm.
var primaryLeasePredicate = predicate.Funcs{
	CreateFunc:  func(event.CreateEvent) bool { return false },
	UpdateFunc:  func(event.UpdateEvent) bool { return false },
	DeleteFunc:  func(event.DeleteEvent) bool { return true },
	GenericFunc: func(event.GenericEvent) bool { return false },
}

// reconcilePrimaryLease ensures a Lease object named after the cluster exists and is owned by it.
// The instance manager uses this lease as a primary-election mutex.
func (r *ClusterReconciler) reconcilePrimaryLease(ctx context.Context, cluster *apiv1.Cluster) error {
	contextLogger := log.FromContext(ctx)

	lease := coordinationv1.Lease{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: cluster.Namespace,
			Name:      cluster.Name,
		},
	}
	cluster.SetInheritedDataAndOwnership(&lease.ObjectMeta)

	err := r.Create(ctx, &lease)
	switch {
	case err == nil:
		contextLogger.Info("Created primary lease", "leaseName", lease.Name)
		return nil
	case !apierrs.IsAlreadyExists(err):
		return err
	}

	// A lease with our name already exists. Either we already own it (common
	// path, every reconcile) or it is a leftover from a previous incarnation
	// of this cluster that Kubernetes GC has not yet cleaned up. In the second
	// case we adopt it so the current Cluster's deletion eventually cascades
	// to it; if it is controlled by anything else we refuse to take over.
	existing := &coordinationv1.Lease{}
	if err := r.Get(ctx, client.ObjectKeyFromObject(&lease), existing); err != nil {
		return err
	}
	switch classifyLeaseAdoption(existing, cluster) {
	case adoptAlreadyOurs:
		return nil
	case adoptRefuseForeign:
		owner := metav1.GetControllerOf(existing)
		err := fmt.Errorf("primary lease %s/%s is controlled by %s %q, refusing to adopt",
			existing.Namespace, existing.Name, owner.Kind, owner.Name)
		r.Recorder.Eventf(cluster, "Warning", "PrimaryLeaseConflict", "%v", err)
		return err
	case adoptOrphan:
		cluster.SetInheritedDataAndOwnership(&existing.ObjectMeta)
		if err := r.Update(ctx, existing); err != nil {
			return err
		}
		contextLogger.Info("Adopted orphan primary lease", "leaseName", existing.Name)
	}
	return nil
}

// leaseObservation is the primary Lease record of a cluster as the controller
// first saw it, and when. RenewTime is the holder's clock and is compared for
// equality only; the window is timed on observedAt, the controller's clock,
// which keeps the monotonic reading of time.Now so that a step of the wall
// clock cannot move it.
type leaseObservation struct {
	holder     string
	renewTime  time.Time
	observedAt time.Time
}

// livePrimaryLeaseHolder returns the holder of the cluster's primary Lease
// while the record has been seen unchanged for less than the lease duration,
// and "" when the Lease is missing, released or has gone unrenewed that long.
// A fresh record always counts as live: the first sight starts the window.
func (r *ClusterReconciler) livePrimaryLeaseHolder(ctx context.Context, cluster *apiv1.Cluster) (string, error) {
	key := client.ObjectKeyFromObject(cluster)
	var lease coordinationv1.Lease
	if err := r.Get(ctx, key, &lease); err != nil {
		if apierrs.IsNotFound(err) {
			r.forgetPrimaryLease(key)
			return "", nil
		}
		return "", err
	}
	holder := ptr.Deref(lease.Spec.HolderIdentity, "")
	if holder == "" {
		r.forgetPrimaryLease(key)
		return "", nil
	}
	var renewTime time.Time
	if lease.Spec.RenewTime != nil {
		renewTime = lease.Spec.RenewTime.Time
	}

	now := time.Now()
	r.leaseMu.Lock()
	defer r.leaseMu.Unlock()
	prev, seen := r.leaseObservations[key]
	if !seen || prev.holder != holder || !prev.renewTime.Equal(renewTime) {
		r.leaseObservations[key] = leaseObservation{holder: holder, renewTime: renewTime, observedAt: now}
		return holder, nil
	}
	if now.Sub(prev.observedAt) >= cluster.GetPrimaryLeaseDuration() {
		return "", nil
	}
	return holder, nil
}

// checkOldPrimaryLease returns ErrPrimaryLeaseHeld while the old primary may
// still be sending WAL: its lease is held, or cannot be read. A lease held by
// anyone else is that of a target that was never promoted, and waiting for it
// would stall the failover for as long as that target keeps renewing.
func (r *ClusterReconciler) checkOldPrimaryLease(ctx context.Context, cluster *apiv1.Cluster) error {
	holder, err := r.livePrimaryLeaseHolder(ctx, cluster)
	if err != nil || (holder != "" && holder == cluster.Status.CurrentPrimary) {
		log.FromContext(ctx).Info("Primary lease still held by the old primary or unreadable, deferring election",
			"holder", holder, "err", err)
		return ErrPrimaryLeaseHeld
	}
	return nil
}

// forgetPrimaryLease drops the lease observation of a cluster.
func (r *ClusterReconciler) forgetPrimaryLease(key client.ObjectKey) {
	r.leaseMu.Lock()
	defer r.leaseMu.Unlock()
	delete(r.leaseObservations, key)
}

// leaseAdoption is the decision made about an existing Lease named after the
// cluster but not (yet) controlled by it.
type leaseAdoption int

const (
	// adoptAlreadyOurs means the lease is already controlled by this cluster
	// (UID match). No write is needed.
	adoptAlreadyOurs leaseAdoption = iota
	// adoptOrphan means the lease has no controllerRef, or its controllerRef
	// points at a previous incarnation of this same cluster (same Kind, same
	// APIVersion, same name, different UID). Safe to rewrite ownership.
	adoptOrphan
	// adoptRefuseForeign means the lease is genuinely controlled by something
	// else (different kind, different apiVersion, or different name). We must
	// not take it over.
	adoptRefuseForeign
)

// classifyLeaseAdoption decides what to do with an existing Lease named after
// the cluster. It is pure so the three branches can be unit-tested without an
// apiserver.
func classifyLeaseAdoption(existing *coordinationv1.Lease, cluster *apiv1.Cluster) leaseAdoption {
	if metav1.IsControlledBy(existing, cluster) {
		return adoptAlreadyOurs
	}
	owner := metav1.GetControllerOf(existing)
	if owner == nil {
		return adoptOrphan
	}
	if owner.Kind == apiv1.ClusterKind &&
		owner.APIVersion == apiv1.SchemeGroupVersion.String() &&
		owner.Name == cluster.Name {
		return adoptOrphan
	}
	return adoptRefuseForeign
}
