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

// Package namespaced provides utilities for configuring namespaced operator deployments
package namespaced

import (
	"fmt"

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

// ConfigureDeployment configures an existing operator deployment for namespaced mode.
// It patches the deployment to set WATCH_NAMESPACE and NAMESPACED env vars, updates RBAC
// to use a namespaced Role instead of ClusterRole, and waits for the operator to restart.
func ConfigureDeployment(env *environment.TestingEnvironment, namespace string) {
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
