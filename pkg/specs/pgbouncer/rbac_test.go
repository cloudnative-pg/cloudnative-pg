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

package pgbouncer

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Pooler ServiceAccount, Role, and RoleBinding", func() {
	var pooler *apiv1.Pooler

	BeforeEach(func() {
		pooler = &apiv1.Pooler{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pooler",
				Namespace: "test-namespace",
			},
			Status: apiv1.PoolerStatus{
				Secrets: &apiv1.PoolerSecrets{},
			},
		}
	})

	Context("when creating a ServiceAccount", func() {
		It("returns the correct ServiceAccount", func() {
			serviceAccount := ServiceAccount(pooler)
			Expect(serviceAccount.Name).To(Equal(pooler.Name))
			Expect(serviceAccount.Namespace).To(Equal(pooler.Namespace))
		})
	})

	Context("when creating a Role", func() {
		It("returns the correct Role", func() {
			role := Role(pooler)
			Expect(role.Name).To(Equal(pooler.Name))
			Expect(role.Namespace).To(Equal(pooler.Namespace))
			Expect(role.Rules).To(HaveLen(3))
			Expect(role.Rules[0].APIGroups).To(ContainElement("postgresql.cnpg.io"))
			Expect(role.Rules[0].Resources).To(ContainElement("poolers"))
			Expect(role.Rules[0].Verbs).To(ConsistOf("get", "watch"))
			Expect(role.Rules[0].ResourceNames).To(ContainElement(pooler.Name))
			Expect(role.Rules[2].APIGroups).To(ContainElement(""))
			Expect(role.Rules[2].Resources).To(ContainElement("secrets"))
			Expect(role.Rules[2].Verbs).To(ConsistOf("get", "watch"))
		})
	})

	Context("when creating a RoleBinding", func() {
		It("returns the correct RoleBinding", func() {
			roleBinding := RoleBinding(pooler, pooler.GetServiceAccountName())
			Expect(roleBinding.Name).To(Equal(pooler.Name))
			Expect(roleBinding.Namespace).To(Equal(pooler.Namespace))
		})
	})
})
