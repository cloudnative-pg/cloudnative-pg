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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

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
		return &DatabaseRoleReconciler{Client: cli, Scheme: scheme}, cli
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
