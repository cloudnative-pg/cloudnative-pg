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
	"database/sql"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	testRoleName    = "app_role"
	testClusterName = "cluster-example"
	testPodName     = "cluster-example-1"
	testNamespace   = "default"
)

// fakeRoleInstance is a minimal instanceInterface for the DatabaseRole reconciler tests.
type fakeRoleInstance struct {
	db *sql.DB
}

func (f *fakeRoleInstance) GetSuperUserDB() (*sql.DB, error) { return f.db, nil }
func (f *fakeRoleInstance) GetClusterName() string           { return testClusterName }
func (f *fakeRoleInstance) GetPodName() string               { return testPodName }
func (f *fakeRoleInstance) GetNamespaceName() string         { return testNamespace }

func newTestDatabaseRole() *apiv1.DatabaseRole {
	return &apiv1.DatabaseRole{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "role-cr",
			Namespace:  testNamespace,
			Generation: 1,
		},
		Spec: apiv1.DatabaseRoleSpec{
			RoleConfiguration: apiv1.RoleConfiguration{Name: testRoleName},
			ClusterRef:        corev1.LocalObjectReference{Name: testClusterName},
			ReclaimPolicy:     apiv1.DatabaseRoleReclaimRetain,
		},
	}
}

func newTestCluster() *apiv1.Cluster {
	return &apiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: testClusterName, Namespace: testNamespace},
		Status: apiv1.ClusterStatus{
			CurrentPrimary: testPodName,
			TargetPrimary:  testPodName,
		},
	}
}

// shadowRole makes the cluster manage testRoleName through its inline managed.roles stanza.
func shadowRole(cluster *apiv1.Cluster) {
	cluster.Spec.Managed = &apiv1.ManagedConfiguration{
		Roles: []apiv1.RoleConfiguration{{Name: testRoleName}},
	}
}

func makeReplica(cluster *apiv1.Cluster) {
	cluster.Spec.ReplicaCluster = &apiv1.ReplicaClusterConfiguration{Enabled: ptr.To(true)}
}

// markReconciled records a successful past reconciliation, as
// succeededReconciliation would have.
func markReconciled(role *apiv1.DatabaseRole) {
	role.Status.ObservedGeneration = role.Generation
}

func markDeleting(role *apiv1.DatabaseRole) {
	now := metav1.Now()
	role.DeletionTimestamp = &now
	role.Finalizers = []string{utils.RoleFinalizerName}
	// On a live apiserver a deleting object's generation has moved past its
	// observedGeneration, so it is never treated as already-reconciled. The
	// fake client does not reproduce that, so model it here.
	role.Generation++
}

var _ = Describe("DatabaseRole shouldDropRole", func() {
	DescribeTable("decides whether a deleted role must be dropped",
		func(policy apiv1.DatabaseRoleReclaimPolicy, reconciled bool,
			mutateCluster func(*apiv1.Cluster), expected bool,
		) {
			role := newTestDatabaseRole()
			role.Spec.ReclaimPolicy = policy
			if reconciled {
				markReconciled(role)
			}
			cluster := newTestCluster()
			if mutateCluster != nil {
				mutateCluster(cluster)
			}
			Expect(shouldDropRole(role, cluster)).To(Equal(expected))
		},
		Entry("delete policy, role owned by this cluster", apiv1.DatabaseRoleReclaimDelete, true, nil, true),
		Entry("retain policy", apiv1.DatabaseRoleReclaimRetain, true, nil, false),
		Entry("delete policy, shadowed by inline managed.roles",
			apiv1.DatabaseRoleReclaimDelete, true, shadowRole, false),
		Entry("delete policy, replica cluster", apiv1.DatabaseRoleReclaimDelete, true, makeReplica, false),
		Entry("delete policy, never reconciled (conflicting duplicate)",
			apiv1.DatabaseRoleReclaimDelete, false, nil, false),
	)
})

