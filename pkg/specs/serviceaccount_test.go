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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Service accounts", func() {
	emptyMeta := metav1.ObjectMeta{}

	It("create a service account with the cluster name", func() {
		sa := &corev1.ServiceAccount{}
		err := UpdateServiceAccount(nil, sa)
		Expect(err).ToNot(HaveOccurred())
		Expect(sa.Annotations[utils.OperatorManagedSecretsAnnotationName]).To(Equal("null"))
	})

	It("correctly create the annotation storing the secret names", func() {
		sa := &corev1.ServiceAccount{}
		err := UpdateServiceAccount([]string{"one", "two"}, sa)
		Expect(err).ToNot(HaveOccurred())
		Expect(sa.Annotations[utils.OperatorManagedSecretsAnnotationName]).To(Equal(`["one","two"]`))
	})

	When("the pull secrets are changed", func() {
		It("can detect that the ServiceAccount is needing a refresh", func(ctx SpecContext) {
			sa := &corev1.ServiceAccount{}
			err := UpdateServiceAccount([]string{"one", "two"}, sa)
			Expect(err).ToNot(HaveOccurred())
			Expect(IsServiceAccountAligned(ctx, sa, []string{"one", "two"}, emptyMeta)).To(BeTrue())
			Expect(IsServiceAccountAligned(ctx, sa, []string{"one", "two", "three"}, emptyMeta)).To(BeFalse())
		})
	})

	When("there are secrets not directly managed by the operator", func() {
		It("can detect that the ServiceAccount is needing a refresh", func(ctx SpecContext) {
			sa := &corev1.ServiceAccount{}
			err := UpdateServiceAccount([]string{"one", "two"}, sa)

			// This image pull secret is not managed by the operator since its name
			// has not been stored inside the annotation inside the ServiceAccount
			sa.ImagePullSecrets = append(sa.ImagePullSecrets, corev1.LocalObjectReference{
				Name: "token",
			})
			Expect(err).ToNot(HaveOccurred())

			Expect(IsServiceAccountAligned(ctx, sa, []string{"one", "two"}, emptyMeta)).To(BeTrue())
			Expect(IsServiceAccountAligned(ctx, sa, []string{"one", "two", "three"}, emptyMeta)).To(BeFalse())
		})
	})

	When("there are custom labels to set on the ServiceAccount", func() {
		It("can detect if the ServiceAccount is needing a refresh", func(ctx SpecContext) {
			meta := metav1.ObjectMeta{
				Labels: map[string]string{
					"test": "value",
				},
			}
			updatedMeta := metav1.ObjectMeta{
				Labels: map[string]string{
					"test": "valueChanged",
				},
			}

			sa := &corev1.ServiceAccount{
				ObjectMeta: meta,
			}
			err := UpdateServiceAccount([]string{}, sa)
			Expect(err).ToNot(HaveOccurred())

			Expect(IsServiceAccountAligned(ctx, sa, nil, meta)).To(BeTrue())
			Expect(IsServiceAccountAligned(ctx, sa, nil, updatedMeta)).To(BeFalse())
		})
	})

	When("there are custom annotations to be set on the ServiceAccount", func() {
		It("can detect if the ServiceAccount is needing a refresh", func(ctx SpecContext) {
			meta := metav1.ObjectMeta{
				Annotations: map[string]string{
					"test": "value",
				},
			}
			updatedMeta := metav1.ObjectMeta{
				Annotations: map[string]string{
					"test": "valueChanged",
				},
			}

			sa := &corev1.ServiceAccount{
				ObjectMeta: meta,
			}
			err := UpdateServiceAccount([]string{}, sa)
			Expect(err).ToNot(HaveOccurred())

			Expect(IsServiceAccountAligned(ctx, sa, nil, meta)).To(BeTrue())
			Expect(IsServiceAccountAligned(ctx, sa, nil, updatedMeta)).To(BeFalse())
		})
	})
})
