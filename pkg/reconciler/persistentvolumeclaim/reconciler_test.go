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

package persistentvolumeclaim

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
	"github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Reconcile Resources", func() {
	It("Reconcile existing resources shouldn't fail and "+
		"it should make sure to add the new instanceRole label to existing PVC", func() {
		clusterName := "Cluster-pvc-resources"
		pvcs := corev1.PersistentVolumeClaimList{
			Items: []corev1.PersistentVolumeClaim{
				makePVC(clusterName, "1", utils.PVCRolePgData, false),
				makePVC(clusterName, "2", utils.PVCRolePgWal, false),      // role is out of sync with name
				makePVC(clusterName, "3-wal", utils.PVCRolePgData, false), // role is out of sync with name
				makePVC(clusterName, "3", utils.PVCRolePgData, false),
			},
		}
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:        clusterName,
				Labels:      map[string]string{"label1": "value"},
				Annotations: map[string]string{"annotation1": "value"},
			},
			Spec: apiv1.ClusterSpec{
				InheritedMetadata: &apiv1.EmbeddedObjectMetadata{
					Labels:      map[string]string{"label2": "value"},
					Annotations: map[string]string{"annotation2": "value"},
				},
				StorageConfiguration: apiv1.StorageConfiguration{
					Size: "1Gi",
				},
				WalStorage: &apiv1.StorageConfiguration{
					Size: "1Gi",
				},
			},
		}

		pods := corev1.PodList{
			Items: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: clusterName + "-3",
						Labels: map[string]string{
							utils.ClusterRoleLabelName: specs.ClusterRoleLabelPrimary,
						},
						Annotations: map[string]string{
							utils.ClusterSerialAnnotationName: "3",
						},
					},
					Spec: corev1.PodSpec{
						Volumes: []corev1.Volume{
							{
								Name: clusterName + "-3",
								VolumeSource: corev1.VolumeSource{
									PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
										ClaimName: clusterName + "-3",
									},
								},
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: clusterName + "-2",
						Labels: map[string]string{
							utils.ClusterRoleLabelName: specs.ClusterRoleLabelReplica,
						},
						Annotations: map[string]string{
							utils.ClusterSerialAnnotationName: "2",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: clusterName + "-1",
						Labels: map[string]string{
							utils.ClusterRoleLabelName: specs.ClusterRoleLabelReplica,
						},
						Annotations: map[string]string{
							utils.ClusterSerialAnnotationName: "1",
						},
					},
				},
			},
		}
		cli := fake.NewClientBuilder().
			WithScheme(scheme.BuildWithAllKnownScheme()).
			WithObjects(cluster).
			WithLists(&pvcs, &pods).
			Build()

		configuration.Current.InheritedAnnotations = []string{"annotation1"}
		configuration.Current.InheritedLabels = []string{"label1"}
		_, err := Reconcile(
			context.Background(),
			cli,
			cluster,
			pods.Items,
			pvcs.Items,
		)
		Expect(err).ToNot(HaveOccurred())
		for _, stalePVC := range pvcs.Items {
			var pvc corev1.PersistentVolumeClaim
			err := cli.Get(context.Background(), types.NamespacedName{Name: stalePVC.Name, Namespace: stalePVC.Namespace}, &pvc)
			Expect(err).ToNot(HaveOccurred())

			Expect(pvc.Labels).Should(HaveKey("label1"))
			Expect(pvc.Labels).Should(HaveKey("label2"))
			Expect(pvc.Labels).Should(HaveKey(utils.ClusterInstanceRoleLabelName), fmt.Sprintf("PVC NAME: %s", pvc.Name))
			Expect(pvc.Annotations).Should(HaveKey("annotation1"))
			Expect(pvc.Annotations).Should(HaveKey("annotation2"))
		}
	})
})

var _ = Describe("Reconcile resource requests", func() {
	cli := fake.NewClientBuilder().WithScheme(scheme.BuildWithAllKnownScheme()).Build()
	cluster := &apiv1.Cluster{}

	It("Reconcile resources with empty PVCs shouldn't fail", func() {
		err := reconcileResourceRequests(
			context.Background(),
			cli,
			cluster,
			[]corev1.PersistentVolumeClaim{},
		)
		Expect(err).ToNot(HaveOccurred())
	})

	It("Reconcile resources with resize in use and empty PVCs shouldn't fail", func() {
		cluster.Spec = apiv1.ClusterSpec{
			StorageConfiguration: apiv1.StorageConfiguration{
				ResizeInUseVolumes: ptr.To(false),
			},
		}

		cli := fake.NewClientBuilder().WithScheme(scheme.BuildWithAllKnownScheme()).WithObjects(cluster).Build()
		err := reconcileResourceRequests(
			context.Background(),
			cli,
			cluster,
			[]corev1.PersistentVolumeClaim{},
		)
		Expect(err).ToNot(HaveOccurred())
	})
})

