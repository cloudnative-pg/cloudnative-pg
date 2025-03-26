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

package resources

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("RetryWithRefreshedResource", func() {
	const (
		name      = "test-deployment"
		namespace = "default"
	)

	var (
		fakeClient   client.Client
		testResource *appsv1.Deployment
	)

	BeforeEach(func() {
		fakeClient = fake.NewClientBuilder().WithScheme(schemeBuilder.BuildWithAllKnownScheme()).Build()
		testResource = &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			Spec: appsv1.DeploymentSpec{
				Replicas: ptr.To(int32(1)),
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "test"},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "test"}},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "test-container",
								Image: "nginx",
							},
						},
					},
				},
			},
		}
	})

	Context("when client.Get succeeds", func() {
		BeforeEach(func(ctx SpecContext) {
			// Set up the fake client to return the resource without error
			Expect(fakeClient.Create(ctx, testResource)).To(Succeed())

			modified := testResource.DeepCopy()
			modified.Spec.Replicas = ptr.To(int32(10))
			err := fakeClient.Update(ctx, modified)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should invoke the callback without error and update the resource", func(ctx SpecContext) {
			// ensure that the local deployment contains the old value
			Expect(*testResource.Spec.Replicas).To(Equal(int32(1)))

			cb := func() error {
				return nil
			}

			// ensure that now the deployment contains the new value
			err := RetryWithRefreshedResource(ctx, fakeClient, testResource, cb)
			Expect(err).ToNot(HaveOccurred())
			Expect(*testResource.Spec.Replicas).To(Equal(int32(10)))
		})
	})
})
