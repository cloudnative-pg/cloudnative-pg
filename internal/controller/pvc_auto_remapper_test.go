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

package controller

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/pvcremapper"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/persistentvolumeclaim"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func verifyVolumes(
	volumes []corev1.Volume,
	toRemap pvcremapper.InstancePVCs,
) {
	var (
		hasPreviousVolumes bool
		hasNewVolumes      bool
	)
	for _, volume := range volumes {
		for _, ipvc := range toRemap {
			if volume.PersistentVolumeClaim == nil {
				continue
			}
			if !ipvc.RemapRequired() {
				continue
			}
			if volume.PersistentVolumeClaim.ClaimName == ipvc.Name() {
				hasPreviousVolumes = true
			}
			if volume.PersistentVolumeClaim.ClaimName == ipvc.ExpectedName() {
				hasNewVolumes = true
			}
		}
	}
	Ω(hasPreviousVolumes).To(BeFalse())
	Ω(hasNewVolumes).To(BeTrue())
}

func generateInstancePVs(
	ctx context.Context,
	c client.Client,
	input pvcremapper.InstancePVCs,
) pvcremapper.InstancePVCs {
	remapperTimeoutDelay = time.Microsecond
	var output []corev1.PersistentVolumeClaim
	var i int
	for _, ipvc := range input {
		pvName := fmt.Sprintf("%x", sha256.Sum256([]byte((ipvc.AsNamespacedName().String()))))

		var pvc corev1.PersistentVolumeClaim
		err := c.Get(ctx, ipvc.AsNamespacedName(), &pvc)
		Ω(err).ToNot(HaveOccurred())

		patchedPvc := pvc.DeepCopy()
		patchedPvc.Spec.VolumeName = pvName
		err = c.Patch(ctx, patchedPvc, client.MergeFrom(&pvc))
		Ω(err).ToNot(HaveOccurred())
		err = c.Get(ctx, ipvc.AsNamespacedName(), &pvc)
		Ω(err).ToNot(HaveOccurred())

		pv := corev1.PersistentVolume{
			ObjectMeta: metav1.ObjectMeta{
				Name: pvName,
			},
			Spec: corev1.PersistentVolumeSpec{
				ClaimRef: &corev1.ObjectReference{
					APIVersion:      pvc.TypeMeta.APIVersion,
					Kind:            pvc.TypeMeta.Kind,
					Name:            pvc.ObjectMeta.Name,
					Namespace:       pvc.ObjectMeta.Namespace,
					ResourceVersion: pvc.ResourceVersion,
					UID:             pvc.UID,
				},
				PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimDelete,
			},
		}
		err = c.Create(ctx, &pv)
		Ω(err).ToNot(HaveOccurred())
		err = c.Get(ctx, types.NamespacedName{Name: pvName}, &pv)
		Ω(err).ToNot(HaveOccurred())
		Ω(pv.Spec.ClaimRef.Name).NotTo(BeEmpty())
		Ω(pv.Spec.ClaimRef.Name).To(Equal(ipvc.Name()))
		output = append(output, *patchedPvc)
		i++
	}
	ipvcs, err := pvcremapper.InstancePVCsFromPVCs(output)
	Ω(err).ToNot(HaveOccurred())
	Ω(ipvcs).To(HaveLen(i))
	for _, ipvc := range ipvcs {
		Ω(ipvc.PvName()).NotTo(BeEmpty())
		Ω(ipvc.RemapRequired()).To(BeTrue())
	}
	return ipvcs
}

