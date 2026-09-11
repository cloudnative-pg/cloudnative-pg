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
	"strings"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
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

var _ = Describe("DatabaseRole passwordSetToNull", func() {
	It("leaves the password alone when nothing asks for it to be set to NULL", func() {
		role := newTestDatabaseRole()
		Expect(passwordSetToNull(role)).To(BeFalse())

		role.Spec.Password = &apiv1.PasswordConfiguration{Mode: apiv1.PasswordModeGenerate}
		Expect(passwordSetToNull(role)).To(BeFalse())

		role.Spec.Password.Mode = apiv1.PasswordModeExternal
		Expect(passwordSetToNull(role)).To(BeFalse())
	})

	It("sets the password to NULL when the role asks for it", func() {
		role := newTestDatabaseRole()
		role.Spec.Password = &apiv1.PasswordConfiguration{Mode: apiv1.PasswordModeSetNull}

		Expect(passwordSetToNull(role)).To(BeTrue())
	})

	It("sets the password to NULL when disablePassword asks for it", func() {
		role := newTestDatabaseRole()
		role.Spec.DisablePassword = true

		Expect(passwordSetToNull(role)).To(BeTrue())
	})

	It("revokes a generated password nothing can read any more", func() {
		role := newTestDatabaseRole()
		role.Spec.Password = &apiv1.PasswordConfiguration{Mode: apiv1.PasswordModeExternal}
		role.Status.Password = &apiv1.GeneratedPasswordState{PendingRevocation: true}

		Expect(passwordSetToNull(role)).To(BeTrue())
	})

	It("leaves the password alone once the revocation was acknowledged", func() {
		role := newTestDatabaseRole()
		role.Spec.Password = &apiv1.PasswordConfiguration{Mode: apiv1.PasswordModeExternal}
		role.Status.Password = &apiv1.GeneratedPasswordState{}

		Expect(passwordSetToNull(role)).To(BeFalse())
	})

	It("does not revoke a password the role is about to read from a secret", func() {
		// The status can still carry a revocation while the spec has already
		// moved to a mode naming a Secret; applying both is an error.
		role := newTestDatabaseRole()
		role.Spec.Password = &apiv1.PasswordConfiguration{
			Mode:   apiv1.PasswordModeSecret,
			Secret: "role-secret",
		}
		role.Status.Password = &apiv1.GeneratedPasswordState{PendingRevocation: true}

		Expect(passwordSetToNull(role)).To(BeFalse())
	})
})

var _ = Describe("DatabaseRole generatedPasswordValidUntil", func() {
	rotatingRole := func(expiration string) *apiv1.DatabaseRole {
		role := newTestDatabaseRole()
		role.Spec.Password = &apiv1.PasswordConfiguration{
			Mode:     apiv1.PasswordModeGenerate,
			Duration: &metav1.Duration{Duration: 90 * 24 * time.Hour},
		}
		role.Status.Password = &apiv1.GeneratedPasswordState{Expiration: expiration}
		return role
	}

	It("has nothing to say about a role that does not generate a password", func() {
		role := newTestDatabaseRole()
		validUntil, err := generatedPasswordValidUntil(role)
		Expect(err).NotTo(HaveOccurred())
		Expect(validUntil.Valid).To(BeFalse())
	})

	It("has nothing to say about a generated password with no lifetime", func() {
		role := newTestDatabaseRole()
		role.Spec.Password = &apiv1.PasswordConfiguration{Mode: apiv1.PasswordModeGenerate}
		role.Status.Password = &apiv1.GeneratedPasswordState{
			Expiration: time.Now().UTC().Format(time.RFC3339),
		}

		// Without a duration the password never expires, so neither does the
		// role: an expiration recorded from an earlier specification must not
		// start expiring it.
		validUntil, err := generatedPasswordValidUntil(role)
		Expect(err).NotTo(HaveOccurred())
		Expect(validUntil.Valid).To(BeFalse())
	})

	It("waits for an expiration to be recorded before expiring the role", func() {
		role := rotatingRole("")
		validUntil, err := generatedPasswordValidUntil(role)
		Expect(err).NotTo(HaveOccurred())
		Expect(validUntil.Valid).To(BeFalse())

		role.Status.Password = nil
		validUntil, err = generatedPasswordValidUntil(role)
		Expect(err).NotTo(HaveOccurred())
		Expect(validUntil.Valid).To(BeFalse())
	})

	It("follows the expiration of the generated password", func() {
		expiration := time.Now().Add(90 * 24 * time.Hour).UTC().Truncate(time.Second)
		role := rotatingRole(expiration.Format(time.RFC3339))

		validUntil, err := generatedPasswordValidUntil(role)
		Expect(err).NotTo(HaveOccurred())
		Expect(validUntil.Valid).To(BeTrue())
		Expect(validUntil.Time).To(BeTemporally("==", expiration))
	})

	It("reports an expiration it cannot read instead of leaving the role unexpiring", func() {
		role := rotatingRole("not-a-timestamp")
		validUntil, err := generatedPasswordValidUntil(role)
		Expect(err).To(HaveOccurred())
		Expect(validUntil.Valid).To(BeFalse())
	})
})

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

	It("is false while a generated password is left to revoke", func() {
		// The operator records the revocation after the role stopped generating
		// a password, which can be after that same generation was applied:
		// going by the generation alone would leave the password in place for
		// good.
		role := newTestDatabaseRole()
		role.Spec.Password = &apiv1.PasswordConfiguration{Mode: apiv1.PasswordModeExternal}
		role.Status.ObservedGeneration = role.Generation
		role.Status.Password = &apiv1.GeneratedPasswordState{PendingRevocation: true}
		Expect(r.isAlreadyReconciled(role)).To(BeFalse())

		role.Status.Password.PendingRevocation = false
		Expect(r.isAlreadyReconciled(role)).To(BeTrue())
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

		It("follows the operator-generated secret when there is no passwordSecret", func() {
			role := newTestDatabaseRole()
			role.Spec.Password = &apiv1.PasswordConfiguration{Mode: apiv1.PasswordModeGenerate}
			role.Status.ObservedGeneration = role.Generation
			setObservedSecretVersion(role, "rv-2")
			role.Status.SecretResourceVersion = "rv-1"
			Expect(r.isAlreadyReconciled(role)).To(BeFalse())

			role.Status.SecretResourceVersion = "rv-2"
			Expect(r.isAlreadyReconciled(role)).To(BeTrue())
		})
	})
})

