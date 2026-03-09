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

package namespaced

import (
	"slices"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/environment"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/operator"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// RBACRestrictionOptions defines which RBAC resources to restrict
type RBACRestrictionOptions struct {
	RemoveNodeAccess                bool
	RemoveClusterImageCatalogAccess bool // TODO in separate commit
}

// defaultClusterRoleRules stores the original cluster role rules for restoration
var defaultClusterRoleRules []rbacv1.PolicyRule

// removeResourceFromRules removes a specific resource from all rules
func removeResourceFromRules(rules []rbacv1.PolicyRule, resource string) []rbacv1.PolicyRule {
	var filtered []rbacv1.PolicyRule
	for _, rule := range rules {
		var filteredResources []string
		for _, r := range rule.Resources {
			if r != resource {
				filteredResources = append(filteredResources, r)
			}
		}
		if len(filteredResources) > 0 {
			rule.Resources = filteredResources
			filtered = append(filtered, rule)
		}
	}
	return filtered
}

// removeClusterImageCatalogsFromRules removes clusterimagecatalogs from all rules
func removeClusterImageCatalogsFromRules(rules []rbacv1.PolicyRule) []rbacv1.PolicyRule {
	return removeResourceFromRules(rules, "clusterimagecatalogs")
}

// removeNodesFromRules removes nodes from all rules
func removeNodesFromRules(rules []rbacv1.PolicyRule) []rbacv1.PolicyRule {
	return removeResourceFromRules(rules, "nodes")
}

// splitRBACRulesForNamespacedDeployment splits cluster role rules into cluster role and namespaced role rules
func splitRBACRulesForNamespacedDeployment(clusterRole *rbacv1.ClusterRole) ([]rbacv1.PolicyRule, []rbacv1.PolicyRule) {
	var roleRules, clusterRoleRules []rbacv1.PolicyRule

	for _, rule := range clusterRole.Rules {
		// Keep admission controller in cluster role
		if slices.Contains(rule.APIGroups, "admissionregistration.k8s.io") {
			clusterRoleRules = append(clusterRoleRules, *rule.DeepCopy())
			continue
		}

		// Keep nodes in cluster role
		if slices.Contains(rule.Resources, "nodes") {
			clusterRoleRules = append(clusterRoleRules, *rule.DeepCopy())
			continue
		}

		// Split imagecatalog and clusterimagecatalog into separate roles
		if slices.Contains(rule.APIGroups, "postgresql.cnpg.io") &&
			slices.Contains(rule.Resources, "imagecatalogs") && slices.Contains(rule.Resources, "clusterimagecatalogs") {
			// clusterimagecatalog stays in cluster role
			clusterRule := rule.DeepCopy()
			clusterRule.Resources = []string{"clusterimagecatalogs"}
			clusterRoleRules = append(clusterRoleRules, *clusterRule)

			// imagecatalog goes to role
			nsRule := rule.DeepCopy()
			nsRule.Resources = []string{"imagecatalogs"}
			roleRules = append(roleRules, *nsRule)
			continue
		}

		// Add remaining resources to role
		nsRule := rule.DeepCopy()
		roleRules = append(roleRules, *nsRule)
	}

	return roleRules, clusterRoleRules
}

// StoreDefaultRBACSpec captures the default RBAC spec before modifications
func StoreDefaultRBACSpec(env *environment.TestingEnvironment) {
	var clusterRole rbacv1.ClusterRole
	err := env.Client.Get(env.Ctx, types.NamespacedName{Name: "cnpg-manager"}, &clusterRole)
	Expect(err).NotTo(HaveOccurred())
	defaultClusterRoleRules = make([]rbacv1.PolicyRule, len(clusterRole.Rules))
	for i, rule := range clusterRole.Rules {
		defaultClusterRoleRules[i] = *rule.DeepCopy()
	}
}

// ConfigureOperatorWithoutNodeAccess configures an existing operator deployment for namespaced mode
func ConfigureOperatorWithoutNodeAccess(env *environment.TestingEnvironment, namespace string, timeout uint) {
	ConfigureOperatorWithRBACRestrictions(env, namespace, RBACRestrictionOptions{
		RemoveNodeAccess: true,
	}, timeout)
}

// ConfigureOperatorWithRBACRestrictions configures RBAC restrictions based on provided options
func ConfigureOperatorWithRBACRestrictions(
	env *environment.TestingEnvironment,
	namespace string,
	opts RBACRestrictionOptions,
	timeout uint,
) {
	By("patching deployment for namespaced mode", func() {
		var deployment appsv1.Deployment
		err := env.Client.Get(env.Ctx, types.NamespacedName{
			Namespace: namespace,
			Name:      "cnpg-controller-manager",
		}, &deployment)
		Expect(err).NotTo(HaveOccurred())

		for i := range deployment.Spec.Template.Spec.Containers {
			deployment.Spec.Template.Spec.Containers[i].Env = append(
				deployment.Spec.Template.Spec.Containers[i].Env,
				corev1.EnvVar{Name: "WATCH_NAMESPACE", Value: namespace},
				corev1.EnvVar{Name: "WATCH_NODES", Value: strconv.FormatBool(!opts.RemoveNodeAccess)},
			)
		}
		err = env.Client.Update(env.Ctx, &deployment)
		Expect(err).NotTo(HaveOccurred())
	})

	By("updating RBAC", func() {
		var clusterRole rbacv1.ClusterRole
		err := env.Client.Get(env.Ctx, types.NamespacedName{Name: "cnpg-manager"}, &clusterRole)
		Expect(err).NotTo(HaveOccurred())

		roleRules, clusterRoleRules := splitRBACRulesForNamespacedDeployment(&clusterRole)

		if opts.RemoveClusterImageCatalogAccess {
			clusterRoleRules = removeClusterImageCatalogsFromRules(clusterRoleRules)
		}

		if opts.RemoveNodeAccess {
			clusterRoleRules = removeNodesFromRules(clusterRoleRules)
		}

		clusterRole.Rules = clusterRoleRules
		err = env.Client.Update(env.Ctx, &clusterRole)
		Expect(err).NotTo(HaveOccurred())

		role := &rbacv1.Role{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cnpg-manager",
				Namespace: namespace,
			},
			Rules: roleRules,
		}
		err = env.Client.Create(env.Ctx, role)
		if err != nil {
			err = env.Client.Update(env.Ctx, role)
			Expect(err).NotTo(HaveOccurred())
		}

		roleBinding := &rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cnpg-manager-rolebinding",
				Namespace: namespace,
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "Role",
				Name:     "cnpg-manager",
			},
			Subjects: []rbacv1.Subject{
				{
					Kind:      "ServiceAccount",
					Name:      "cnpg-manager",
					Namespace: namespace,
				},
			},
		}
		err = env.Client.Create(env.Ctx, roleBinding)
		if err != nil {
			err = env.Client.Update(env.Ctx, roleBinding)
			Expect(err).NotTo(HaveOccurred())
		}
	})

	By("waiting for namespaced operator deployment to be ready", func() {
		Expect(operator.WaitForReady(env.Ctx, env.Client, timeout, false)).Should(Succeed())
	})
}