var _ = Describe("unit test for pvc remapper logic", func() {
	var env *testingEnvironment

	BeforeEach(func() {
		env = buildTestEnvironment()
		configuration.Current = configuration.NewConfiguration()
	})

	When("remapping is required", func() {
		ctx := context.Background()
		It("should rename PVC's", func() {
			namespace := newFakeNamespace(env.client)
			cluster := newFakeCNPGCluster(
				env.client,
				namespace,
				func(cluster *apiv1.Cluster) {
					cluster.Spec.WalStorage = &apiv1.StorageConfiguration{
						Size: "1Gi",
					}
				},
			)
			instances := generateFakeClusterPods(env.client, cluster, true)
			pvcs := generateClusterPVC(env.client, cluster, persistentvolumeclaim.StatusReady)
			ipvcs, err := pvcremapper.InstancePVCsFromPVCs(pvcs)
			Ω(err).NotTo(HaveOccurred())
			configuration.Current.DataVolumeSuffix = "-data2"
			configuration.Current.WalArchiveVolumeSuffix = "-wal2"
			ipvcs = generateInstancePVs(ctx, env.client, ipvcs)

			By("PV and PVC are properly linked", func() {
				var pv corev1.PersistentVolume
				var pvc corev1.PersistentVolumeClaim
				for _, ipvc := range ipvcs {
					fmt.Fprintf(GinkgoWriter, "DEBUG - pvcName: %s, pvName: %s\n", ipvc.Name(), ipvc.PvName())
					err := env.clusterReconciler.Client.Get(ctx, types.NamespacedName{
						Name: ipvc.PvName(),
					}, &pv)
					Ω(err).ToNot(HaveOccurred())

					err = env.clusterReconciler.Client.Get(ctx, types.NamespacedName{
						Name:      ipvc.Name(),
						Namespace: cluster.Namespace,
					}, &pvc)
					Ω(err).ToNot(HaveOccurred())
					Ω(pv.Spec.ClaimRef.Name).To(Equal(pvc.Name))
					Ω(pvc.Spec.VolumeName).To(Equal(pv.Name))
					Ω(pv.Spec.ClaimRef.Name).NotTo(BeEmpty())
					Ω(pv.Spec.ClaimRef.Name).To(Equal(ipvc.Name()))
					Ω(pv.Spec.PersistentVolumeReclaimPolicy).To(Equal(corev1.PersistentVolumeReclaimDelete))
				}
			})
			By("checking that remap finishes successfully", func() {
				for _, instance := range instances {
					err := env.clusterReconciler.instanceRemapping(
						ctx,
						types.NamespacedName{Name: instance.Name, Namespace: namespace},
						ipvcs,
					)
					Ω(err).NotTo(HaveOccurred())
				}
			})
			By("checking that the pods are linked to the correct PVC's", func() {
				var expectedPod corev1.Pod
				for _, instance := range instances {
					err := env.clusterReconciler.Client.Get(ctx, types.NamespacedName{
						Name:      instance.Name,
						Namespace: cluster.Namespace,
					}, &expectedPod)
					Ω(err).ToNot(HaveOccurred())
					var volumes []string
					for _, volume := range expectedPod.Spec.Volumes {
						if volume.PersistentVolumeClaim != nil {
							volumes = append(volumes, volume.PersistentVolumeClaim.ClaimName)
						}
					}
					Ω(volumes).To(ContainElement(instance.Name + "-data2"))
					Ω(volumes).To(ContainElement(instance.Name + "-wal2"))
					Ω(volumes).ToNot(ContainElement(instance.Name + ""))
					Ω(volumes).ToNot(ContainElement(instance.Name + "-wal"))
				}
			})
			By("checking that the new PVC's are created and old are removed", func() {
				var namespacePVCs []string
				var availablePVCs corev1.PersistentVolumeClaimList
				err := env.clusterReconciler.Client.List(ctx, &availablePVCs, &client.ListOptions{
					Namespace: namespace,
				})
				Ω(err).ToNot(HaveOccurred())
				for _, pvc := range availablePVCs.Items {
					namespacePVCs = append(namespacePVCs, pvc.Name)
				}

				for _, instance := range instances {
					for _, test := range []struct {
						pvcName  string
						expected bool
					}{
						{pvcName: instance.Name + "", expected: false},
						{pvcName: instance.Name + "-wal", expected: false},
						{pvcName: instance.Name + "-wal2", expected: true},
						{pvcName: instance.Name + "-data2", expected: true},
					} {
						if test.expected {
							Ω(namespacePVCs).To(ContainElement(test.pvcName))
						} else {
							Ω(namespacePVCs).ToNot(ContainElement(test.pvcName))
						}
					}
				}
			})
			By("PV and PVC are properly linked", func() {
				var pv corev1.PersistentVolume
				var pvc corev1.PersistentVolumeClaim
				for _, ipvc := range ipvcs {
					newPvcName := ipvc.ExpectedName()
					err = env.clusterReconciler.Client.Get(ctx, types.NamespacedName{
						Name: ipvc.PvName(),
					}, &pv)
					Ω(err).ToNot(HaveOccurred())

					err = env.clusterReconciler.Client.Get(ctx, types.NamespacedName{
						Name:      newPvcName,
						Namespace: cluster.Namespace,
					}, &pvc)
					Ω(err).ToNot(HaveOccurred())
					Ω(pvc.Spec.VolumeName).To(Equal(pv.Name))
					Ω(pv.Spec.ClaimRef.Name).To(Equal(newPvcName))
					Ω(pv.Spec.PersistentVolumeReclaimPolicy).To(Equal(corev1.PersistentVolumeReclaimDelete))

				}
			})
			By("Pods ends with proper fields", func() {
				for _, instance := range instances {
					var pod corev1.Pod
					err = env.clusterReconciler.Client.Get(ctx, types.NamespacedName{
						Name:      instance.Name,
						Namespace: instance.Namespace,
					}, &pod)
					Ω(err).ToNot(HaveOccurred())
					Ω(pod.ObjectMeta.OwnerReferences).NotTo(BeEmpty())
					Ω(controllerutil.HasOwnerReference(pod.OwnerReferences, cluster, env.scheme)).
						To(BeTrue())
					Ω(pod.ObjectMeta.Labels).To(Equal(map[string]string{
						utils.InstanceNameLabelName: instance.Name,
						utils.ClusterLabelName:      cluster.Name,
						utils.PodRoleLabelName:      "instance",
					}))
					verifyVolumes(pod.Spec.Volumes, ipvcs.ForInstance(instance.Name))
				}
			})
		})
	})
})
