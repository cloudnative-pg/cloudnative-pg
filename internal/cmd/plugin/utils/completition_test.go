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

package utils

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8client "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/scheme"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("CompleteClusters", func() {
	var (
		ctx    context.Context
		client k8client.Client
		args   []string
	)

	BeforeEach(func() {
		ctx = context.Background()

		cluster1 := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cluster1",
				Namespace: "default",
			},
		}
		cluster2 := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cluster2",
				Namespace: "default",
			},
		}

		client = fake.NewClientBuilder().WithScheme(scheme.BuildWithAllKnownScheme()).
			WithObjects(cluster1, cluster2).Build()
	})

	It("should return matching cluster names", func() {
		toComplete := "clu"
		result := CompleteClusters(ctx, client, args, toComplete)
		Expect(result).To(HaveLen(2))
		Expect(result).To(ConsistOf("cluster1", "cluster2"))
	})

	It("should return empty array when no clusters found", func() {
		toComplete := "nonexistent"
		result := CompleteClusters(ctx, client, args, toComplete)
		Expect(result).To(BeEmpty())
	})

	It("should skip clusters already in args", func() {
		args = []string{"cluster1"}
		toComplete := ""
		result := CompleteClusters(ctx, client, args, toComplete)
		Expect(result).To(HaveLen(1))
		Expect(result).To(ConsistOf("cluster2"))
	})

	It("should skip clusters with prefix not matching toComplete", func() {
		toComplete := "nonexistent"
		result := CompleteClusters(ctx, client, args, toComplete)
		Expect(result).To(BeEmpty())
	})
})
