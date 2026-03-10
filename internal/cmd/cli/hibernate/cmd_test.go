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

package hibernate

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8client "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("annotateCluster", func() {
	var (
		cluster    *apiv1.Cluster
		cli        k8client.Client
		clusterKey k8client.ObjectKey
	)

	BeforeEach(func() {
		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "test-namespace",
			},
		}
		cli = fake.NewClientBuilder().WithScheme(schemeBuilder.BuildWithAllKnownScheme()).WithObjects(cluster).Build()
		clusterKey = k8client.ObjectKeyFromObject(cluster)
	})

	It("annotates the cluster with hibernation on", func(ctx SpecContext) {
		err := annotateCluster(ctx, cli, clusterKey, utils.HibernationAnnotationValueOn)
		Expect(err).ToNot(HaveOccurred())

		updatedCluster := &apiv1.Cluster{}
		err = cli.Get(ctx, clusterKey, updatedCluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(updatedCluster.Annotations[utils.HibernationAnnotationName]).
			To(Equal(string(utils.HibernationAnnotationValueOn)))
	})

	It("annotates the cluster with hibernation off", func(ctx SpecContext) {
		err := annotateCluster(ctx, cli, clusterKey, utils.HibernationAnnotationValueOff)
		Expect(err).ToNot(HaveOccurred())

		updatedCluster := &apiv1.Cluster{}
		err = cli.Get(ctx, clusterKey, updatedCluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(updatedCluster.Annotations[utils.HibernationAnnotationName]).
			To(Equal(string(utils.HibernationAnnotationValueOff)))
	})

	It("returns an error if the cluster is already in the requested state", func(ctx SpecContext) {
		err := annotateCluster(ctx, cli, clusterKey, utils.HibernationAnnotationValueOn)
		Expect(err).ToNot(HaveOccurred())

		err = annotateCluster(ctx, cli, clusterKey, utils.HibernationAnnotationValueOn)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(Equal(fmt.Sprintf("cluster %s is already in the requested state", clusterKey.Name)))
	})

	It("returns an error if the cluster cannot be retrieved", func(ctx SpecContext) {
		nonExistingClusterKey := k8client.ObjectKey{
			Name:      "non-existing-cluster",
			Namespace: "test-namespace",
		}

		err := annotateCluster(ctx, cli, nonExistingClusterKey, utils.HibernationAnnotationValueOn)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(fmt.Sprintf("failed to get cluster %s", nonExistingClusterKey.Name)))
	})

	It("toggles hibernation from on to off", func(ctx SpecContext) {
		err := annotateCluster(ctx, cli, clusterKey, utils.HibernationAnnotationValueOn)
		Expect(err).ToNot(HaveOccurred())

		updatedCluster := &apiv1.Cluster{}
		err = cli.Get(ctx, clusterKey, updatedCluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(updatedCluster.Annotations[utils.HibernationAnnotationName]).
			To(Equal(string(utils.HibernationAnnotationValueOn)))

		err = annotateCluster(ctx, cli, clusterKey, utils.HibernationAnnotationValueOff)
		Expect(err).ToNot(HaveOccurred())

		updatedCluster = &apiv1.Cluster{}
		err = cli.Get(ctx, clusterKey, updatedCluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(updatedCluster.Annotations[utils.HibernationAnnotationName]).
			To(Equal(string(utils.HibernationAnnotationValueOff)))
	})
})
