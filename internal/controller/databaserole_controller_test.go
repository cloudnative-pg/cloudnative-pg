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
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("DatabaseRole operator-side controller", func() {
	ctx := context.Background()

	buildRoleReconciler := func(objs ...client.Object) (*DatabaseRoleReconciler, client.Client) {
		scheme := schemeBuilder.BuildWithAllKnownScheme()
		cli := fake.NewClientBuilder().
			WithScheme(scheme).
			WithStatusSubresource(&apiv1.DatabaseRole{}).
			WithObjects(objs...).
			Build()
		return &DatabaseRoleReconciler{Client: cli, Scheme: scheme, Recorder: record.NewFakeRecorder(eventBufferSize)}, cli
	}

	newRole := func(name, secretName string) *apiv1.DatabaseRole {
		role := &apiv1.DatabaseRole{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec: apiv1.DatabaseRoleSpec{
				RoleConfiguration: apiv1.RoleConfiguration{Name: name},
				ClusterRef:        corev1.LocalObjectReference{Name: "cluster-example"},
			},
		}
		if secretName != "" {
			role.Spec.PasswordSecret = &apiv1.LocalObjectReference{Name: secretName}
		}
		return role
	}

	newPasswordSecret := func(name string) *corev1.Secret {
		return &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Data:       map[string][]byte{"password": []byte("secret")},
		}
	}

	requestFor := func(role *apiv1.DatabaseRole) ctrl.Request {
		return ctrl.Request{NamespacedName: types.NamespacedName{Namespace: role.Namespace, Name: role.Name}}
	}

	passwordCondition := func(cli client.Client, role *apiv1.DatabaseRole) *metav1.Condition {
		got := &apiv1.DatabaseRole{}
		Expect(cli.Get(ctx, client.ObjectKeyFromObject(role), got)).To(Succeed())
		return meta.FindStatusCondition(got.Status.Conditions, string(apiv1.ConditionPasswordSecretChange))
	}

	It("records the secret resource version in the PasswordSecretChange condition", func() {
		secret := newPasswordSecret("role-secret")
		role := newRole("role-a", "role-secret")
		r, cli := buildRoleReconciler(role, secret)

		stored := &corev1.Secret{}
		Expect(cli.Get(ctx, client.ObjectKeyFromObject(secret), stored)).To(Succeed())

		_, err := r.Reconcile(ctx, requestFor(role))
		Expect(err).NotTo(HaveOccurred())

		cond := passwordCondition(cli, role)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionTrue))
		Expect(cond.Message).To(Equal(stored.ResourceVersion))
	})

	It("updates the condition when the secret resource version changes", func() {
		secret := newPasswordSecret("role-secret")
		role := newRole("role-a", "role-secret")
		r, cli := buildRoleReconciler(role, secret)

		_, err := r.Reconcile(ctx, requestFor(role))
		Expect(err).NotTo(HaveOccurred())
		firstMessage := passwordCondition(cli, role).Message

		// Rotating the password bumps the secret's resource version.
		stored := &corev1.Secret{}
		Expect(cli.Get(ctx, client.ObjectKeyFromObject(secret), stored)).To(Succeed())
		stored.Data["password"] = []byte("rotated")
		Expect(cli.Update(ctx, stored)).To(Succeed())

		_, err = r.Reconcile(ctx, requestFor(role))
		Expect(err).NotTo(HaveOccurred())

		cond := passwordCondition(cli, role)
		Expect(cond.Message).To(Equal(stored.ResourceVersion))
		Expect(cond.Message).NotTo(Equal(firstMessage))
	})

	It("clears a stale condition when the password secret is removed", func() {
		role := newRole("role-a", "")
		r, cli := buildRoleReconciler(role)

		// Seed a leftover condition from a previously configured secret.
		stored := &apiv1.DatabaseRole{}
		Expect(cli.Get(ctx, client.ObjectKeyFromObject(role), stored)).To(Succeed())
		meta.SetStatusCondition(&stored.Status.Conditions, metav1.Condition{
			Type:    string(apiv1.ConditionPasswordSecretChange),
			Status:  metav1.ConditionTrue,
			Reason:  "ChangeDetected",
			Message: "12345",
		})
		Expect(cli.Status().Update(ctx, stored)).To(Succeed())
		Expect(passwordCondition(cli, role)).NotTo(BeNil())

		_, err := r.Reconcile(ctx, requestFor(role))
		Expect(err).NotTo(HaveOccurred())

		Expect(passwordCondition(cli, role)).To(BeNil())
	})

	It("does nothing when the referenced secret does not exist yet", func() {
		role := newRole("role-a", "missing-secret")
		r, cli := buildRoleReconciler(role)

		_, err := r.Reconcile(ctx, requestFor(role))
		Expect(err).NotTo(HaveOccurred())

		Expect(passwordCondition(cli, role)).To(BeNil())
	})

	It("skips reconciliation when the role has a DeletionTimestamp", func() {
		secret := newPasswordSecret("role-secret")
		role := newRole("role-a", "role-secret")
		// A finalizer (e.g. from ArgoCD foreground pruning) keeps the role
		// around after deletion is requested.
		now := metav1.Now()
		role.Finalizers = []string{"cnpg.io/test-finalizer"}
		role.DeletionTimestamp = &now
		r, cli := buildRoleReconciler(role, secret)

		_, err := r.Reconcile(ctx, requestFor(role))
		Expect(err).NotTo(HaveOccurred())

		Expect(passwordCondition(cli, role)).To(BeNil())
	})

	It("getRolesUsingSecret returns only the roles referencing the given secret", func() {
		list := apiv1.DatabaseRoleList{Items: []apiv1.DatabaseRole{
			*newRole("uses-it", "shared-secret"),
			*newRole("uses-other", "other-secret"),
			*newRole("no-secret", ""),
		}}

		got := getRolesUsingSecret(list, newPasswordSecret("shared-secret"))
		Expect(got).To(ConsistOf(types.NamespacedName{Namespace: "default", Name: "uses-it"}))
	})
})