var _ = Describe("DatabaseRole isAlreadyReconciled", func() {
	r := &DatabaseRoleReconciler{}

	It("is false while the role is being deleted", func() {
		role := newTestDatabaseRole()
		role.Status.ObservedGeneration = role.Generation
		markDeleting(role)
		Expect(r.isAlreadyReconciled(role)).To(BeFalse())
	})

	It("is true when the generation matches and no secret is configured", func() {
		role := newTestDatabaseRole()
		role.Status.ObservedGeneration = role.Generation
		Expect(r.isAlreadyReconciled(role)).To(BeTrue())
	})

	It("is false when the generation has moved on", func() {
		role := newTestDatabaseRole()
		role.Status.ObservedGeneration = role.Generation - 1
		Expect(r.isAlreadyReconciled(role)).To(BeFalse())
	})

	When("a password secret is configured", func() {
		newRoleWithSecret := func() *apiv1.DatabaseRole {
			role := newTestDatabaseRole()
			role.Spec.PasswordSecret = &apiv1.LocalObjectReference{Name: "role-secret"}
			role.Status.ObservedGeneration = role.Generation
			return role
		}
		setObservedSecretVersion := func(role *apiv1.DatabaseRole, version string) {
			role.Status.Conditions = []metav1.Condition{{
				Type:               string(apiv1.ConditionPasswordSecretChange),
				Status:             metav1.ConditionTrue,
				Reason:             "SecretChanged",
				LastTransitionTime: metav1.Now(),
				Message:            version,
			}}
		}

		It("is true when the applied secret version matches the observed one", func() {
			role := newRoleWithSecret()
			setObservedSecretVersion(role, "rv-1")
			role.Status.SecretResourceVersion = "rv-1"
			Expect(r.isAlreadyReconciled(role)).To(BeTrue())
		})

		It("is false when the secret version changed", func() {
			role := newRoleWithSecret()
			setObservedSecretVersion(role, "rv-2")
			role.Status.SecretResourceVersion = "rv-1"
			Expect(r.isAlreadyReconciled(role)).To(BeFalse())
		})
	})
})

var _ = Describe("DatabaseRole shouldReconcile", func() {
	reconcilerFor := func(role *apiv1.DatabaseRole) *DatabaseRoleReconciler {
		fakeClient := fake.NewClientBuilder().
			WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
			WithObjects(role).
			WithStatusSubresource(&apiv1.DatabaseRole{}).
			Build()
		return &DatabaseRoleReconciler{Client: fakeClient, instance: &fakeRoleInstance{}}
	}

	requeue := &ctrl.Result{RequeueAfter: databaseRoleReconciliationInterval}

	DescribeTable("applies the instance/timing and apply-path gates",
		func(setup func(role *apiv1.DatabaseRole, cluster *apiv1.Cluster), expected *ctrl.Result) {
			role := newTestDatabaseRole()
			cluster := newTestCluster()
			setup(role, cluster)

			result, err := reconcilerFor(role).shouldReconcile(context.Background(), role, cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(expected))
		},
		Entry("proceeds for a fresh role on the primary",
			func(_ *apiv1.DatabaseRole, _ *apiv1.Cluster) {}, nil),
		Entry("stops when the role is already reconciled",
			func(role *apiv1.DatabaseRole, _ *apiv1.Cluster) {
				role.Status.ObservedGeneration = role.Generation
			}, &ctrl.Result{}),
		Entry("requeues when this pod is not the primary",
			func(_ *apiv1.DatabaseRole, cluster *apiv1.Cluster) {
				cluster.Status.CurrentPrimary = "other-pod"
			}, requeue),
		Entry("requeues during a switchover",
			func(_ *apiv1.DatabaseRole, cluster *apiv1.Cluster) {
				cluster.Status.TargetPrimary = "other-pod"
			}, requeue),
		Entry("proceeds while deleting even if shadowed by inline managed.roles",
			func(role *apiv1.DatabaseRole, cluster *apiv1.Cluster) {
				markDeleting(role)
				shadowRole(cluster)
			}, nil),
		Entry("stops when shadowed by inline managed.roles",
			func(_ *apiv1.DatabaseRole, cluster *apiv1.Cluster) {
				shadowRole(cluster)
			}, requeue),
		Entry("surfaces the inline takeover of an already-reconciled role",
			func(role *apiv1.DatabaseRole, cluster *apiv1.Cluster) {
				markReconciled(role)
				shadowRole(cluster)
			}, requeue),
		Entry("stays dormant when already reconciled and shadowed on a non-primary pod",
			func(role *apiv1.DatabaseRole, cluster *apiv1.Cluster) {
				markReconciled(role)
				shadowRole(cluster)
				cluster.Status.CurrentPrimary = "other-pod"
			}, &ctrl.Result{}),
		Entry("stops on a replica cluster",
			func(_ *apiv1.DatabaseRole, cluster *apiv1.Cluster) {
				makeReplica(cluster)
			}, requeue),
	)

	It("persists Applied=false when shadowed by inline managed.roles", func() {
		role := newTestDatabaseRole()
		role.Status.Applied = ptr.To(true)
		cluster := newTestCluster()
		shadowRole(cluster)
		r := reconcilerFor(role)

		_, err := r.shouldReconcile(context.Background(), role, cluster)
		Expect(err).NotTo(HaveOccurred())

		got := &apiv1.DatabaseRole{}
		Expect(r.Get(context.Background(), client.ObjectKeyFromObject(role), got)).To(Succeed())
		Expect(got.Status.Applied).To(Equal(ptr.To(false)))
	})

	It("voids the recorded reconciliation when shadowed after a successful apply", func() {
		role := newTestDatabaseRole()
		markReconciled(role)
		role.Status.Applied = ptr.To(true)
		cluster := newTestCluster()
		shadowRole(cluster)
		r := reconcilerFor(role)

		result, err := r.shouldReconcile(context.Background(), role, cluster)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(requeue))

		got := &apiv1.DatabaseRole{}
		Expect(r.Get(context.Background(), client.ObjectKeyFromObject(role), got)).To(Succeed())
		Expect(got.Status.Applied).To(Equal(ptr.To(false)))
		Expect(got.Status.Message).To(ContainSubstring("managed by the CNPG cluster"))
		Expect(got.Status.ObservedGeneration).To(BeZero())
	})

	It("persists Applied=Unknown (nil) on a replica cluster", func() {
		role := newTestDatabaseRole()
		role.Status.Applied = ptr.To(true)
		cluster := newTestCluster()
		makeReplica(cluster)
		r := reconcilerFor(role)

		_, err := r.shouldReconcile(context.Background(), role, cluster)
		Expect(err).NotTo(HaveOccurred())

		got := &apiv1.DatabaseRole{}
		Expect(r.Get(context.Background(), client.ObjectKeyFromObject(role), got)).To(Succeed())
		Expect(got.Status.Applied).To(BeNil())
	})
})

