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

package plugin

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8client "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/versions"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("create client", func() {
	It("with given configuration", func() {
		// createClient is not a pure function and as a side effect
		// it will:
		// - set the Client global variable
		// - set the UserAgent field inside cfg
		err := createClient(cfg)

		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.UserAgent).To(Equal("kubectl-cnpg/v" + versions.Version + " (" + versions.Info.Commit + ")"))
		Expect(Client).NotTo(BeNil())
	})
})

var _ = Describe("CompleteClusters testing", func() {
	const namespace = "default"
	var client k8client.Client

	BeforeEach(func() {
		cluster1 := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cluster1",
				Namespace: namespace,
			},
		}
		cluster2 := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cluster2",
				Namespace: namespace,
			},
		}

		client = fake.NewClientBuilder().WithScheme(scheme.BuildWithAllKnownScheme()).
			WithObjects(cluster1, cluster2).Build()
	})

	It("should return matching cluster names", func(ctx SpecContext) {
		toComplete := "clu"
		result := completeClusters(ctx, client, namespace, []string{}, toComplete)
		Expect(result).To(HaveLen(2))
		Expect(result).To(ConsistOf("cluster1", "cluster2"))
	})

	It("should return empty array when no clusters found", func(ctx SpecContext) {
		toComplete := "nonexistent"
		result := completeClusters(ctx, client, namespace, []string{}, toComplete)
		Expect(result).To(BeEmpty())
	})

	It("should skip clusters with prefix not matching toComplete", func(ctx SpecContext) {
		toComplete := "nonexistent"
		result := completeClusters(ctx, client, namespace, []string{}, toComplete)
		Expect(result).To(BeEmpty())
	})

	It("should return nothing when a cluster name is already on the arguments list", func(ctx SpecContext) {
		args := []string{"cluster-example"}
		toComplete := "cluster-"
		result := completeClusters(ctx, client, namespace, args, toComplete)
		Expect(result).To(BeEmpty())
	})
})
