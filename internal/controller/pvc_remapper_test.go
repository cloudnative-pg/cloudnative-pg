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

	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/pvcremapper"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/persistentvolumeclaim"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("unit test for pvc remapper logic", func() {
	var env *testingEnvironment

	BeforeEach(func() {
		env = buildTestEnvironment()
		configuration.Current = configuration.NewConfiguration()
	})

	AfterEach(func() {
		configuration.Current = configuration.NewConfiguration()
	})

	When("remapping is required", func() {
		ctx := context.Background()
		It("should rename PVC's", func() {
			namespace := newFakeNamespace(env.client)
			cluster := newFakeCNPGCluster(
				env.client,
				namespace,
				func(cluster *v1.Cluster) {
					cluster.Spec.WalStorage = &v1.StorageConfiguration{
						Size: "1Gi",
					}
				},
			)
			instances := generateFakeClusterPods(env.client, cluster, true)
			pvcs := generateClusterPVC(env.client, cluster, persistentvolumeclaim.StatusReady)
			By("checking that remap finishes succesfully", func() {

				configuration.Current.DataVolumeSuffix = "-data2"
				configuration.Current.WalArchiveVolumeSuffix = "-wal2"

				ipvcs, err := pvcremapper.InstancePVCsFromPVCs(pvcs)
				Ω(err).NotTo(HaveOccurred())

				for _, instance := range instances {
					done, err := env.clusterReconciler.remap(ctx, &instance, ipvcs)
					Ω(err).NotTo(HaveOccurred())
					Ω(done).To(BeTrue())
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
		})
	})
})
