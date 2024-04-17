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
	corev1 "k8s.io/api/core/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

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
		Expect(New().WithLabel("test", "label").Build().ObjectMeta.Labels["test"]).
			To(Equal("label"))
	})

	It("sets service type", func() {
		Expect(New().WithServiceType(corev1.ServiceTypeLoadBalancer, true).Build().Spec.Type).
			To(Equal(corev1.ServiceTypeLoadBalancer))
	})

	It("updates pgbouncer port", func() {
		Expect(NewFrom(&apiv1.ServiceTemplateSpec{
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{
					{
						Name: "pgbouncer",
						Port: 5432,
					},
				},
			},
		}).WithServicePort(&corev1.ServicePort{
			Name: "pgbouncer",
			Port: 9999,
		}).Build().Spec.Ports[0].Port).To(Equal(int32(9999)))
	})

	It("adds pgbouncer port if not present", func() {
		service := New().WithServicePort(&corev1.ServicePort{
			Name: "pgbouncer",
			Port: 9999,
		}).Build()

		Expect(service.Spec.Ports).To(HaveLen(1))
		Expect(service.Spec.Ports[0].Name).To(Equal("pgbouncer"))
		Expect(service.Spec.Ports[0].Port).To(Equal(int32(9999)))
	})

	It("overrides selector", func() {
		Expect(NewFrom(&apiv1.ServiceTemplateSpec{
			Spec: corev1.ServiceSpec{
				Selector: map[string]string{
					utils.PgbouncerNameLabel: "myservice",
				},
			},
		}).WithSelector("otherservice", true).Build().Spec.Selector).
			To(Equal(map[string]string{utils.PgbouncerNameLabel: "otherservice"}))
	})
})