var _ = Describe("DatabaseRole handleDeletion", func() {
	var (
		db     *sql.DB
		dbMock sqlmock.Sqlmock
	)

	BeforeEach(func() {
		var err error
		db, dbMock, err = sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		Expect(dbMock.ExpectationsWereMet()).To(Succeed())
	})

	// run persists the (deleting) role, then drives handleDeletion against it.
	run := func(role *apiv1.DatabaseRole, cluster *apiv1.Cluster) (client.Client, ctrl.Result) {
		fakeClient := fake.NewClientBuilder().
			WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
			WithObjects(role).
			WithStatusSubresource(&apiv1.DatabaseRole{}).
			Build()
		r := &DatabaseRoleReconciler{Client: fakeClient, instance: &fakeRoleInstance{db: db}}

		// Re-read so the object carries a resourceVersion for the finalizer update.
		persisted := &apiv1.DatabaseRole{}
		Expect(fakeClient.Get(context.Background(), client.ObjectKeyFromObject(role), persisted)).To(Succeed())

		result, err := r.handleDeletion(context.Background(), persisted, cluster)
		Expect(err).NotTo(HaveOccurred())
		return fakeClient, result
	}

	expectFinalizerReleased := func(c client.Client, role *apiv1.DatabaseRole) {
		got := &apiv1.DatabaseRole{}
		err := c.Get(context.Background(), client.ObjectKeyFromObject(role), got)
		if apierrors.IsNotFound(err) {
			return // removing the last finalizer completed the deletion
		}
		Expect(err).NotTo(HaveOccurred())
		Expect(got.Finalizers).NotTo(ContainElement(utils.RoleFinalizerName))
	}

	It("releases the finalizer without dropping for the retain policy", func() {
		role := newTestDatabaseRole()
		markDeleting(role)

		c, result := run(role, newTestCluster())
		Expect(result).To(Equal(ctrl.Result{}))
		expectFinalizerReleased(c, role)
	})

	It("drops an owned role and releases the finalizer for the delete policy", func() {
		role := newTestDatabaseRole()
		role.Spec.ReclaimPolicy = apiv1.DatabaseRoleReclaimDelete
		markReconciled(role)
		markDeleting(role)
		dbMock.ExpectExec(`DROP ROLE IF EXISTS "app_role"`).WillReturnResult(sqlmock.NewResult(0, 1))

		c, result := run(role, newTestCluster())
		Expect(result).To(Equal(ctrl.Result{}))
		expectFinalizerReleased(c, role)
	})

	It("does not drop a role shadowed by inline managed.roles", func() {
		role := newTestDatabaseRole()
		role.Spec.ReclaimPolicy = apiv1.DatabaseRoleReclaimDelete
		markReconciled(role)
		markDeleting(role)
		cluster := newTestCluster()
		shadowRole(cluster)

		c, result := run(role, cluster)
		Expect(result).To(Equal(ctrl.Result{}))
		expectFinalizerReleased(c, role)
	})

	It("does not drop a role on a replica cluster", func() {
		role := newTestDatabaseRole()
		role.Spec.ReclaimPolicy = apiv1.DatabaseRoleReclaimDelete
		markReconciled(role)
		markDeleting(role)
		cluster := newTestCluster()
		makeReplica(cluster)

		c, result := run(role, cluster)
		Expect(result).To(Equal(ctrl.Result{}))
		expectFinalizerReleased(c, role)
	})

	It("does not drop a role it never reconciled, releasing the finalizer", func() {
		role := newTestDatabaseRole()
		role.Spec.ReclaimPolicy = apiv1.DatabaseRoleReclaimDelete
		markDeleting(role)

		c, result := run(role, newTestCluster())
		Expect(result).To(Equal(ctrl.Result{}))
		expectFinalizerReleased(c, role)
	})

	It("keeps the finalizer and reports the error when the drop fails", func() {
		role := newTestDatabaseRole()
		role.Spec.ReclaimPolicy = apiv1.DatabaseRoleReclaimDelete
		markReconciled(role)
		markDeleting(role)
		dbMock.ExpectExec(`DROP ROLE IF EXISTS "app_role"`).
			WillReturnError(&pq.Error{
				Code:    "2BP01",
				Message: `role "app_role" cannot be dropped because some objects depend on it`,
			})

		c, result := run(role, newTestCluster())
		Expect(result.RequeueAfter).To(Equal(databaseRoleReconciliationInterval))

		got := &apiv1.DatabaseRole{}
		Expect(c.Get(context.Background(), client.ObjectKeyFromObject(role), got)).To(Succeed())
		Expect(got.Finalizers).To(ContainElement(utils.RoleFinalizerName))
		Expect(got.Status.Applied).To(HaveValue(BeFalse()))
		Expect(got.Status.Message).To(ContainSubstring("depend on it"))
	})
})

