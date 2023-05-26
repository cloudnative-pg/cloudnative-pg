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

package e2e

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	testsUtils "github.com/cloudnative-pg/cloudnative-pg/tests/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PVC Deletion", Label(tests.LabelSelfHealing), func() {
	const (
		namespacePrefix = "cluster-pvc-deletion"
		sampleFile      = fixturesDir + "/pvc_deletion/cluster-pvc-deletion.yaml.template"
		clusterName     = "cluster-pvc-deletion"
		level           = tests.Medium
	)
	var namespace string
	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	It("correctly manages PVCs", func() {
		var err error
		// Create a cluster in a namespace we'll delete after the test
		namespace, err = env.CreateUniqueNamespace(namespacePrefix)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() error {
			if CurrentSpecReport().Failed() {
				env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
			}
			return env.DeleteNamespace(namespace)
		})
		AssertCreateCluster(namespace, clusterName, sampleFile, env)

		// Reuse the same pvc after a deletion
		By("recreating a pod with the same PVC after it's deleted", func() {
			// Get a pod we want to delete
			podName := clusterName + "-3"
			pod := &corev1.Pod{}
			podNamespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      podName,
			}
			err := env.Client.Get(env.Ctx, podNamespacedName, pod)
			Expect(err).ToNot(HaveOccurred())

			// Get the UID of the pod
			pvcName := pod.Spec.Volumes[0].PersistentVolumeClaim.ClaimName
			pvc := &corev1.PersistentVolumeClaim{}
			namespacedPVCName := types.NamespacedName{
				Namespace: namespace,
				Name:      pvcName,
			}
			err = env.Client.Get(env.Ctx, namespacedPVCName, pvc)
			Expect(err).ToNot(HaveOccurred())
			originalPVCUID := pvc.GetUID()

			// Delete the pod
			quickDelete := &ctrlclient.DeleteOptions{
				GracePeriodSeconds: &quickDeletionPeriod,
			}
			err = env.DeletePod(namespace, podName, quickDelete)
			Expect(err).ToNot(HaveOccurred())

			// The pod should be back
			timeout := 300
			Eventually(func() (bool, error) {
				pod := &corev1.Pod{}
				err := env.Client.Get(env.Ctx, podNamespacedName, pod)
				return utils.IsPodActive(*pod) && utils.IsPodReady(*pod), err
			}, timeout).Should(BeTrue())

			// The pod should have the same PVC
			pod = &corev1.Pod{}
			err = env.Client.Get(env.Ctx, podNamespacedName, pod)
			Expect(err).ToNot(HaveOccurred())
			pvc = &corev1.PersistentVolumeClaim{}
			err = env.Client.Get(env.Ctx, namespacedPVCName, pvc)
			Expect(pvc.GetUID(), err).To(BeEquivalentTo(originalPVCUID))
		})

		By("removing a PVC and delete the Pod", func() {
			// Get a pod we want to delete
			podName := clusterName + "-3"
			pod := &corev1.Pod{}
			podNamespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      podName,
			}
			err := env.Client.Get(env.Ctx, podNamespacedName, pod)
			Expect(err).ToNot(HaveOccurred())

			// Get the UID of the PVC
			pvcName := pod.Spec.Volumes[0].PersistentVolumeClaim.ClaimName
			pvc := &corev1.PersistentVolumeClaim{}
			namespacedPVCName := types.NamespacedName{
				Namespace: namespace,
				Name:      pvcName,
			}
			err = env.Client.Get(env.Ctx, namespacedPVCName, pvc)
			Expect(err).ToNot(HaveOccurred())
			originalPVCUID := pvc.GetUID()

			// Check if walStorage is enabled
			walStorageEnabled, err := testsUtils.IsWalStorageEnabled(namespace, clusterName, env)
			Expect(err).ToNot(HaveOccurred())

			// Force delete setting
			quickDelete := &ctrlclient.DeleteOptions{
				GracePeriodSeconds: &quickDeletionPeriod,
			}

			// Delete the PVC and the Pod
			err = env.Client.Delete(env.Ctx, pvc, quickDelete)
			Expect(err).ToNot(HaveOccurred())

			// removing WalStorage PVC if needed
			if walStorageEnabled {
				walPvcName := fmt.Sprintf("%v-wal", pvcName)
				namespacedWalPVCName := types.NamespacedName{
					Namespace: namespace,
					Name:      walPvcName,
				}
				walPVC := &corev1.PersistentVolumeClaim{}
				err = env.Client.Get(env.Ctx, namespacedWalPVCName, walPVC)
				Expect(err).ToNot(HaveOccurred())
				err = env.Client.Delete(env.Ctx, walPVC, quickDelete)
				Expect(err).ToNot(HaveOccurred())
			}

			// Deleting primary pod
			err = env.DeletePod(namespace, podName, quickDelete)
			Expect(err).ToNot(HaveOccurred())

			// A new pod should be created
			timeout := 300
			newPodName := clusterName + "-4"
			newPodNamespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      newPodName,
			}
			Eventually(func() (bool, error) {
				newPod := &corev1.Pod{}
				err := env.Client.Get(env.Ctx, newPodNamespacedName, newPod)
				return utils.IsPodActive(*newPod) && utils.IsPodReady(*newPod), err
			}, timeout).Should(BeTrue())

			// The pod should have a different PVC
			newPod := &corev1.Pod{}
			err = env.Client.Get(env.Ctx, newPodNamespacedName, newPod)
			Expect(err).ToNot(HaveOccurred())
			newPvcName := newPod.Spec.Volumes[0].PersistentVolumeClaim.ClaimName
			newPvc := &corev1.PersistentVolumeClaim{}
			newNamespacedPVCName := types.NamespacedName{
				Namespace: namespace,
				Name:      newPvcName,
			}
			err = env.Client.Get(env.Ctx, newNamespacedPVCName, newPvc)
			Expect(newPvc.GetUID(), err).NotTo(BeEquivalentTo(originalPVCUID))
		})

		// Check the labels of each PVC
		AssertPvcHasLabels(namespace, clusterName)
	})
})
