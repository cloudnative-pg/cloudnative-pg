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
	"fmt"
	"strings"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/environment"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/run"
)

// ConfigureNamespacedDeployment configures an existing operator deployment for namespaced mode
func ConfigureNamespacedDeployment(env *environment.TestingEnvironment, namespace string) {
	ginkgo.By("patching deployment for namespaced mode", func() {
		var deployment appsv1.Deployment
		err := env.Client.Get(env.Ctx, types.NamespacedName{
			Namespace: namespace,
			Name:      "cnpg-controller-manager",
		}, &deployment)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		for i := range deployment.Spec.Template.Spec.Containers {
			deployment.Spec.Template.Spec.Containers[i].Env = append(
				deployment.Spec.Template.Spec.Containers[i].Env,
				corev1.EnvVar{Name: "WATCH_NAMESPACE", Value: namespace},
				corev1.EnvVar{Name: "NAMESPACED", Value: "true"},
			)
		}
		err = env.Client.Update(env.Ctx, &deployment)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.By("updating RBAC for namespaced mode", func() {
		var clusterRole rbacv1.ClusterRole
		err := env.Client.Get(env.Ctx, types.NamespacedName{Name: "cnpg-manager"}, &clusterRole)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		var roleRules, admissionRules []rbacv1.PolicyRule
		for _, rule := range clusterRole.Rules {
			var webhookAPIGroups, otherAPIGroups []string
			for _, apiGroup := range rule.APIGroups {
				if apiGroup == "admissionregistration.k8s.io" {
					webhookAPIGroups = append(webhookAPIGroups, apiGroup)
				} else {
					otherAPIGroups = append(otherAPIGroups, apiGroup)
				}
			}

			if len(webhookAPIGroups) > 0 {
				webhookRule := rule.DeepCopy()
				webhookRule.APIGroups = webhookAPIGroups
				admissionRules = append(admissionRules, *webhookRule)
			}
			if len(otherAPIGroups) > 0 {
				otherRule := rule.DeepCopy()
				otherRule.APIGroups = otherAPIGroups
				roleRules = append(roleRules, *otherRule)
			}
		}

		clusterRole.Rules = admissionRules
		err = env.Client.Update(env.Ctx, &clusterRole)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

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
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		}

		var originalCRB rbacv1.ClusterRoleBinding
		err = env.Client.Get(env.Ctx, types.NamespacedName{Name: "cnpg-manager-rolebinding"}, &originalCRB)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		for idx := range originalCRB.Subjects {
			originalCRB.Subjects[idx].Namespace = namespace
		}

		originalCRB.RoleRef.Kind = "ClusterRole"
		err = env.Client.Update(env.Ctx, &originalCRB)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

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
			Subjects: originalCRB.Subjects,
		}
		err = env.Client.Create(env.Ctx, roleBinding)
		if err != nil {
			err = env.Client.Update(env.Ctx, roleBinding)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		}
	})

	ginkgo.By("waiting for operator to restart", func() {
		_, stderr, err := run.Run(fmt.Sprintf(
			"kubectl rollout status -n %s deployment/cnpg-controller-manager --timeout=120s",
			namespace))
		gomega.Expect(err).NotTo(gomega.HaveOccurred(), "stderr: "+stderr)
	})
}

// RevertNamespacedDeployment reverts the operator deployment to cluster-wide mode
func RevertNamespacedDeployment(env *environment.TestingEnvironment, namespace string) {
	ginkgo.By("removing namespaced environment variables", func() {
		var deployment appsv1.Deployment
		err := env.Client.Get(env.Ctx, types.NamespacedName{
			Namespace: namespace,
			Name:      "cnpg-controller-manager",
		}, &deployment)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		for i := range deployment.Spec.Template.Spec.Containers {
			var filteredEnv []corev1.EnvVar
			for _, env := range deployment.Spec.Template.Spec.Containers[i].Env {
				if env.Name != "WATCH_NAMESPACE" && env.Name != "NAMESPACED" {
					filteredEnv = append(filteredEnv, env)
				}
			}
			deployment.Spec.Template.Spec.Containers[i].Env = filteredEnv
		}
		err = env.Client.Update(env.Ctx, &deployment)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.By("restoring cluster-wide RBAC", func() {
		var clusterRole rbacv1.ClusterRole
		err := env.Client.Get(env.Ctx, types.NamespacedName{Name: "cnpg-manager"}, &clusterRole)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		var role rbacv1.Role
		err = env.Client.Get(env.Ctx, types.NamespacedName{
			Namespace: namespace,
			Name:      "cnpg-manager",
		}, &role)
		if err == nil {
			clusterRole.Rules = append(clusterRole.Rules, role.Rules...)
		}

		err = env.Client.Update(env.Ctx, &clusterRole)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		err = env.Client.Delete(env.Ctx, &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "cnpg-manager-rolebinding"},
		})
		if err != nil && !strings.Contains(err.Error(), "not found") {
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		}

		crb := &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "cnpg-manager-rolebinding"},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "ClusterRole",
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
		err = env.Client.Create(env.Ctx, crb)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		err = env.Client.Delete(env.Ctx, &rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cnpg-manager-rolebinding",
				Namespace: namespace,
			},
		})
		if err != nil && !strings.Contains(err.Error(), "not found") {
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		}

		err = env.Client.Delete(env.Ctx, &role)
		if err != nil && !strings.Contains(err.Error(), "not found") {
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		}
	})

	ginkgo.By("waiting for operator to restart", func() {
		_, stderr, err := run.Run(fmt.Sprintf(
			"kubectl rollout status -n %s deployment/cnpg-controller-manager --timeout=120s",
			namespace))
		gomega.Expect(err).NotTo(gomega.HaveOccurred(), "stderr: "+stderr)
	})
}