var _ = Describe("PVC reconciliation", func() {
	const clusterName = "cluster-pvc-reconciliation"

	fetchPVC := func(cl client.Client, pvcToFetch corev1.PersistentVolumeClaim) corev1.PersistentVolumeClaim {
		var pvc corev1.PersistentVolumeClaim
		err := cl.Get(context.Background(),
			types.NamespacedName{Name: pvcToFetch.Name, Namespace: pvcToFetch.Namespace},
			&pvc)
		Expect(err).ToNot(HaveOccurred())
		return pvc
	}

	It("Will reconcile each PVC's with the correct labels", func() {
		pvcs := corev1.PersistentVolumeClaimList{
			Items: []corev1.PersistentVolumeClaim{
				makePVC(clusterName, "1", utils.PVCRolePgData, false),
				makePVC(clusterName, "2", utils.PVCRolePgWal, false),      // role is out of sync with name
				makePVC(clusterName, "3-wal", utils.PVCRolePgData, false), // role is out of sync with name
				makePVC(clusterName, "3", utils.PVCRolePgData, false),
			},
		}
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:        clusterName,
				Labels:      map[string]string{"label1": "value"},
				Annotations: map[string]string{"annotation1": "value"},
			},
			Spec: apiv1.ClusterSpec{
				InheritedMetadata: &apiv1.EmbeddedObjectMetadata{
					Labels:      map[string]string{"label2": "value"},
					Annotations: map[string]string{"annotation2": "value"},
				},
			},
		}
		configuration.Current.InheritedLabels = []string{"label1"}
		pvcs.Items[1].Labels = map[string]string{
			"label1": "value",
			"label2": "value",
		}

		cli := fake.NewClientBuilder().
			WithScheme(scheme.BuildWithAllKnownScheme()).
			WithObjects(cluster).
			WithLists(&pvcs).
			Build()

		err := newLabelReconciler(cluster).reconcile(
			context.Background(),
			cli,
			pvcs.Items,
		)
		Expect(err).ToNot(HaveOccurred())
		Expect(pvcs.Items[2].Labels).To(BeEquivalentTo(map[string]string{
			utils.PvcRoleLabelName: "PG_DATA",
			"label1":               "value",
			"label2":               "value",
		}))

		configuration.Current.InheritedAnnotations = []string{"annotation1"}
		pvcs.Items[1].Annotations = map[string]string{
			"annotation1": "value",
			"annotation2": "value",
		}
		err = newAnnotationReconciler(cluster).reconcile(
			context.Background(),
			cli,
			pvcs.Items,
		)
		Expect(err).ToNot(HaveOccurred())
		Expect(pvcs.Items[2].Annotations).To(BeEquivalentTo(map[string]string{
			utils.PVCStatusAnnotationName:     "ready",
			utils.ClusterSerialAnnotationName: "3-wal",
			"annotation1":                     "value",
			"annotation2":                     "value",
		}))
	})

	It("will reconcile each PVC's pvc-role labels if there are no pods", func() {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:        clusterName,
				Labels:      map[string]string{"label1": "value"},
				Annotations: map[string]string{"annotation1": "value"},
			},
			Spec: apiv1.ClusterSpec{
				InheritedMetadata: &apiv1.EmbeddedObjectMetadata{
					Labels:      map[string]string{"label2": "value"},
					Annotations: map[string]string{"annotation2": "value"},
				},
			},
			Status: apiv1.ClusterStatus{
				InstanceNames: []string{
					fmt.Sprintf("%s-1", clusterName),
					fmt.Sprintf("%s-2", clusterName),
					fmt.Sprintf("%s-3", clusterName),
				},
			},
		}

		pvc := makePVC(clusterName, "1", utils.PVCRolePgData, false)
		pvc2 := makePVC(clusterName, "2", utils.PVCRolePgWal, false)         // role is out of sync with name
		pvc3Wal := makePVC(clusterName, "3-wal", utils.PVCRolePgData, false) // role is out of sync with name
		pvc3Data := makePVC(clusterName, "3", "", false)
		pvcs := []corev1.PersistentVolumeClaim{
			pvc,
			pvc2,
			pvc3Wal,
			pvc3Data,
		}

		cl := fake.NewClientBuilder().WithScheme(scheme.BuildWithAllKnownScheme()).
			WithObjects(&pvc, &pvc2, &pvc3Wal, &pvc3Data).
			Build()

		ctx := context.Background()
		err := newLabelReconciler(cluster).reconcile(
			ctx,
			cl,
			pvcs,
		)
		Expect(err).NotTo(HaveOccurred())

		patchedPvc2 := fetchPVC(cl, pvc2)

		Expect(patchedPvc2.Labels).To(Equal(map[string]string{
			utils.InstanceNameLabelName: "cluster-pvc-reconciliation-2",
			utils.PvcRoleLabelName:      "PG_DATA",
			"label1":                    "value",
			"label2":                    "value",
		}))

		patchedPvc3Wal := fetchPVC(cl, pvc3Wal)
		Expect(patchedPvc3Wal.Labels).To(Equal(map[string]string{
			utils.InstanceNameLabelName: "cluster-pvc-reconciliation-3",
			utils.PvcRoleLabelName:      "PG_WAL",
			"label1":                    "value",
			"label2":                    "value",
		}))

		patchedPvc3Data := fetchPVC(cl, pvc3Data)
		Expect(patchedPvc3Data.Labels).To(Equal(map[string]string{
			utils.InstanceNameLabelName: "cluster-pvc-reconciliation-3",
			utils.PvcRoleLabelName:      "PG_DATA",
			"label1":                    "value",
			"label2":                    "value",
		}))
	})

	It("will reconcile each PVC's instance-relative labels by invoking the instance metadata reconciler", func() {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test-name", Namespace: "test-namespace"},
			Spec:       apiv1.ClusterSpec{WalStorage: &apiv1.StorageConfiguration{Size: "1Gi"}},
		}

		pods := []corev1.Pod{
			makePod(clusterName, "1", specs.ClusterRoleLabelPrimary),
			makePod(clusterName, "2", specs.ClusterRoleLabelReplica),
			makePod(clusterName, "3", specs.ClusterRoleLabelReplica),
		}

		pvc := makePVC(clusterName, "1", utils.PVCRolePgData, false)
		pvc2 := makePVC(clusterName, "2", utils.PVCRolePgData, false)
		pvc3Wal := makePVC(clusterName, "3-wal", utils.PVCRolePgWal, false)
		pvc3Data := makePVC(clusterName, "3", utils.PVCRolePgData, false)
		pvcs := []corev1.PersistentVolumeClaim{
			pvc,
			pvc2,
			pvc3Wal,
			pvc3Data,
		}

		cl := fake.NewClientBuilder().WithScheme(scheme.BuildWithAllKnownScheme()).
			WithObjects(&pvc, &pvc2, &pvc3Wal, &pvc3Data).
			Build()

		err := reconcileMetadataComingFromInstance(
			context.Background(),
			cl,
			cluster,
			pods,
			pvcs)
		Expect(err).NotTo(HaveOccurred())

		patchedPvc := fetchPVC(cl, pvc)
		Expect(patchedPvc.Labels).To(Equal(map[string]string{
			utils.PvcRoleLabelName:             "PG_DATA",
			utils.ClusterRoleLabelName:         "primary",
			utils.ClusterInstanceRoleLabelName: "primary",
		}))

		patchedPvc2 := fetchPVC(cl, pvc2)
		Expect(patchedPvc2.Labels).To(Equal(map[string]string{
			utils.PvcRoleLabelName:             "PG_DATA",
			utils.ClusterRoleLabelName:         "replica",
			utils.ClusterInstanceRoleLabelName: "replica",
		}))

		patchedPvc3Wal := fetchPVC(cl, pvc3Wal)
		Expect(patchedPvc3Wal.Labels).To(Equal(map[string]string{
			utils.PvcRoleLabelName:             "PG_WAL",
			utils.ClusterRoleLabelName:         "replica",
			utils.ClusterInstanceRoleLabelName: "replica",
		}))

		patchedPvc3Data := fetchPVC(cl, pvc3Data)
		Expect(patchedPvc3Data.Labels).To(Equal(map[string]string{
			utils.PvcRoleLabelName:             "PG_DATA",
			utils.ClusterRoleLabelName:         "replica",
			utils.ClusterInstanceRoleLabelName: "replica",
		}))
	})
})

