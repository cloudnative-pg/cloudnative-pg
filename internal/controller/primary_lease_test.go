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

	coordinationv1 "k8s.io/api/coordination/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	k8scheme "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("primaryLeasePredicate", func() {
	lease := &coordinationv1.Lease{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster-example", Namespace: "default"},
	}

	It("ignores Create events", func() {
		Expect(primaryLeasePredicate.Create(event.CreateEvent{Object: lease})).To(BeFalse())
	})

	It("ignores Update events even when spec changes", func() {
		oldLease := lease.DeepCopy()
		newLease := lease.DeepCopy()
		one := int32(1)
		newLease.Spec.LeaseDurationSeconds = &one
		Expect(primaryLeasePredicate.Update(event.UpdateEvent{
			ObjectOld: oldLease,
			ObjectNew: newLease,
		})).To(BeFalse())
	})

	It("ignores Generic events", func() {
		Expect(primaryLeasePredicate.Generic(event.GenericEvent{Object: lease})).To(BeFalse())
	})

	It("forwards Delete events so the parent cluster reconciles and recreates the lease", func() {
		Expect(primaryLeasePredicate.Delete(event.DeleteEvent{Object: lease})).To(BeTrue())
	})
})

var _ = Describe("reconcilePrimaryLease", func() {
	const (
		clusterName = "test-cluster"
		namespace   = "default"
	)

	newCluster := func() *apiv1.Cluster {
		return &apiv1.Cluster{
			TypeMeta: metav1.TypeMeta{
				APIVersion: apiv1.SchemeGroupVersion.String(),
				Kind:       apiv1.ClusterKind,
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterName,
				Namespace: namespace,
				UID:       "new-cluster-uid",
			},
		}
	}

	newReconciler := func(objs ...client.Object) *ClusterReconciler {
		scheme := k8scheme.BuildWithAllKnownScheme()
		return &ClusterReconciler{
			Client:   fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build(),
			Scheme:   scheme,
			Recorder: record.NewFakeRecorder(10),
		}
	}

	getLease := func(ctx context.Context, r *ClusterReconciler) *coordinationv1.Lease {
		l := &coordinationv1.Lease{}
		Expect(r.Get(ctx, types.NamespacedName{Namespace: namespace, Name: clusterName}, l)).To(Succeed())
		return l
	}

	It("creates the lease when none exists", func(ctx context.Context) {
		cluster := newCluster()
		r := newReconciler(cluster)

		Expect(r.reconcilePrimaryLease(ctx, cluster)).To(Succeed())

		l := getLease(ctx, r)
		Expect(metav1.IsControlledBy(l, cluster)).To(BeTrue())
	})

	It("is a no-op when the existing lease is already owned by this cluster", func(ctx context.Context) {
		cluster := newCluster()
		existing := &coordinationv1.Lease{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterName,
				Namespace: namespace,
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: apiv1.SchemeGroupVersion.String(),
					Kind:       apiv1.ClusterKind,
					Name:       clusterName,
					UID:        cluster.UID,
					Controller: ptr.To(true),
				}},
			},
		}
		r := newReconciler(cluster, existing)

		// Compare ResourceVersion before and after to prove the object was not
		// written to by the reconcile (an erroneous Update would bump it).
		before := getLease(ctx, r)
		Expect(r.reconcilePrimaryLease(ctx, cluster)).To(Succeed())
		after := getLease(ctx, r)
		Expect(after.ResourceVersion).To(Equal(before.ResourceVersion))
		Expect(metav1.IsControlledBy(after, cluster)).To(BeTrue())
	})

	It("adopts a lease with no controllerRef", func(ctx context.Context) {
		cluster := newCluster()
		orphan := &coordinationv1.Lease{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterName,
				Namespace: namespace,
			},
		}
		r := newReconciler(cluster, orphan)

		Expect(r.reconcilePrimaryLease(ctx, cluster)).To(Succeed())

		Expect(metav1.IsControlledBy(getLease(ctx, r), cluster)).To(BeTrue())
	})

	It("adopts a lease left over from a previous incarnation of this cluster", func(ctx context.Context) {
		cluster := newCluster()
		stale := &coordinationv1.Lease{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterName,
				Namespace: namespace,
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: apiv1.SchemeGroupVersion.String(),
					Kind:       apiv1.ClusterKind,
					Name:       clusterName,
					UID:        "previous-cluster-uid",
					Controller: ptr.To(true),
				}},
			},
		}
		r := newReconciler(cluster, stale)

		Expect(r.reconcilePrimaryLease(ctx, cluster)).To(Succeed())

		Expect(metav1.IsControlledBy(getLease(ctx, r), cluster)).To(BeTrue())
	})

	It("refuses to adopt a lease controlled by a different kind", func(ctx context.Context) {
		cluster := newCluster()
		foreign := &coordinationv1.Lease{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterName,
				Namespace: namespace,
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: "v1",
					Kind:       "Pod",
					Name:       "some-pod",
					UID:        "pod-uid",
					Controller: ptr.To(true),
				}},
			},
		}
		r := newReconciler(cluster, foreign)
		before := getLease(ctx, r)

		Expect(r.reconcilePrimaryLease(ctx, cluster)).To(MatchError(ContainSubstring("refusing to adopt")))

		// The lease must not have been mutated by the failed reconcile.
		after := getLease(ctx, r)
		Expect(after.ResourceVersion).To(Equal(before.ResourceVersion))
		Expect(after.OwnerReferences).To(Equal(before.OwnerReferences))

		// A Warning event surfaces the conflict to the user.
		events := r.Recorder.(*record.FakeRecorder).Events
		Expect(events).To(Receive(ContainSubstring("PrimaryLeaseConflict")))
	})

	It("refuses to adopt a lease controlled by a different cluster", func(ctx context.Context) {
		cluster := newCluster()
		foreign := &coordinationv1.Lease{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterName,
				Namespace: namespace,
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: apiv1.SchemeGroupVersion.String(),
					Kind:       apiv1.ClusterKind,
					Name:       "other-cluster",
					UID:        "other-cluster-uid",
					Controller: ptr.To(true),
				}},
			},
		}
		r := newReconciler(cluster, foreign)
		before := getLease(ctx, r)

		Expect(r.reconcilePrimaryLease(ctx, cluster)).To(MatchError(ContainSubstring("refusing to adopt")))

		after := getLease(ctx, r)
		Expect(after.ResourceVersion).To(Equal(before.ResourceVersion))
		Expect(after.OwnerReferences).To(Equal(before.OwnerReferences))
	})
})

