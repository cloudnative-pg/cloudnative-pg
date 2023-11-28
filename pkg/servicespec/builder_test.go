/*
Copyright The CloudNativePG Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package servicespec

import (
	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	corev1 "k8s.io/api/core/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Service template builder", func() {
	It("works without a Service template", func() {
		Expect(NewFrom(nil).status).ToNot(BeNil())
	})

	It("works with a Service template", func() {
		Expect(NewFrom(&apiv1.ServiceTemplateSpec{}).status).ToNot(BeNil())
	})

	It("adds annotations", func() {
		Expect(New().WithAnnotation("test", "annotation").Build().ObjectMeta.Annotations["test"]).
			To(Equal("annotation"))
	})

	It("adds labels", func() {
		Expect(New().WithLabel("test", "label").Build().ObjectMeta.Labels["label"]).
			To(Equal("label"))
	})

	It("sets service type", func() {
		Expect(New().WithServiceType(corev1.ServiceTypeLoadBalancer).Build().Spec.Type).To(Equal(corev1.ServiceTypeLoadBalancer))
	})
})