var _ = Describe("nextRoleSecretReconcile", func() {
	// The renewal deadline is the issue time plus the five minute lifetime,
	// minus the one minute renewal window, so it falls four minutes after the
	// password was issued.
	roleWithPassword := func(issuedAt string) *apiv1.DatabaseRole {
		return &apiv1.DatabaseRole{
			Spec: apiv1.DatabaseRoleSpec{
				Password: &apiv1.PasswordConfiguration{
					Mode:        apiv1.PasswordModeGenerate,
					Duration:    &metav1.Duration{Duration: 5 * time.Minute},
					RenewBefore: &metav1.Duration{Duration: time.Minute},
				},
			},
			Status: apiv1.DatabaseRoleStatus{
				Password: &apiv1.GeneratedPasswordState{IssuedAt: issuedAt},
			},
		}
	}

	It("targets the renewal deadline, not the fixed interval, when password rotation is enabled", func() {
		issuedAt := time.Now().Add(-3 * time.Minute).UTC().Format(time.RFC3339)
		role := roleWithPassword(issuedAt)
		Expect(nextRoleSecretReconcile(role)).To(BeNumerically("~", time.Minute, time.Second))
	})

	It("retries soon, rather than never, when the deadline cannot be read", func() {
		role := roleWithPassword("not-a-timestamp")
		Expect(nextRoleSecretReconcile(role)).To(Equal(roleSecretReconcileInterval))
	})

	It("backs off from a deadline the operator has explained it cannot honor", func() {
		// The message means the password can't be rotated, and won't clear on its own.
		issuedAt := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
		role := roleWithPassword(issuedAt)
		role.Status.Password.Message = "Secret \"role-dante-password\" already exists and is not owned"
		Expect(nextRoleSecretReconcile(role)).To(Equal(roleSecretReconcileInterval))
	})
})

