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

package status

import (
	"context"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PatchWithOptimisticLock", func() {
	It("records the generation the caller evaluated when a spec change lands mid-patch", func(ctx SpecContext) {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "cluster-example",
				Namespace:  "default",
				Generation: 1,
			},
			Status: apiv1.ClusterStatus{Phase: apiv1.PhaseHealthy},
		}

		cli := fake.NewClientBuilder().
			WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
			WithStatusSubresource(&apiv1.Cluster{}).
			WithObjects(cluster.DeepCopy()).
			WithInterceptorFuncs(interceptor.Funcs{
				Get: func(
					ctx context.Context,
					c client.WithWatch,
					key client.ObjectKey,
					obj client.Object,
					opts ...client.GetOption,
				) error {
					if err := c.Get(ctx, key, obj, opts...); err != nil {
						return err
					}
					if fetched, ok := obj.(*apiv1.Cluster); ok {
						fetched.Generation = 2
					}
					return nil
				},
			}).
			Build()

		Expect(PatchWithOptimisticLock(ctx, cli, cluster, SetClusterReadyCondition)).To(Succeed())

		condition := meta.FindStatusCondition(cluster.Status.Conditions, string(apiv1.ConditionClusterReady))
		Expect(condition).ToNot(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionTrue))
		Expect(condition.ObservedGeneration).To(Equal(int64(1)))
	})
})
