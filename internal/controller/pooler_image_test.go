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

package controller

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("resolvePoolerImage", func() {
	var env *testingEnvironment

	BeforeEach(func() {
		env = buildTestEnvironment()
		configuration.Current = configuration.NewConfiguration()
		configuration.Current.PgbouncerImageName = "operator-default:1"
	})

	AfterEach(func() {
		configuration.Current = configuration.NewConfiguration()
	})

	newPooler := func() *apiv1.Pooler {
		return &apiv1.Pooler{
			ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "ns"},
			Spec: apiv1.PoolerSpec{
				PgBouncer: &apiv1.PgBouncerSpec{},
			},
		}
	}

	It("falls back to the operator default when nothing is configured", func() {
		image, err := env.poolerReconciler.resolvePoolerImage(context.Background(), newPooler())
		Expect(err).ToNot(HaveOccurred())
		Expect(image).To(Equal("operator-default:1"))
	})

	It("uses spec.pgbouncer.image over the operator default", func() {
		pooler := newPooler()
		pooler.Spec.PgBouncer.Image = "explicit:9"

		image, err := env.poolerReconciler.resolvePoolerImage(context.Background(), pooler)
		Expect(err).ToNot(HaveOccurred())
		Expect(image).To(Equal("explicit:9"))
	})

	It("lets the pod template override spec.pgbouncer.image", func() {
		pooler := newPooler()
		pooler.Spec.PgBouncer.Image = "explicit:9"
		pooler.Spec.Template = &apiv1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "pgbouncer", Image: "from-template:42"},
				},
			},
		}

		image, err := env.poolerReconciler.resolvePoolerImage(context.Background(), pooler)
		Expect(err).ToNot(HaveOccurred())
		Expect(image).To(Equal("from-template:42"))
	})

	It("ignores pod-template containers that are not pgbouncer", func() {
		pooler := newPooler()
		pooler.Spec.Template = &apiv1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "sidecar", Image: "sidecar:1"},
				},
			},
		}

		image, err := env.poolerReconciler.resolvePoolerImage(context.Background(), pooler)
		Expect(err).ToNot(HaveOccurred())
		Expect(image).To(Equal("operator-default:1"))
	})

	It("ignores a pgbouncer container in the template that has no image", func() {
		pooler := newPooler()
		pooler.Spec.PgBouncer.Image = "explicit:9"
		pooler.Spec.Template = &apiv1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "pgbouncer"},
				},
			},
		}

		image, err := env.poolerReconciler.resolvePoolerImage(context.Background(), pooler)
		Expect(err).ToNot(HaveOccurred())
		Expect(image).To(Equal("explicit:9"))
	})
})
