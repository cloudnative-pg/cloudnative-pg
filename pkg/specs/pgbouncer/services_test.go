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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	pgBouncerConfig "github.com/cloudnative-pg/cloudnative-pg/pkg/management/pgbouncer/config"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils/hash"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Pooler Service", func() {
	var (
		pooler  *apiv1.Pooler
		cluster *apiv1.Cluster
	)

	BeforeEach(func() {
		pooler = &apiv1.Pooler{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pooler",
				Namespace: "test-namespace",
			},
			Spec: apiv1.PoolerSpec{
				ServiceTemplate: &apiv1.ServiceTemplateSpec{
					Spec: corev1.ServiceSpec{
						Type: corev1.ServiceTypeClusterIP,
					},
				},
			},
		}
		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "test-namespace",
			},
		}
	})

	Context("when creating a Service", func() {
		It("returns the correct Service", func() {
			service, err := Service(pooler, cluster)

			Expect(err).ShouldNot(HaveOccurred())
			Expect(service).ToNot(BeNil())

			expectedHash, err := hash.ComputeVersionedHash(pooler.Spec, 3)
			Expect(err).ShouldNot(HaveOccurred())

			// Check the computed hash
			Expect(service.ObjectMeta.Annotations[utils.PoolerSpecHashAnnotationName]).Should(Equal(expectedHash))

			// Check the metadata
			Expect(service.Name).To(Equal(pooler.Name))
			Expect(service.Namespace).To(Equal(pooler.Namespace))
			Expect(service.Labels).To(BeEquivalentTo(map[string]string{
				utils.ClusterLabelName:                cluster.Name,
				utils.PgbouncerNameLabel:              pooler.Name,
				utils.PodRoleLabelName:                string(utils.PodRolePooler),
				utils.KubernetesAppLabelName:          utils.AppName,
				utils.KubernetesAppInstanceLabelName:  cluster.Name,
				utils.KubernetesAppComponentLabelName: utils.PoolerComponentName,
				utils.KubernetesAppManagedByLabelName: utils.ManagerName,
			}))
			Expect(service.Spec.Type).To(Equal(corev1.ServiceTypeClusterIP))
			Expect(service.Spec.Ports).To(ConsistOf(corev1.ServicePort{
				Name:       "pgbouncer",
				Port:       pgBouncerConfig.PgBouncerPort,
				TargetPort: intstr.FromString("pgbouncer"),
				Protocol:   corev1.ProtocolTCP,
			}))
			Expect(service.Spec.Selector).To(Equal(map[string]string{
				utils.PgbouncerNameLabel: pooler.Name,
			}))
		})
	})
})