var _ = Describe("DatabaseRole status patch", func() {
	ctx := context.Background()

	newRole := func() *apiv1.DatabaseRole {
		return &apiv1.DatabaseRole{
			ObjectMeta: metav1.ObjectMeta{Name: "role-a", Namespace: "default"},
			Spec: apiv1.DatabaseRoleSpec{
				RoleConfiguration: apiv1.RoleConfiguration{Name: "role-a"},
				ClusterRef:        corev1.LocalObjectReference{Name: "cluster-example"},
			},
			Status: apiv1.DatabaseRoleStatus{
				Password: &apiv1.GeneratedPasswordState{SecretName: "role-a-password"},
			},
		}
	}

	buildReconciler := func(
		role *apiv1.DatabaseRole,
		funcs interceptor.Funcs,
	) (*DatabaseRoleReconciler, client.Client) {
		scheme := schemeBuilder.BuildWithAllKnownScheme()
		cli := fake.NewClientBuilder().
			WithScheme(scheme).
			WithStatusSubresource(&apiv1.DatabaseRole{}).
			WithObjects(role).
			WithInterceptorFuncs(funcs).
			Build()
		return &DatabaseRoleReconciler{Client: cli, Scheme: scheme, Recorder: record.NewFakeRecorder(eventBufferSize)}, cli
	}

	It("stops without an error when the role is deleted while being reconciled", func() {
		stored := newRole()
		r, cli := buildReconciler(stored, interceptor.Funcs{})
		Expect(cli.Delete(ctx, stored)).To(Succeed())

		role := newRole()
		role.Status.Password.IssuedAt = "2026-08-20T10:00:00Z"
		Expect(r.patchRoleStatus(ctx, newRole(), role)).To(Succeed())
	})

	It("keeps what the instance manager wrote while the patch was in flight", func() {
		stored := newRole()
		concurrentWrites := 0
		r, cli := buildReconciler(stored, interceptor.Funcs{
			SubResourcePatch: func(
				ctx context.Context, c client.Client, subResource string,
				obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption,
			) error {
				// The instance manager lands between the read this patch is based
				// on and the patch itself, with a condition and the expiration it
				// applied: a merge patch of the whole status would wipe both out.
				if concurrentWrites == 0 {
					concurrentWrites++
					other := &apiv1.DatabaseRole{}
					Expect(c.Get(ctx, client.ObjectKeyFromObject(obj), other)).To(Succeed())
					meta.SetStatusCondition(&other.Status.Conditions, metav1.Condition{
						Type: "SomebodyElsesCondition", Status: metav1.ConditionTrue, Reason: "Whatever",
					})
					other.Status.Password.AppliedExpiration = "2026-09-01T10:00:00Z"
					Expect(c.Status().Update(ctx, other)).To(Succeed())
				}
				return c.SubResource(subResource).Patch(ctx, obj, patch, opts...)
			},
		})

		role := stored.DeepCopy()
		meta.SetStatusCondition(&role.Status.Conditions, metav1.Condition{
			Type: string(apiv1.ConditionPasswordSecretChange), Status: metav1.ConditionTrue,
			Reason: "SecretChanged", Message: "42",
		})
		role.Status.Password.IssuedAt = "2026-08-20T10:00:00Z"
		Expect(r.patchRoleStatus(ctx, stored, role)).To(Succeed())
		Expect(concurrentWrites).To(Equal(1))

		got := &apiv1.DatabaseRole{}
		Expect(cli.Get(ctx, client.ObjectKeyFromObject(role), got)).To(Succeed())
		Expect(meta.FindStatusCondition(got.Status.Conditions, "SomebodyElsesCondition")).NotTo(BeNil())
		Expect(meta.FindStatusCondition(got.Status.Conditions,
			string(apiv1.ConditionPasswordSecretChange))).NotTo(BeNil())
		Expect(got.Status.Password.IssuedAt).To(Equal("2026-08-20T10:00:00Z"))
		Expect(got.Status.Password.AppliedExpiration).To(Equal("2026-09-01T10:00:00Z"))
	})
})
