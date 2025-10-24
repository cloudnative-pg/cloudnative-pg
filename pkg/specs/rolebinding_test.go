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

package specs

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Role binding", func() {
	cluster := apiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "thistest",
			Namespace: "default",
		},
	}

	It("is created with the same name as the cluster", func() {
		roleBinding := CreateRoleBinding(cluster)
		Expect(roleBinding.Name).To(Equal(cluster.Name))
		Expect(roleBinding.Namespace).To(Equal(cluster.Namespace))
		Expect(roleBinding.Subjects[0].Name).To(Equal(cluster.Name))
		Expect(roleBinding.RoleRef.Name).To(Equal(cluster.Name))
	})

	It("uses custom service account name when specified", func() {
		customSAName := "custom-sa"
		clusterWithCustomSA := apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "thistest",
				Namespace: "default",
			},
			Spec: apiv1.ClusterSpec{
				ServiceAccountTemplate: &apiv1.ServiceAccountTemplate{
					Metadata: apiv1.Metadata{
						Name: customSAName,
					},
				},
			},
		}

		roleBinding := CreateRoleBinding(clusterWithCustomSA)
		Expect(roleBinding.Name).To(Equal(clusterWithCustomSA.Name)) // RoleBinding name stays as cluster name
		Expect(roleBinding.Namespace).To(Equal(clusterWithCustomSA.Namespace))
		Expect(roleBinding.Subjects[0].Name).To(Equal(customSAName)) // Custom SA name
		Expect(roleBinding.RoleRef.Name).To(Equal(customSAName))     // Custom Role name
	})

})
