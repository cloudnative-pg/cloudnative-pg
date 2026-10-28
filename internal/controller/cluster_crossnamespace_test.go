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

	rbacv1 "k8s.io/api/rbac/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	k8client "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("cross-namespace DatabaseRole password Secrets access", func() {
	const (
		clusterNamespace = "postgres"
		tenantNamespace  = "tenant"
		otherTenant      = "other-tenant"
	)

	var (
		ctx        context.Context
		fakeClient k8client.Client
		reconciler *ClusterReconciler
		cluster    *apiv1.Cluster
		rbacName   string
	)

	newRole := func(namespace, name, secret string) *apiv1.DatabaseRole {
		role := &apiv1.DatabaseRole{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			Spec: apiv1.DatabaseRoleSpec{
				RoleConfiguration: apiv1.RoleConfiguration{Name: name},
				ClusterRef: apiv1.ClusterObjectReference{
					Name:      cluster.Name,
					Namespace: cluster.Namespace,
				},
			},
		}
		if secret != "" {
			role.Spec.PasswordSecret = &apiv1.LocalObjectReference{Name: secret}
		}
		return role
	}

	getRole := func(namespace string) (*rbacv1.Role, error) {
		var role rbacv1.Role
		err := fakeClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: rbacName}, &role)
		return &role, err
	}

	getRoleBinding := func(namespace string) (*rbacv1.RoleBinding, error) {
		var binding rbacv1.RoleBinding
		err := fakeClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: rbacName}, &binding)
		return &binding, err
	}

	secretsRule := func(names ...string) []rbacv1.PolicyRule {
		return []rbacv1.PolicyRule{{
			APIGroups:     []string{""},
			Resources:     []string{"secrets"},
			Verbs:         []string{"get", "watch"},
			ResourceNames: names,
		}}
	}

	BeforeEach(func() {
		ctx = context.Background()
		fakeClient = newClusterIndexedFakeClientBuilder(schemeBuilder.BuildWithAllKnownScheme()).Build()
		reconciler = &ClusterReconciler{
			Client:   fakeClient,
			Recorder: record.NewFakeRecorder(10000),
			Scheme:   schemeBuilder.BuildWithAllKnownScheme(),
		}
		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test-cluster", Namespace: clusterNamespace},
			Spec: apiv1.ClusterSpec{
				Instances:                     1,
				EnableCrossNamespaceResources: true,
			},
		}
		rbacName = specs.GetCrossNamespaceRoleName(*cluster)
	})

	It("grants access to exactly the password Secrets of the roles in the tenant namespace", func() {
		Expect(fakeClient.Create(ctx, newRole(tenantNamespace, "alice", "alice-secret"))).To(Succeed())
		Expect(fakeClient.Create(ctx, newRole(tenantNamespace, "bob", "bob-secret"))).To(Succeed())
		noPassword := newRole(tenantNamespace, "carol", "carol-secret")
		noPassword.Spec.DisablePassword = true
		Expect(fakeClient.Create(ctx, noPassword)).To(Succeed())

		Expect(reconciler.reconcileCrossNamespaceRBAC(ctx, cluster)).To(Succeed())

		role, err := getRole(tenantNamespace)
		Expect(err).NotTo(HaveOccurred())
		Expect(role.Rules).To(Equal(secretsRule("alice-secret", "bob-secret")))
		Expect(specs.IsCrossNamespaceRBACOwnedBy(role.Labels, cluster.Namespace, cluster.Name)).To(BeTrue())

		binding, err := getRoleBinding(tenantNamespace)
		Expect(err).NotTo(HaveOccurred())
		Expect(specs.IsCrossNamespaceRBACOwnedBy(binding.Labels, cluster.Namespace, cluster.Name)).To(BeTrue())
		Expect(binding.RoleRef).To(Equal(rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     rbacName,
		}))
		Expect(binding.Subjects).To(ConsistOf(rbacv1.Subject{
			Kind:      "ServiceAccount",
			Name:      cluster.Name,
			Namespace: cluster.Namespace,
		}))
	})

	It("does not create a Role in the cluster namespace", func() {
		Expect(fakeClient.Create(ctx, newRole(clusterNamespace, "alice", "alice-secret"))).To(Succeed())

		Expect(reconciler.reconcileCrossNamespaceRBAC(ctx, cluster)).To(Succeed())

		_, err := getRole(clusterNamespace)
		Expect(apierrs.IsNotFound(err)).To(BeTrue())
	})

	It("ignores the roles of other clusters", func() {
		foreign := newRole(tenantNamespace, "alice", "alice-secret")
		foreign.Spec.ClusterRef.Name = "another-cluster"
		Expect(fakeClient.Create(ctx, foreign)).To(Succeed())

		Expect(reconciler.reconcileCrossNamespaceRBAC(ctx, cluster)).To(Succeed())

		_, err := getRole(tenantNamespace)
		Expect(apierrs.IsNotFound(err)).To(BeTrue())
	})

	It("does not grant access to every Secret when no role of the namespace has a password", func() {
		noPassword := newRole(tenantNamespace, "carol", "carol-secret")
		noPassword.Spec.DisablePassword = true
		Expect(fakeClient.Create(ctx, noPassword)).To(Succeed())

		Expect(reconciler.reconcileCrossNamespaceRBAC(ctx, cluster)).To(Succeed())

		_, err := getRole(tenantNamespace)
		Expect(apierrs.IsNotFound(err)).To(BeTrue())
		_, err = getRoleBinding(tenantNamespace)
		Expect(apierrs.IsNotFound(err)).To(BeTrue())
	})

	It("does nothing when the feature is disabled", func() {
		cluster.Spec.EnableCrossNamespaceResources = false
		Expect(fakeClient.Create(ctx, newRole(tenantNamespace, "alice", "alice-secret"))).To(Succeed())

		Expect(reconciler.reconcileCrossNamespaceRBAC(ctx, cluster)).To(Succeed())

		_, err := getRole(tenantNamespace)
		Expect(apierrs.IsNotFound(err)).To(BeTrue())
	})

	It("updates the Secret names as roles are added and removed", func() {
		alice := newRole(tenantNamespace, "alice", "alice-secret")
		Expect(fakeClient.Create(ctx, alice)).To(Succeed())
		Expect(reconciler.reconcileCrossNamespaceRBAC(ctx, cluster)).To(Succeed())

		Expect(fakeClient.Create(ctx, newRole(tenantNamespace, "bob", "bob-secret"))).To(Succeed())
		Expect(reconciler.reconcileCrossNamespaceRBAC(ctx, cluster)).To(Succeed())
		role, err := getRole(tenantNamespace)
		Expect(err).NotTo(HaveOccurred())
		Expect(role.Rules).To(Equal(secretsRule("alice-secret", "bob-secret")))

		Expect(fakeClient.Delete(ctx, alice)).To(Succeed())
		Expect(reconciler.reconcileCrossNamespaceRBAC(ctx, cluster)).To(Succeed())
		role, err = getRole(tenantNamespace)
		Expect(err).NotTo(HaveOccurred())
		Expect(role.Rules).To(Equal(secretsRule("bob-secret")))
	})

	It("removes the Role and the RoleBinding when the last role of the namespace goes", func() {
		alice := newRole(tenantNamespace, "alice", "alice-secret")
		Expect(fakeClient.Create(ctx, alice)).To(Succeed())
		Expect(fakeClient.Create(ctx, newRole(otherTenant, "bob", "bob-secret"))).To(Succeed())
		Expect(reconciler.reconcileCrossNamespaceRBAC(ctx, cluster)).To(Succeed())

		Expect(fakeClient.Delete(ctx, alice)).To(Succeed())
		Expect(reconciler.reconcileCrossNamespaceRBAC(ctx, cluster)).To(Succeed())

		_, err := getRole(tenantNamespace)
		Expect(apierrs.IsNotFound(err)).To(BeTrue())
		_, err = getRoleBinding(tenantNamespace)
		Expect(apierrs.IsNotFound(err)).To(BeTrue())

		_, err = getRole(otherTenant)
		Expect(err).NotTo(HaveOccurred())
		_, err = getRoleBinding(otherTenant)
		Expect(err).NotTo(HaveOccurred())
	})

	It("restores the rules and the subjects that were changed", func() {
		Expect(fakeClient.Create(ctx, newRole(tenantNamespace, "alice", "alice-secret"))).To(Succeed())
		Expect(reconciler.reconcileCrossNamespaceRBAC(ctx, cluster)).To(Succeed())

		role, err := getRole(tenantNamespace)
		Expect(err).NotTo(HaveOccurred())
		role.Rules = secretsRule("alice-secret", "stolen")
		Expect(fakeClient.Update(ctx, role)).To(Succeed())
		binding, err := getRoleBinding(tenantNamespace)
		Expect(err).NotTo(HaveOccurred())
		binding.Subjects = append(binding.Subjects, rbacv1.Subject{
			Kind: "ServiceAccount", Name: "intruder", Namespace: tenantNamespace,
		})
		Expect(fakeClient.Update(ctx, binding)).To(Succeed())

		Expect(reconciler.reconcileCrossNamespaceRBAC(ctx, cluster)).To(Succeed())

		role, err = getRole(tenantNamespace)
		Expect(err).NotTo(HaveOccurred())
		Expect(role.Rules).To(Equal(secretsRule("alice-secret")))
		binding, err = getRoleBinding(tenantNamespace)
		Expect(err).NotTo(HaveOccurred())
		Expect(binding.Subjects).To(ConsistOf(rbacv1.Subject{
			Kind: "ServiceAccount", Name: cluster.Name, Namespace: cluster.Namespace,
		}))
	})

	It("does not touch a Role or a RoleBinding not created for the cluster", func() {
		Expect(fakeClient.Create(ctx, newRole(tenantNamespace, "alice", "alice-secret"))).To(Succeed())
		foreignRole := &rbacv1.Role{
			ObjectMeta: metav1.ObjectMeta{Name: rbacName, Namespace: tenantNamespace},
			Rules:      secretsRule("something-else"),
		}
		Expect(fakeClient.Create(ctx, foreignRole)).To(Succeed())
		foreignBinding := &rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: rbacName, Namespace: tenantNamespace},
			RoleRef:    rbacv1.RoleRef{APIGroup: "rbac.authorization.k8s.io", Kind: "Role", Name: "other"},
		}
		Expect(fakeClient.Create(ctx, foreignBinding)).To(Succeed())

		// A tenant must not be able to block the reconciliation of the
		// cluster by squatting the name of the Role in its namespace
		Expect(reconciler.reconcileCrossNamespaceRBAC(ctx, cluster)).To(Succeed())

		role, err := getRole(tenantNamespace)
		Expect(err).NotTo(HaveOccurred())
		Expect(role.Rules).To(Equal(secretsRule("something-else")))
		binding, err := getRoleBinding(tenantNamespace)
		Expect(err).NotTo(HaveOccurred())
		Expect(binding.RoleRef.Name).To(Equal("other"))
		Expect(binding.Subjects).To(BeEmpty())

		// Nor delete them when the last role goes away
		Expect(fakeClient.DeleteAllOf(ctx, &apiv1.DatabaseRole{}, k8client.InNamespace(tenantNamespace))).To(Succeed())
		Expect(reconciler.reconcileCrossNamespaceRBAC(ctx, cluster)).To(Succeed())
		Expect(reconciler.deleteCrossNamespaceRBACByKey(ctx, k8client.ObjectKeyFromObject(cluster), nil)).
			To(Succeed())
		_, err = getRole(tenantNamespace)
		Expect(err).NotTo(HaveOccurred())
		_, err = getRoleBinding(tenantNamespace)
		Expect(err).NotTo(HaveOccurred())
	})

	It("removes every Role and RoleBinding when the cluster is deleted", func() {
		Expect(fakeClient.Create(ctx, newRole(tenantNamespace, "alice", "alice-secret"))).To(Succeed())
		Expect(fakeClient.Create(ctx, newRole(otherTenant, "bob", "bob-secret"))).To(Succeed())
		Expect(reconciler.reconcileCrossNamespaceRBAC(ctx, cluster)).To(Succeed())

		// The roles may still be around, held by their finalizers
		Expect(reconciler.deleteCrossNamespaceRBACByKey(ctx, k8client.ObjectKeyFromObject(cluster), nil)).
			To(Succeed())

		for _, namespace := range []string{tenantNamespace, otherTenant} {
			_, err := getRole(namespace)
			Expect(apierrs.IsNotFound(err)).To(BeTrue())
			_, err = getRoleBinding(namespace)
			Expect(apierrs.IsNotFound(err)).To(BeTrue())
		}
	})
})