var _ = Describe("DatabaseRole mapClusterToDatabaseRoles", func() {
	var (
		r    *DatabaseRoleReconciler
		mine *apiv1.DatabaseRole
	)

	BeforeEach(func() {
		mine = newTestDatabaseRole()
		other := newTestDatabaseRole()
		other.Name = "role-cr-other-cluster"
		other.Spec.ClusterRef.Name = "another-cluster"
		// Same cluster name, different namespace: only the namespace guard
		// in mapClusterToDatabaseRoles keeps this one out.
		foreign := newTestDatabaseRole()
		foreign.Name = "role-cr-other-namespace"
		foreign.Namespace = "another-namespace"
		fakeClient := fake.NewClientBuilder().
			WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
			WithObjects(mine, other, foreign).
			Build()
		r = &DatabaseRoleReconciler{Client: fakeClient, instance: &fakeRoleInstance{}}
	})

	It("enqueues only the roles targeting this instance's cluster", func() {
		requests := r.mapClusterToDatabaseRoles(context.Background(), newTestCluster())
		Expect(requests).To(ConsistOf(reconcile.Request{
			NamespacedName: client.ObjectKeyFromObject(mine),
		}))
	})

	It("ignores other clusters and other namespaces", func() {
		otherCluster := newTestCluster()
		otherCluster.Name = "another-cluster"
		Expect(r.mapClusterToDatabaseRoles(context.Background(), otherCluster)).To(BeEmpty())

		otherNamespace := newTestCluster()
		otherNamespace.Namespace = "another-namespace"
		Expect(r.mapClusterToDatabaseRoles(context.Background(), otherNamespace)).To(BeEmpty())
	})
})
