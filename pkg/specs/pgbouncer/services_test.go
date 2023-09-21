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

package pgbouncer

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	pgBouncerConfig "github.com/cloudnative-pg/cloudnative-pg/pkg/management/pgbouncer/config"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Pooler Service", func() {
	var pooler *apiv1.Pooler

	BeforeEach(func() {
		pooler = &apiv1.Pooler{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pooler",
				Namespace: "test-namespace",
			},
		}
	})

	Context("when creating a Service", func() {
		It("returns the correct Service", func() {
			service := Service(pooler)
			Expect(service.Name).To(Equal(pooler.Name))
			Expect(service.Namespace).To(Equal(pooler.Namespace))
			Expect(service.Spec.Type).To(Equal(corev1.ServiceTypeClusterIP))
			Expect(service.Spec.Ports).To(ConsistOf(corev1.ServicePort{
				Name:       "pgbouncer",
				Protocol:   corev1.ProtocolTCP,
				TargetPort: intstr.FromInt(pgBouncerConfig.PgBouncerPort),
				Port:       pgBouncerConfig.PgBouncerPort,
			}))
			Expect(service.Spec.Selector).To(Equal(map[string]string{
				utils.PgbouncerNameLabel: pooler.Name,
			}))
		})
	})
})
