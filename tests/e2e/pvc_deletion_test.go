package e2e

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/pkg/utils"
	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/tests"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("PVC Deletion", func() {

	It("correctly manages PVCs", func() {
		const namespace = "cluster-pvc-deletion"
		const sampleFile = fixturesDir + "/base/cluster-storage-class.yaml"
		const clusterName = "postgresql-storage-class"
		// Create a cluster in a namespace we'll delete after the test
		err := env.CreateNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
		defer func() {
			err := env.DeleteNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())
		}()
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
			_, _, err = tests.Run(fmt.Sprintf("kubectl delete -n %v pod/%v", namespace, podName))
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
			Expect(pvc.GetUID()).To(BeEquivalentTo(originalPVCUID), err)
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

			// Delete the PVC, this will set the PVC as terminated
			_, _, err = tests.Run(fmt.Sprintf("kubectl delete -n %v pvc/%v --wait=false", namespace, pvcName))
			Expect(err).ToNot(HaveOccurred())
			// Delete the pod
			_, _, err = tests.Run(fmt.Sprintf("kubectl delete -n %v pod/%v", namespace, podName))
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
			Expect(newPvc.GetUID()).NotTo(BeEquivalentTo(originalPVCUID), err)
		})
	})
})
