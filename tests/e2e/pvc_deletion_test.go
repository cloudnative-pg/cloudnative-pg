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

	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	testsUtils "github.com/cloudnative-pg/cloudnative-pg/tests/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PVC Deletion", func() {
	const (
		namespace   = "cluster-pvc-deletion"
		sampleFile  = fixturesDir + "/base/cluster-storage-class.yaml.template"
		clusterName = "postgresql-storage-class"
		level       = tests.Medium
	)
	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	JustAfterEach(func() {
		if CurrentSpecReport().Failed() {
			env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
		}
	})
	AfterEach(func() {
		err := env.DeleteNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
	})
	It("correctly manages PVCs", func() {
		// Create a cluster in a namespace we'll delete after the test
		err := env.CreateNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
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
			_, _, err = testsUtils.Run(fmt.Sprintf("kubectl delete -n %v pod/%v", namespace, podName))
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

			// Delete the PVC and the Pod
			_, _, err = testsUtils.Run(fmt.Sprintf("kubectl delete -n %v pvc/%v pod/%v", namespace, pvcName, podName))
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
	})
})
