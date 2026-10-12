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
	"time"

	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/persistentvolumeclaim"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	clusterasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/cluster"
	pgasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/internal/resources"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/exec"
	podutils "github.com/cloudnative-pg/cloudnative-pg/tests/utils/pods"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// A backup tool restores the instance Pods together with their PVCs, adding
// an init container that copies the PVC content back. The Pods still carry
// the bootstrap-instance init container they were created with, and
// Kubernetes runs it again on volumes that already hold the instances: it
// must leave the data alone, wherever the tool put its own init container.
var _ = Describe("Pods restored with their PVCs", Label(tests.LabelBackupRestore), func() {
	const (
		namespacePrefix = "restored-pods-e2e"
		sampleFile      = fixturesDir + "/restored_pods/cluster-restored-pods.yaml.template"
		clusterName     = "cluster-restored-pods"
		tableName       = "restored_pods"
		// the init container standing for the backup tool copying the
		// PVC content back. The operator replaces the restored Pods only
		// once it has terminated in all of them.
		restoreToolContainerName = "restore-tool"
		level                    = tests.Medium
	)

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	// restorePods recreates the backed up Pods as they were, adding the
	// restore tool's init container right before or right after
	// bootstrap-instance, sleeping for the given number of seconds.
	restorePods := func(backedUpPods []corev1.Pod, toolBeforeBootstrap bool,
		toolSleep func(idx int) string,
	) map[string]types.UID {
		restoredPodUIDs := make(map[string]types.UID, len(backedUpPods))
		for idx := range backedUpPods {
			backedUpPod := &backedUpPods[idx]
			restoredPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:        backedUpPod.Name,
					Namespace:   backedUpPod.Namespace,
					Labels:      backedUpPod.Labels,
					Annotations: backedUpPod.Annotations,
				},
				Spec: *backedUpPod.Spec.DeepCopy(),
			}
			restoredPod.Spec.NodeName = ""

			var initContainers []corev1.Container
			for _, container := range restoredPod.Spec.InitContainers {
				if container.Name != specs.BootstrapWorkContainerName {
					initContainers = append(initContainers, container)
					continue
				}
				restoreTool := corev1.Container{
					Name:            restoreToolContainerName,
					Image:           container.Image,
					Command:         []string{"sleep", toolSleep(idx)},
					SecurityContext: container.SecurityContext,
				}
				if toolBeforeBootstrap {
					initContainers = append(initContainers, restoreTool, container)
				} else {
					initContainers = append(initContainers, container, restoreTool)
				}
			}
			Expect(initContainers).To(ContainElement(HaveField("Name", restoreToolContainerName)))
			restoredPod.Spec.InitContainers = initContainers

			Expect(env.Client.Create(env.Ctx, restoredPod)).To(Succeed())
			restoredPodUIDs[restoredPod.Name] = restoredPod.UID
		}
		return restoredPodUIDs
	}

	// bootstrapStatusOf asserts that the given Pod is still the restored one,
	// and that its bootstrap-instance init container status satisfies matcher
	bootstrapStatusOf := func(g Gomega, namespace, podName string, podUID types.UID, matcher OmegaMatcher) {
		pod, err := podutils.Get(env.Ctx, env.Client, namespace, podName)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(pod.UID).To(Equal(podUID), "the restored Pod %s was replaced too early", podName)
		g.Expect(pod.Status.InitContainerStatuses).To(ContainElement(SatisfyAll(
			HaveField("Name", specs.BootstrapWorkContainerName),
			matcher,
		)), "unexpected bootstrap-instance status in the restored Pod %s", podName)
	}

	assertRestoredPodsKeepTheirData := func(toolBeforeBootstrap bool) {
		namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
		Expect(err).ToNot(HaveOccurred())
		clusterasserts.AssertCreateCluster(env, testTimeouts, namespace, clusterName, sampleFile)

		tableLocator := pgasserts.TableLocator{
			Namespace:   namespace,
			ClusterName: clusterName,
			TableName:   tableName,
		}
		pgasserts.AssertCreateTestData(env, tableLocator)

		var backedUpPods []corev1.Pod
		// A backup is consistent once the operator has seen every bootstrap
		// succeed, which it records by removing the pending mark from the Pods
		// and then marking the PGDATA PVCs as ready.
		By("waiting for the operator to mark every bootstrap as completed", func() {
			Eventually(func(g Gomega) {
				podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(podList.Items).To(HaveLen(2))
				for idx := range podList.Items {
					g.Expect(podList.Items[idx].Spec.InitContainers).To(ContainElement(
						HaveField("Name", specs.BootstrapWorkContainerName)))
					g.Expect(podList.Items[idx].Annotations).ToNot(HaveKey(utils.BootstrapPendingAnnotationName))
					var pvc corev1.PersistentVolumeClaim
					g.Expect(env.Client.Get(env.Ctx, ctrlclient.ObjectKey{
						Namespace: namespace,
						Name:      persistentvolumeclaim.NewPgDataCalculator().GetName(podList.Items[idx].Name),
					}, &pvc)).To(Succeed())
					g.Expect(pvc.Annotations).To(HaveKeyWithValue(
						utils.PVCStatusAnnotationName, persistentvolumeclaim.StatusReady))
				}
				backedUpPods = podList.Items
			}, 60).Should(Succeed())
		})

		By("deleting the cluster, keeping every resource it owns", func() {
			cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			Expect(env.Client.Delete(env.Ctx, cluster,
				ctrlclient.PropagationPolicy(metav1.DeletePropagationOrphan))).To(Succeed())
			Eventually(func() bool {
				_, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				return apierrs.IsNotFound(err)
			}, 60).Should(BeTrue())
		})

		By("deleting the Pods and the primary Lease, as lost with the Kubernetes cluster", func() {
			// The operator cannot adopt the Lease of a previous Cluster yet,
			// as it is not allowed to delete Leases: drop it, so that the
			// restored Cluster can elect its primary.
			Expect(env.Client.Delete(env.Ctx, &coordinationv1.Lease{
				ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: clusterName},
			})).To(Succeed())

			quickDelete := &ctrlclient.DeleteOptions{GracePeriodSeconds: &quickDeletionPeriod}
			for idx := range backedUpPods {
				Expect(podutils.Delete(env.Ctx, env.Client, namespace, backedUpPods[idx].Name,
					quickDelete)).To(Succeed())
			}
			Eventually(func(g Gomega) {
				podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(podList.Items).To(BeEmpty())
			}, 120).Should(Succeed())
		})

		if toolBeforeBootstrap {
			// The first Pod's tool finishes in seconds, the other's after a
			// minute: meanwhile bootstrap-instance runs in the first Pod on
			// the restored volumes.
			var restoredPodUIDs map[string]types.UID
			By("restoring the Pods and the Cluster, with the tool running before bootstrap-instance", func() {
				restoredPodUIDs = restorePods(backedUpPods, true, func(idx int) string {
					if idx == 0 {
						return "5"
					}
					return "60"
				})
				resources.CreateResourceFromFile(env, namespace, sampleFile)
			})

			By("letting bootstrap-instance run again on the restored volumes", func() {
				firstPodName := backedUpPods[0].Name
				Eventually(func(g Gomega) {
					bootstrapStatusOf(g, namespace, firstPodName, restoredPodUIDs[firstPodName], Or(
						HaveField("State.Running", Not(BeNil())),
						HaveField("State.Terminated", Not(BeNil())),
					))
				}, 60).Should(Succeed())
			})
		} else {
			// The tool runs after bootstrap-instance, so the operator does
			// not replace the Pods before bootstrap-instance has run on the
			// restored volumes.
			var restoredPodUIDs map[string]types.UID
			By("restoring the Pods and the Cluster, with the tool running after bootstrap-instance", func() {
				restoredPodUIDs = restorePods(backedUpPods, false, func(int) string { return "30" })
				resources.CreateResourceFromFile(env, namespace, sampleFile)
			})

			By("letting bootstrap-instance run again on the restored volumes", func() {
				Eventually(func(g Gomega) {
					for podName, podUID := range restoredPodUIDs {
						bootstrapStatusOf(g, namespace, podName, podUID,
							HaveField("State.Terminated.ExitCode", BeEquivalentTo(0)))
					}
				}, 120).Should(Succeed())
			})
		}

		clusterasserts.AssertClusterIsReady(env, namespace, clusterName, testTimeouts[timeouts.ClusterIsReady])

		By("finding the data written before the backup on every instance", func() {
			pgasserts.AssertDataExpectedCount(env, tableLocator, 2)

			podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			for idx := range podList.Items {
				podLocator := exec.PodLocator{Namespace: namespace, PodName: podList.Items[idx].Name}
				Eventually(func(g Gomega) {
					out, _, err := exec.QueryInInstancePod(env.Ctx, env.Client, env.Interface,
						env.RestClientConfig, podLocator, "app", "SELECT count(*) FROM "+tableName)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(out).To(Equal("2\n"))
				}, 60).Should(Succeed())

				timeout := 10 * time.Second
				out, _, err := exec.CommandInInstancePod(env.Ctx, env.Client, env.Interface,
					env.RestClientConfig, podLocator, &timeout, "sh", "-c",
					"ls -d "+specs.PgDataPath+"_* "+specs.PgWalVolumePgWalPath+"_* 2>/dev/null; true")
				Expect(err).ToNot(HaveOccurred())
				Expect(out).To(BeEmpty(), "the data directories of %s were moved aside", podLocator.PodName)
			}
		})
	}

	It("keeps the data when the restore tool's init container runs after bootstrap-instance", func() {
		assertRestoredPodsKeepTheirData(false)
	})

	It("keeps the data when the restore tool's init container runs before bootstrap-instance", func() {
		assertRestoredPodsKeepTheirData(true)
	})
})