// RevertOperatorToClusterWideMode reverts the operator to cluster-wide mode with full permissions
func RevertOperatorToClusterWideMode(env *environment.TestingEnvironment, namespace string, timeout uint) {
	By("restoring cluster-wide RBAC", func() {
		var clusterRole rbacv1.ClusterRole
		err := env.Client.Get(env.Ctx, types.NamespacedName{Name: "cnpg-manager"}, &clusterRole)
		Expect(err).NotTo(HaveOccurred())

		clusterRole.Rules = make([]rbacv1.PolicyRule, len(defaultClusterRoleRules))
		for i, rule := range defaultClusterRoleRules {
			clusterRole.Rules[i] = *rule.DeepCopy()
		}

		err = env.Client.Update(env.Ctx, &clusterRole)
		Expect(err).NotTo(HaveOccurred())

		// Verify the update
		err = env.Client.Get(env.Ctx, types.NamespacedName{Name: "cnpg-manager"}, &clusterRole)
		Expect(err).NotTo(HaveOccurred())

		// Delete existing role binding and role
		err = env.Client.Delete(env.Ctx, &rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cnpg-manager-rolebinding",
				Namespace: namespace,
			},
		})
		if err != nil && !strings.Contains(err.Error(), "not found") {
			Expect(err).NotTo(HaveOccurred())
		}

		err = env.Client.Delete(env.Ctx, &rbacv1.Role{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cnpg-manager",
				Namespace: namespace,
			},
		})
		if err != nil && !strings.Contains(err.Error(), "not found") {
			Expect(err).NotTo(HaveOccurred())
		}
	})
	By("removing cluster-wide object access restrictions", func() {
		var deployment appsv1.Deployment
		err := env.Client.Get(env.Ctx, types.NamespacedName{
			Namespace: namespace,
			Name:      "cnpg-controller-manager",
		}, &deployment)
		Expect(err).NotTo(HaveOccurred())

		for i := range deployment.Spec.Template.Spec.Containers {
			var filteredEnv []corev1.EnvVar
			for _, env := range deployment.Spec.Template.Spec.Containers[i].Env {
				if env.Name != "WATCH_NAMESPACE" && env.Name != "WATCH_NODES" {
					filteredEnv = append(filteredEnv, env)
				}
			}
			deployment.Spec.Template.Spec.Containers[i].Env = filteredEnv
		}
		err = env.Client.Update(env.Ctx, &deployment)
		Expect(err).NotTo(HaveOccurred())
	})

	By("waiting for operator deployment to be ready", func() {
		Expect(operator.WaitForReady(env.Ctx, env.Client, timeout, true)).Should(Succeed())
	})
}
