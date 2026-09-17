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

package e2e

import (
	"k8s.io/client-go/util/retry"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	clusterasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/cluster"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/objects"
	storageutils "github.com/cloudnative-pg/cloudnative-pg/tests/utils/storage"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Test case for validating that spec.inheritedMetadata.labels can override the
// common app.kubernetes.io/* labels the operator sets on every PVC
var _ = Describe("PVC inheritedMetadata labels", Label(tests.LabelClusterMetadata), func() {
	const (
		namespacePrefix = "cluster-pvc-inherited-metadata"
		sampleFile      = fixturesDir + "/pvc_inherited_metadata/cluster-pvc-inherited-metadata.yaml.template"
		clusterName     = "cluster-pvc-inherited-metadata"
		level           = tests.Medium
	)
	var namespace string

	inheritedLabels := map[string]string{
		utils.KubernetesAppLabelName:          "my-custom-app",
		utils.KubernetesAppManagedByLabelName: "my-gitops-tool",
		utils.KubernetesAppComponentLabelName: "my-custom-component",
	}

	updatedLabels := map[string]string{
		utils.KubernetesAppLabelName:          "my-custom-app-v2",
		utils.KubernetesAppManagedByLabelName: "my-gitops-tool",
		utils.KubernetesAppComponentLabelName: "my-custom-component",
	}

	assertPVCsHaveLabels := func(g Gomega, expected map[string]string) {
		pvcList, err := storageutils.GetPVCList(env.Ctx, env.Client, namespace)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(pvcList.Items).ToNot(BeEmpty())

		for _, pvc := range pvcList.Items {
			g.Expect(storageutils.PvcHasLabels(pvc, expected)).To(BeTrue(),
				"expected PVC %q to have labels %v, but found %v",
				pvc.Name, expected, pvc.Labels)
		}
	}

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	It("lets inheritedMetadata override the common labels on PVCs", func() {
		var err error
		namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
		Expect(err).ToNot(HaveOccurred())
		clusterasserts.AssertCreateCluster(env, testTimeouts, namespace, clusterName, sampleFile)

		By("checking the PVCs have the inherited labels after creation", func() {
			assertPVCsHaveLabels(Default, inheritedLabels)
		})

		By("changing the inherited labels on the cluster", func() {
			err := retry.OnError(retry.DefaultBackoff, objects.IsRetryableConflictOrTransientError, func() error {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				if err != nil {
					return err
				}
				cluster.Spec.InheritedMetadata.Labels[utils.KubernetesAppLabelName] = updatedLabels[utils.KubernetesAppLabelName]
				return env.Client.Update(env.Ctx, cluster)
			})
			Expect(err).ToNot(HaveOccurred())
		})

		By("checking the PVCs get the new inherited labels, not the operator defaults", func() {
			Eventually(func(g Gomega) {
				assertPVCsHaveLabels(g, updatedLabels)
			}, testTimeouts[timeouts.ClusterIsReadyQuick]).Should(Succeed())
		})
	})
})
