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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/storage"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Test destroy instance", func() {
	const (
		level = tests.High
	)

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	Describe("Unrecoverable instance annotation", func() {
		const namespacePrefix = "destroy-instance"
		const sampleFile = fixturesDir + "/base/cluster-storage-class.yaml.template"
		const clusterName = "postgresql-storage-class"
		var namespace string
		var unrecoverableInstanceName string
		var originalPVCUIDs map[string]string

		collectPVCUIDsForInstance := func(instanceName string) map[string]string {
			GinkgoHelper()
			pvcs, err := storage.GetPVCList(env.Ctx, env.Client, namespace)
			Expect(err).ToNot(HaveOccurred(), "failed to list PVCs")

			result := map[string]string{}
			for i := range pvcs.Items {
				if pvcs.Items[i].Labels[utils.InstanceNameLabelName] == instanceName {
					result[pvcs.Items[i].Name] = string(pvcs.Items[i].UID)
				}
			}
			return result
		}

		It("should destroy instance successfully", func() {
			// If we have specified secrets, we test that we're able to use them
			// to connect
			By("creating a CNPG cluster", func() {
				// Create a cluster in a namespace we'll delete after the test
				var err error
				namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
				Expect(err).ToNot(HaveOccurred())
				AssertCreateCluster(namespace, clusterName, sampleFile, env)
			})

			By("marking a instance as unrecoverable", func() {
				podList, err := clusterutils.GetReplicas(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(
					HaveOccurred(),
					"Failed to get the list of replicas")
				Expect(podList.Items).Should(HaveLen(2))

				unrecoverableInstanceName = podList.Items[0].Name

				originalPVCUIDs = collectPVCUIDsForInstance(unrecoverableInstanceName)
				Expect(originalPVCUIDs).ToNot(BeEmpty(), "expected at least one PVC for the instance")

				var pod corev1.Pod
				err = env.Client.Get(
					env.Ctx,
					types.NamespacedName{Namespace: namespace, Name: unrecoverableInstanceName},
					&pod)
				Expect(err).ToNot(
					HaveOccurred(),
					"failed to get pod")

				if pod.Annotations == nil {
					pod.Annotations = map[string]string{}
				}
				originalPod := pod.DeepCopy()
				pod.Annotations[utils.UnrecoverableInstanceAnnotationName] = "true"

				err = env.Client.Patch(env.Ctx, &pod, ctrlclient.MergeFrom(originalPod))
				Expect(err).ToNot(
					HaveOccurred(),
					"failed to patch pod with unrecoverable instance annotation")
			})

			By("waiting for every PVC of the instance to be recreated", func() {
				// The unrecoverable instance keeps its name (serials are reused),
				// so each PVC must come back under the same name with a fresh UID.
				// Checking the full set guards against leaks of secondary PVCs
				// (wal, tablespaces) that share only the instance label.
				Eventually(func(g Gomega) {
					currentUIDs := collectPVCUIDsForInstance(unrecoverableInstanceName)
					g.Expect(currentUIDs).To(HaveLen(len(originalPVCUIDs)))
					for name, originalUID := range originalPVCUIDs {
						currentUID, ok := currentUIDs[name]
						g.Expect(ok).To(BeTrue(),
							"expected PVC %q to exist after recreation", name)
						g.Expect(currentUID).ToNot(Equal(originalUID),
							"PVC %q kept its original UID instead of being recreated", name)
					}
				}, 300).Should(Succeed())
			})

			By("waiting for the cluster to healthy again", func() {
				AssertClusterIsReady(namespace, clusterName, testTimeouts[timeouts.ClusterIsReady], env)
			})
		})
	})
})