var _ = Describe("classifyLeaseAdoption", func() {
	const (
		clusterName = "test-cluster"
		namespace   = "default"
	)

	cluster := &apiv1.Cluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: apiv1.SchemeGroupVersion.String(),
			Kind:       apiv1.ClusterKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName,
			Namespace: namespace,
			UID:       "new-cluster-uid",
		},
	}

	leaseWithOwner := func(refs ...metav1.OwnerReference) *coordinationv1.Lease {
		return &coordinationv1.Lease{
			ObjectMeta: metav1.ObjectMeta{
				Name:            clusterName,
				Namespace:       namespace,
				OwnerReferences: refs,
			},
		}
	}

	ourRef := metav1.OwnerReference{
		APIVersion: apiv1.SchemeGroupVersion.String(),
		Kind:       apiv1.ClusterKind,
		Name:       clusterName,
		UID:        cluster.UID,
		Controller: ptr.To(true),
	}

	It("treats a lease controlled by this cluster (UID match) as already ours", func() {
		Expect(classifyLeaseAdoption(leaseWithOwner(ourRef), cluster)).To(Equal(adoptAlreadyOurs))
	})

	It("treats a lease with no controllerRef as an orphan", func() {
		Expect(classifyLeaseAdoption(leaseWithOwner(), cluster)).To(Equal(adoptOrphan))
	})

	It("treats a stale ref to a previous incarnation of this cluster as an orphan", func() {
		stale := ourRef
		stale.UID = "previous-cluster-uid"
		Expect(classifyLeaseAdoption(leaseWithOwner(stale), cluster)).To(Equal(adoptOrphan))
	})

	It("refuses a controllerRef of a different kind", func() {
		differentKind := metav1.OwnerReference{
			APIVersion: "v1", Kind: "Pod", Name: clusterName,
			UID: "pod-uid", Controller: ptr.To(true),
		}
		Expect(classifyLeaseAdoption(leaseWithOwner(differentKind), cluster)).To(Equal(adoptRefuseForeign))
	})

	It("refuses a controllerRef of a different apiVersion", func() {
		differentAPIVersion := ourRef
		differentAPIVersion.UID = "previous-cluster-uid"
		differentAPIVersion.APIVersion = "postgresql.cnpg.io/v1beta1"
		Expect(classifyLeaseAdoption(leaseWithOwner(differentAPIVersion), cluster)).To(Equal(adoptRefuseForeign))
	})

	It("refuses a controllerRef of a different name", func() {
		differentName := ourRef
		differentName.Name = "other-cluster"
		differentName.UID = "other-cluster-uid"
		Expect(classifyLeaseAdoption(leaseWithOwner(differentName), cluster)).To(Equal(adoptRefuseForeign))
	})
})