var _ = Describe("Storage configuration", func() {
	cluster := &apiv1.Cluster{
		Spec: apiv1.ClusterSpec{
			StorageConfiguration: apiv1.StorageConfiguration{},
			WalStorage:           &apiv1.StorageConfiguration{},
		},
	}

	It("Should not fail when the roles it's correct", func() {
		configuration, err := getStorageConfiguration(cluster, utils.PVCRolePgData)
		Expect(configuration).ToNot(BeNil())
		Expect(err).ToNot(HaveOccurred())

		configuration, err = getStorageConfiguration(cluster, utils.PVCRolePgWal)
		Expect(configuration).ToNot(BeNil())
		Expect(err).ToNot(HaveOccurred())
	})

	It("fail if we look for the wrong role", func() {
		configuration, err := getStorageConfiguration(cluster, "NoRol")
		Expect(err).To(HaveOccurred())
		Expect(configuration.StorageClass).To(BeNil())
	})
})

var _ = Describe("Storage source", func() {
	When("bootstrapping from a VolumeSnapshot", func() {
		pgDataSnapshotVolumeName := "pgdata-snapshot"
		pgWalSnapshotVolumeName := "pgwal-snapshot"
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				StorageConfiguration: apiv1.StorageConfiguration{},
				WalStorage:           &apiv1.StorageConfiguration{},
				Bootstrap: &apiv1.BootstrapConfiguration{
					Recovery: &apiv1.BootstrapRecovery{
						VolumeSnapshots: &apiv1.DataSource{
							Storage: corev1.TypedLocalObjectReference{
								Name:     pgDataSnapshotVolumeName,
								Kind:     "VolumeSnapshot",
								APIGroup: ptr.To("snapshot.storage.k8s.io"),
							},
							WalStorage: &corev1.TypedLocalObjectReference{
								Name:     pgWalSnapshotVolumeName,
								Kind:     "VolumeSnapshot",
								APIGroup: ptr.To("snapshot.storage.k8s.io"),
							},
						},
					},
				},
			},
		}

		When("working on the first instance", func() {
			It("should fail when looking for a wrong role", func() {
				_, err := getStorageSource(cluster, "NoRol", 1)
				Expect(err).To(HaveOccurred())
			})

			It("should return the correct source when chosing pgdata", func() {
				source, err := getStorageSource(cluster, utils.PVCRolePgData, 1)
				Expect(err).ToNot(HaveOccurred())
				Expect(source).ToNot(BeNil())
				Expect(source.Name).To(Equal(pgDataSnapshotVolumeName))
			})

			It("should return the correct source when chosing pgwal", func() {
				source, err := getStorageSource(cluster, utils.PVCRolePgWal, 1)
				Expect(err).ToNot(HaveOccurred())
				Expect(source).ToNot(BeNil())
				Expect(source.Name).To(Equal(pgWalSnapshotVolumeName))
			})
		})

		When("working on instances beside the first one", func() {
			It("should always return nil", func() {
				source, err := getStorageSource(cluster, utils.PVCRolePgData, 2)
				Expect(err).ToNot(HaveOccurred())
				Expect(source).To(BeNil())
			})
		})
	})

	When("not bootstrapping from a VolumeSnapshot", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				StorageConfiguration: apiv1.StorageConfiguration{},
				WalStorage:           &apiv1.StorageConfiguration{},
			},
		}

		It("should return an empty storage source", func() {
			source, err := getStorageSource(cluster, utils.PVCRolePgData, 1)
			Expect(err).ToNot(HaveOccurred())
			Expect(source).To(BeNil())
		})
	})
})