var _ = Describe("DatabaseRole succeededReconciliation", func() {
	It("acknowledges the revocation the apply it reports has just carried out", func() {
		role := newTestDatabaseRole()
		role.Spec.Password = &apiv1.PasswordConfiguration{Mode: apiv1.PasswordModeExternal}
		role.Status.Password = &apiv1.GeneratedPasswordState{PendingRevocation: true}
		fakeClient := fake.NewClientBuilder().
			WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
			WithObjects(role).
			WithStatusSubresource(&apiv1.DatabaseRole{}).
			Build()
		r := &DatabaseRoleReconciler{Client: fakeClient, instance: &fakeRoleInstance{}}

		_, err := r.succeededReconciliation(context.Background(), role, "")
		Expect(err).NotTo(HaveOccurred())

		// Without this the password would be set to NULL again on every loop,
		// and the operator would never retire the record of the revocation.
		var updated apiv1.DatabaseRole
		Expect(fakeClient.Get(context.Background(), client.ObjectKeyFromObject(role), &updated)).To(Succeed())
		Expect(updated.Status.Password).NotTo(BeNil())
		Expect(updated.Status.Password.PendingRevocation).To(BeFalse())
		Expect(updated.Status.Applied).To(HaveValue(BeTrue()))
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
		Entry("stops when the role is already reconciled and applied",
			func(role *apiv1.DatabaseRole, _ *apiv1.Cluster) {
				role.Status.ObservedGeneration = role.Generation
				role.Status.Applied = ptr.To(true)
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

	It("reports the conflict but keeps the recorded reconciliation when shadowed after a successful apply", func() {
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
		// The reconciliation is kept so a conflicting DatabaseRole cannot take
		// over the role while it is shadowed.
		Expect(got.Status.ObservedGeneration).To(Equal(got.Generation))
	})

	It("reports the replica condition but keeps the recorded reconciliation when the cluster is demoted", func() {
		role := newTestDatabaseRole()
		markReconciled(role)
		role.Status.Applied = ptr.To(true)
		cluster := newTestCluster()
		makeReplica(cluster)
		r := reconcilerFor(role)

		result, err := r.shouldReconcile(context.Background(), role, cluster)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(requeue))

		got := &apiv1.DatabaseRole{}
		Expect(r.Get(context.Background(), client.ObjectKeyFromObject(role), got)).To(Succeed())
		Expect(got.Status.Applied).To(BeNil())
		Expect(got.Status.Message).To(ContainSubstring("waiting for the cluster to become primary"))
		// Ownership is retained across the demotion: the role still counts as
		// reconciled, so a conflicting DatabaseRole created meanwhile cannot
		// take it over once the cluster is promoted back.
		Expect(got.Status.ObservedGeneration).To(Equal(got.Generation))
	})

	It("re-applies after a promotion, when a previous demotion left the role unapplied", func() {
		role := newTestDatabaseRole()
		markReconciled(role)
		role.Status.Applied = nil // left as Unknown by a previous demotion
		cluster := newTestCluster()
		r := reconcilerFor(role)

		result, err := r.shouldReconcile(context.Background(), role, cluster)
		Expect(err).NotTo(HaveOccurred())
		// A nil result asks the caller to proceed with the regular apply flow.
		Expect(result).To(BeNil())
	})

	It("keeps polling without writing on a replica while the cluster status is still settling", func() {
		role := newTestDatabaseRole()
		markReconciled(role)
		role.Status.Applied = ptr.To(true) // stale, not yet cleared
		cluster := newTestCluster()
		makeReplica(cluster)
		cluster.Status.CurrentPrimary = "other-pod" // this pod is not (yet) the designated primary
		r := reconcilerFor(role)

		result, err := r.shouldReconcile(context.Background(), role, cluster)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(requeue))

		// A non-designated pod must not write the status.
		got := &apiv1.DatabaseRole{}
		Expect(r.Get(context.Background(), client.ObjectKeyFromObject(role), got)).To(Succeed())
		Expect(got.Status.Applied).To(Equal(ptr.To(true)))
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

var _ = Describe("DatabaseRole isAlreadyReconciled", func() {
	// settled builds a role the instance manager has already applied: the
	// generation it observed, and the resource version of the password Secret
	// the condition announces.
	settled := func() *apiv1.DatabaseRole {
		role := newTestDatabaseRole()
		role.Spec.Password = &apiv1.PasswordConfiguration{
			Mode:     apiv1.PasswordModeGenerate,
			Duration: &metav1.Duration{Duration: 1008 * time.Hour},
		}
		role.Status.ObservedGeneration = role.Generation
		role.Status.SecretResourceVersion = "100"
		meta.SetStatusCondition(&role.Status.Conditions, metav1.Condition{
			Type:    string(apiv1.ConditionPasswordSecretChange),
			Status:  metav1.ConditionTrue,
			Reason:  "ChangeDetected",
			Message: "100",
		})
		return role
	}

	It("is reconciled once the applied expiration matches the recorded one", func() {
		role := settled()
		role.Status.Password = &apiv1.GeneratedPasswordState{
			SecretName:        "role-cr-password",
			Expiration:        "2026-11-16T09:12:44Z",
			AppliedExpiration: "2026-11-16T09:12:44Z",
		}

		r := &DatabaseRoleReconciler{}
		Expect(r.isAlreadyReconciled(role)).To(BeTrue())
	})

	It("is not reconciled while an expiration has not been applied yet", func() {
		// Adding a duration to a password that is not due for rotation changes
		// the expiration without touching the Secret, so neither the generation
		// nor the Secret's resource version says anything changed. The role's
		// VALID UNTIL follows that expiration, so it still has to be applied.
		role := settled()
		role.Status.Password = &apiv1.GeneratedPasswordState{
			SecretName: "role-cr-password",
			Expiration: "2026-11-16T09:12:44Z",
		}

		r := &DatabaseRoleReconciler{}
		Expect(r.isAlreadyReconciled(role)).To(BeFalse())
	})

	It("is not reconciled while a changed expiration has not been applied", func() {
		role := settled()
		role.Status.Password = &apiv1.GeneratedPasswordState{
			SecretName:        "role-cr-password",
			Expiration:        "2026-12-25T09:12:44Z",
			AppliedExpiration: "2026-11-16T09:12:44Z",
		}

		r := &DatabaseRoleReconciler{}
		Expect(r.isAlreadyReconciled(role)).To(BeFalse())
	})

	It("has no expiration to apply for a password with no lifetime", func() {
		role := settled()
		role.Spec.Password.Duration = nil
		role.Status.Password = &apiv1.GeneratedPasswordState{
			SecretName: "role-cr-password",
			// Left over from a lifetime that is no longer requested.
			Expiration: "2026-11-16T09:12:44Z",
		}

		r := &DatabaseRoleReconciler{}
		Expect(r.isAlreadyReconciled(role)).To(BeTrue())
	})
})

var _ = Describe("DatabaseRole reconcileRole VALID UNTIL", func() {
	var (
		db         *sql.DB
		dbMock     sqlmock.Sqlmock
		statements []string
	)

	// The columns pg_authid is listed with, so that List can return no rows and
	// reconcileRole takes the create path.
	listColumns := []string{
		"rolname", "rolsuper", "rolinherit", "rolcreaterole", "rolcreatedb",
		"rolcanlogin", "rolreplication", "rolconnlimit", "rolpassword",
		"rolvaliduntil", "rolbypassrls", "comment", "xmin", "inroles",
	}

	BeforeEach(func() {
		// Every statement is recorded and matched, so the assertions can be made
		// on the SQL itself: what matters here is a clause being absent as much
		// as being present, and RE2 has no way to say "does not contain".
		statements = nil
		recorder := sqlmock.QueryMatcherFunc(func(_, actualSQL string) error {
			statements = append(statements, actualSQL)
			return nil
		})

		var err error
		db, dbMock, err = sqlmock.New(sqlmock.QueryMatcherOption(recorder))
		Expect(err).NotTo(HaveOccurred())

		dbMock.ExpectQuery("").WillReturnRows(sqlmock.NewRows(listColumns))
		dbMock.ExpectBegin()
		dbMock.ExpectExec("").WillReturnResult(sqlmock.NewResult(0, 0))
		dbMock.ExpectExec("").WillReturnResult(sqlmock.NewResult(0, 0))
		dbMock.ExpectExec("").WillReturnResult(sqlmock.NewResult(0, 1))
		dbMock.ExpectCommit()
	})

	AfterEach(func() {
		Expect(dbMock.ExpectationsWereMet()).To(Succeed())
	})

	// createRoleStatement returns the CREATE ROLE the reconciliation issued.
	createRoleStatement := func() string {
		for _, statement := range statements {
			if strings.HasPrefix(statement, "CREATE ROLE") {
				return statement
			}
		}
		Fail("no CREATE ROLE statement was issued")
		return ""
	}

	run := func(role *apiv1.DatabaseRole) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "role-cr-password", Namespace: testNamespace},
			Type:       corev1.SecretTypeBasicAuth,
			Data: map[string][]byte{
				corev1.BasicAuthUsernameKey: []byte(testRoleName),
				corev1.BasicAuthPasswordKey: []byte("0mA3nCe0f6THe1dIvIne"),
			},
		}
		cli := fake.NewClientBuilder().
			WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
			WithObjects(role, secret).
			WithStatusSubresource(&apiv1.DatabaseRole{}).
			Build()
		r := &DatabaseRoleReconciler{Client: cli, instance: &fakeRoleInstance{db: db}}

		_, err := r.reconcileRole(context.Background(), role)
		Expect(err).NotTo(HaveOccurred())
	}

	generatingRole := func(duration *metav1.Duration, expiration string) *apiv1.DatabaseRole {
		role := newTestDatabaseRole()
		role.Spec.Password = &apiv1.PasswordConfiguration{
			Mode:     apiv1.PasswordModeGenerate,
			Duration: duration,
		}
		role.Status.Password = &apiv1.GeneratedPasswordState{
			SecretName: "role-cr-password",
			Expiration: expiration,
		}
		return role
	}

	It("sets VALID UNTIL from the expiration of the generated password", func() {
		expiration := time.Now().Add(1008 * time.Hour).UTC().Truncate(time.Second)
		run(generatingRole(&metav1.Duration{Duration: 1008 * time.Hour}, expiration.Format(time.RFC3339)))

		// The clause has to reach PostgreSQL, not just the DatabaseRole the
		// reconciler builds: the lifetime is only a deadline if the database
		// enforces it.
		Expect(createRoleStatement()).To(ContainSubstring(
			"VALID UNTIL '" + expiration.Format("2006-01-02 15:04:05")))
	})

	It("leaves the role unexpiring when the generated password has no lifetime", func() {
		// No duration means no expiration to follow, so nothing should be said
		// about VALID UNTIL even if the status still carries one.
		run(generatingRole(nil, time.Now().UTC().Format(time.RFC3339)))

		Expect(createRoleStatement()).NotTo(ContainSubstring("VALID UNTIL"))
	})

	It("leaves the role unexpiring until an expiration has been recorded", func() {
		run(generatingRole(&metav1.Duration{Duration: 1008 * time.Hour}, ""))

		Expect(createRoleStatement()).NotTo(ContainSubstring("VALID UNTIL"))
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
