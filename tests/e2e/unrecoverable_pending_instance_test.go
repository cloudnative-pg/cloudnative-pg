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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	clusterasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/cluster"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/nodes"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/objects"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/pods"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/run"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/storage"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// This test covers the unrecoverable annotation on an instance that is stuck
// Pending. The healthy-Running case lives in destroy_instance_test.go; here we
// force a Pending instance by cordoning the node its PVC is pinned to, which is
// the state that the active-instances gate used to short-circuit.
var _ = Describe("Unrecoverable pending instance", Serial,
	Label(tests.LabelDisruptive, tests.LabelMaintenance), func() {
		const (
			level           = tests.High
			namespacePrefix = "unrecoverable-pending"
			sampleFile      = fixturesDir + "/base/cluster-storage-class.yaml.template"
			clusterName     = "postgresql-storage-class"
		)
		var namespace string

		BeforeEach(func() {
			if testLevelEnv.Depth < int(level) {
				Skip("Test depth is lower than the amount requested for this test")
			}
		})

		AfterEach(func() {
			// Always uncordon, even on failure, so a cordoned node does not leak
			// into other specs.
			err := nodes.UncordonAll(env.Ctx, env.Client)
			Expect(err).ToNot(HaveOccurred())
		})

		It("honors the annotation on a Pending instance", func() {
			collectPVCUIDs := func(instanceName string) map[string]string {
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

			By("creating a CNPG cluster", func() {
				var err error
				namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
				Expect(err).ToNot(HaveOccurred())
				clusterasserts.AssertCreateCluster(env, testTimeouts, namespace, clusterName, sampleFile)
			})

			var instanceName, cordonedNode string
			var originalPVCUIDs map[string]string
			var expectedInstanceCount, expectedPVCCount int

			By("cordoning a replica's node and deleting the replica pod", func() {
				replicas, err := clusterutils.GetReplicas(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred(), "failed to get the list of replicas")
				Expect(replicas.Items).ToNot(BeEmpty())

				instanceName = replicas.Items[0].Name
				cordonedNode = replicas.Items[0].Spec.NodeName
				Expect(cordonedNode).ToNot(BeEmpty())

				originalPVCUIDs = collectPVCUIDs(instanceName)
				Expect(originalPVCUIDs).ToNot(BeEmpty(), "expected at least one PVC for the instance")

				instancePods, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred(), "failed to list instance pods")
				expectedInstanceCount = len(instancePods.Items)

				allPVCs, err := storage.GetPVCList(env.Ctx, env.Client, namespace)
				Expect(err).ToNot(HaveOccurred(), "failed to list PVCs")
				expectedPVCCount = len(allPVCs.Items)

				// Cordoning the node makes it unschedulable. The instance's PVs are
				// pinned to it (local-path with WaitForFirstConsumer), so the pod
				// recreated after the deletion below cannot be scheduled anywhere and
				// stays Pending.
				_, _, err = run.Run(fmt.Sprintf("kubectl cordon %v", cordonedNode))
				Expect(err).ToNot(HaveOccurred())

				err = pods.Delete(env.Ctx, env.Client, namespace, instanceName)
				Expect(err).ToNot(HaveOccurred(), "failed to delete the replica pod")
			})

			By("waiting for the instance to be stuck Pending", func() {
				Eventually(func(g Gomega) {
					var pod corev1.Pod
					err := env.Client.Get(env.Ctx,
						types.NamespacedName{Namespace: namespace, Name: instanceName}, &pod)
					g.Expect(err).ToNot(HaveOccurred())
					// Make sure this is the recreated pod, not the one being deleted.
					g.Expect(pod.DeletionTimestamp).To(BeNil())
					g.Expect(pod.Status.Phase).To(Equal(corev1.PodPending))
				}, 180).Should(Succeed())
			})

			By("marking the Pending instance as unrecoverable", func() {
				var pod corev1.Pod
				err := env.Client.Get(env.Ctx,
					types.NamespacedName{Namespace: namespace, Name: instanceName}, &pod)
				Expect(err).ToNot(HaveOccurred(), "failed to get the Pending pod")

				originalPod := pod.DeepCopy()
				if pod.Annotations == nil {
					pod.Annotations = map[string]string{}
				}
				pod.Annotations[utils.UnrecoverableInstanceAnnotationName] = "true"

				err = objects.Patch(env.Ctx, env.Client, &pod, ctrlclient.MergeFrom(originalPod))
				Expect(err).ToNot(HaveOccurred(),
					"failed to patch the pod with the unrecoverable annotation")
			})

			By("recording a DeleteUnrecoverableInstance event on the Cluster", func() {
				Eventually(func(g Gomega) {
					eventList := corev1.EventList{}
					err := env.Client.List(env.Ctx, &eventList,
						ctrlclient.InNamespace(namespace),
						ctrlclient.MatchingFields{
							"involvedObject.kind": "Cluster",
							"involvedObject.name": clusterName,
						})
					g.Expect(err).ToNot(HaveOccurred())

					reasons := make([]string, 0, len(eventList.Items))
					for i := range eventList.Items {
						reasons = append(reasons, eventList.Items[i].Reason)
					}
					g.Expect(reasons).To(ContainElement("DeleteUnrecoverableInstance"))
				}, 180).Should(Succeed())
			})

			By("recreating the instance PVCs with fresh UIDs", func() {
				// The recreated instance gets a new serial, so its PVCs land
				// under a different name: match by UID instead. The full PVC
				// count must be restored, and none of the deleted instance's
				// original UIDs may still exist.
				Eventually(func(g Gomega) {
					pvcs, err := storage.GetPVCList(env.Ctx, env.Client, namespace)
					g.Expect(err).ToNot(HaveOccurred(), "failed to list PVCs")
					g.Expect(pvcs.Items).To(HaveLen(expectedPVCCount),
						"expected the full PVC set to be restored after recreation")

					currentUIDs := make(map[string]bool, len(pvcs.Items))
					for i := range pvcs.Items {
						currentUIDs[string(pvcs.Items[i].UID)] = true
					}
					for name, originalUID := range originalPVCUIDs {
						g.Expect(currentUIDs[originalUID]).To(BeFalse(),
							"PVC %q kept its original UID instead of being recreated", name)
					}
				}, 300).Should(Succeed())
			})

			By("scheduling the recreated instance onto a schedulable node", func() {
				Eventually(func(g Gomega) {
					instancePods, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
					g.Expect(err).ToNot(HaveOccurred(), "failed to list instance pods")
					g.Expect(instancePods.Items).To(HaveLen(expectedInstanceCount),
						"expected the cluster to have its full complement of instances back")
					for i := range instancePods.Items {
						g.Expect(instancePods.Items[i].Status.Phase).To(Equal(corev1.PodRunning))
						g.Expect(instancePods.Items[i].Spec.NodeName).ToNot(Equal(cordonedNode))
					}
				}, 300).Should(Succeed())
			})

			By("bringing the cluster back to healthy", func() {
				clusterasserts.AssertClusterIsReady(env, namespace, clusterName,
					testTimeouts[timeouts.ClusterIsReady])
			})
		})
	})