var _ = Describe("Reconcile PVC Quantity", func() {
	var (
		clusterName = "cluster-pvc-quantity"
		cluster     *apiv1.Cluster
		pvc         corev1.PersistentVolumeClaim
		cli         client.Client
	)

	BeforeEach(func() {
		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterName,
			},
		}
		pvc = makePVC(clusterName, "1", "", false)

		cli = fake.NewClientBuilder().
			WithScheme(scheme.BuildWithAllKnownScheme()).
			WithObjects(cluster, &pvc).
			Build()
	})

	It("fail if we dont' have the proper role", func() {
		err := reconcilePVCQuantity(
			context.Background(),
			cli,
			cluster,
			&pvc)
		Expect(err).To(HaveOccurred())
	})

	It("Without the proper storage configuration it should always fail", func() {
		pvc.Labels = map[string]string{
			utils.PvcRoleLabelName: string(utils.PVCRolePgData),
		}

		err := reconcilePVCQuantity(
			context.Background(),
			cli,
			cluster,
			&pvc)
		Expect(err).To(HaveOccurred())
	})

	It("If we don't have the proper storage configuration it should fail", func() {
		cluster.Spec.StorageConfiguration = apiv1.StorageConfiguration{}

		// If we don't have a proper storage configuration we should also fail
		err := reconcilePVCQuantity(
			context.Background(),
			cli,
			cluster,
			&pvc)
		Expect(err).To(HaveOccurred())
	})

	It("It should not fail it's everything it's ok", func() {
		pvc.Labels = map[string]string{
			utils.PvcRoleLabelName: string(utils.PVCRolePgData),
		}
		cluster.Spec.StorageConfiguration.Size = "1Gi"

		err := reconcilePVCQuantity(
			context.Background(),
			cli,
			cluster,
			&pvc)
		Expect(err).ToNot(HaveOccurred())
	})
})
