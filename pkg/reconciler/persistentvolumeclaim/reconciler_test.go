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

package persistentvolumeclaim

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
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
	"github.com/cloudnative-pg/cloudnative-pg/pkg/versions"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Reconcile Metadata", func() {
	It("Reconcile existing resources shouldn't fail and "+
		"it should make sure to add the new instanceRole label to existing PVC", func() {
		clusterName := "Cluster-pvc-resources"
		pvcs := corev1.PersistentVolumeClaimList{
			Items: []corev1.PersistentVolumeClaim{
				makePVC(clusterName, "1", "1", NewPgDataCalculator(), false),
				makePVC(clusterName, "2", "2", NewPgWalCalculator(), false),      // role is out of sync with name
				makePVC(clusterName, "3-wal", "3", NewPgDataCalculator(), false), // role is out of sync with name
				makePVC(clusterName, "3", "3", NewPgDataCalculator(), false),
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
			Status: apiv1.ClusterStatus{
				CurrentPrimary: clusterName + "-1",
				InstanceNames:  []string{clusterName + "-1", clusterName + "-2", clusterName + "-3"},
			},
		}

		pods := corev1.PodList{
			Items: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: clusterName + "-3",
						Labels: map[string]string{
							//nolint:staticcheck
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
							//nolint:staticcheck
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
							//nolint:staticcheck
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
		err := ReconcileMetadata(
			context.Background(),
			cli,
			cluster,
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
		err := reconcileExistingPVCs(
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
		err := reconcileExistingPVCs(
			context.Background(),
			cli,
			cluster,
			[]corev1.PersistentVolumeClaim{},
		)
		Expect(err).ToNot(HaveOccurred())
	})
})

var _ = Describe("PVC reconciliation", Ordered, func() {
	const clusterName = "cluster-pvc-reconciliation"

	fetchPVC := func(cl client.Client, pvcToFetch corev1.PersistentVolumeClaim) corev1.PersistentVolumeClaim {
		var pvc corev1.PersistentVolumeClaim
		err := cl.Get(context.Background(),
			types.NamespacedName{Name: pvcToFetch.Name, Namespace: pvcToFetch.Namespace},
			&pvc)
		Expect(err).ToNot(HaveOccurred())
		return pvc
	}

	assertCommonLabels := func(pvcLabels map[string]string) {
		Expect(pvcLabels).To(HaveKeyWithValue(utils.KubernetesAppManagedByLabelName, utils.ManagerName))
		Expect(pvcLabels).To(HaveKeyWithValue(utils.KubernetesAppLabelName, utils.AppName))
		Expect(pvcLabels).To(HaveKeyWithValue(utils.KubernetesAppComponentLabelName, utils.DatabaseComponentName))
	}

	It("Will reconcile each PVC's with the correct labels", func() {
		pvcs := corev1.PersistentVolumeClaimList{
			Items: []corev1.PersistentVolumeClaim{
				makePVC(clusterName, "1", "1", NewPgDataCalculator(), false),
				makePVC(clusterName, "2", "2", NewPgWalCalculator(), false),      // role is out of sync with name
				makePVC(clusterName, "3-wal", "3", NewPgDataCalculator(), false), // role is out of sync with name
				makePVC(clusterName, "3", "3", NewPgDataCalculator(), false),
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
		Expect(pvcs.Items[2].Labels).To(HaveKeyWithValue("label1", "value"))
		Expect(pvcs.Items[2].Labels).To(HaveKeyWithValue("label2", "value"))
		Expect(pvcs.Items[2].Labels).To(HaveKeyWithValue(utils.PvcRoleLabelName, string(utils.PVCRolePgData)))
		Expect(pvcs.Items[2].Labels).To(HaveKeyWithValue(utils.InstanceNameLabelName, clusterName+"-3-wal"))
		assertCommonLabels(pvcs.Items[2].Labels)

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
			utils.PVCStatusAnnotationName:       "ready",
			utils.ClusterSerialAnnotationName:   "3",
			"annotation1":                       "value",
			"annotation2":                       "value",
			utils.OperatorVersionAnnotationName: versions.Version,
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

		pvc := makePVC(clusterName, "1", "1", NewPgDataCalculator(), false)
		pvc2 := makePVC(clusterName, "2", "2", NewPgWalCalculator(), false)         // role is out of sync with name
		pvc3Wal := makePVC(clusterName, "3-wal", "3", NewPgDataCalculator(), false) // role is out of sync with name
		pvc3Data := makePVC(clusterName, "3", "3", nil, false)
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
		patchedPvc3Wal := fetchPVC(cl, pvc3Wal)
		patchedPvc3Data := fetchPVC(cl, pvc3Data)

		for _, pvc := range []corev1.PersistentVolumeClaim{patchedPvc2, patchedPvc3Wal, patchedPvc3Data} {
			role := string(utils.PVCRolePgData)
			if strings.Contains(pvc.Name, "-wal") {
				role = string(utils.PVCRolePgWal)
			}
			Expect(pvc.Labels).To(HaveKeyWithValue("label1", "value"))
			Expect(pvc.Labels).To(HaveKeyWithValue("label2", "value"))
			Expect(pvc.Labels).To(HaveKeyWithValue(utils.PvcRoleLabelName, role))
			Expect(pvc.Labels).To(HaveKeyWithValue(utils.InstanceNameLabelName,
				fmt.Sprintf("%s-%s", clusterName, pvc.Annotations[utils.ClusterSerialAnnotationName])))
			assertCommonLabels(pvc.Labels)
		}
	})

	It("will reconcile each PVC's instance-relative labels by invoking the instance metadata reconciler", func() {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: "test-namespace"},
			Spec:       apiv1.ClusterSpec{WalStorage: &apiv1.StorageConfiguration{Size: "1Gi"}},
			Status: apiv1.ClusterStatus{
				CurrentPrimary: clusterName + "-1",
				InstanceNames:  []string{clusterName + "-1", clusterName + "-2", clusterName + "-3"},
			},
		}

		pods := []corev1.Pod{
			makePod(clusterName, "1", specs.ClusterRoleLabelPrimary),
			makePod(clusterName, "2", specs.ClusterRoleLabelReplica),
			makePod(clusterName, "3", specs.ClusterRoleLabelReplica),
		}

		pvc := makePVC(clusterName, "1", "0", NewPgDataCalculator(), false)
		pvc2 := makePVC(clusterName, "2", "0", NewPgDataCalculator(), false)
		pvc3Wal := makePVC(clusterName, "3-wal", "0", NewPgWalCalculator(), false)
		pvc3Data := makePVC(clusterName, "3", "0", NewPgDataCalculator(), false)
		pvcs := []corev1.PersistentVolumeClaim{
			pvc,
			pvc2,
			pvc3Wal,
			pvc3Data,
		}

		cl := fake.NewClientBuilder().WithScheme(scheme.BuildWithAllKnownScheme()).
			WithObjects(&pvc, &pvc2, &pvc3Wal, &pvc3Data).
			Build()

		err := ReconcileMetadata(
			context.Background(),
			cl,
			cluster,
			pvcs)
		Expect(err).NotTo(HaveOccurred())

		err = ReconcileSerialAnnotation(
			context.Background(),
			cl,
			cluster,
			pods,
			pvcs)
		Expect(err).NotTo(HaveOccurred())

		patchedPvc := fetchPVC(cl, pvc)
		patchedPvc2 := fetchPVC(cl, pvc2)
		patchedPvc3Wal := fetchPVC(cl, pvc3Wal)
		patchedPvc3Data := fetchPVC(cl, pvc3Data)

		// Serial annotation
		Expect(patchedPvc.Annotations).To(HaveKeyWithValue(utils.ClusterSerialAnnotationName, "1"))
		Expect(patchedPvc2.Annotations).To(HaveKeyWithValue(utils.ClusterSerialAnnotationName, "2"))
		Expect(patchedPvc3Wal.Annotations).To(HaveKeyWithValue(utils.ClusterSerialAnnotationName, "3"))
		Expect(patchedPvc3Data.Annotations).To(HaveKeyWithValue(utils.ClusterSerialAnnotationName, "3"))

		patchedPvcs := []corev1.PersistentVolumeClaim{
			patchedPvc,
			patchedPvc2,
			patchedPvc3Wal,
			patchedPvc3Data,
		}
		for _, pvc := range patchedPvcs {
			role := string(utils.PVCRolePgData)
			if strings.Contains(pvc.Name, "-wal") {
				role = string(utils.PVCRolePgWal)
			}
			clusterRole := specs.ClusterRoleLabelPrimary
			if pvc.Annotations[utils.ClusterSerialAnnotationName] != "1" {
				clusterRole = specs.ClusterRoleLabelReplica
			}
			// Labels
			Expect(pvc.Labels).To(HaveKeyWithValue(utils.ClusterRoleLabelName, clusterRole)) //nolint:staticcheck
			Expect(pvc.Labels).To(HaveKeyWithValue(utils.ClusterInstanceRoleLabelName, clusterRole))
			Expect(pvc.Labels).To(HaveKeyWithValue(utils.PvcRoleLabelName, role))
			Expect(pvc.Labels).To(HaveKeyWithValue(utils.InstanceNameLabelName,
				fmt.Sprintf("%s-%s", clusterName, pvc.Annotations[utils.ClusterSerialAnnotationName])))
			assertCommonLabels(pvc.Labels)

			// Annotations
			Expect(pvc.Annotations).To(HaveKeyWithValue(utils.PVCStatusAnnotationName, "ready"))
			Expect(pvc.Annotations).To(HaveKeyWithValue(utils.OperatorVersionAnnotationName, versions.Version))
		}
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
		configuration, err := NewPgDataCalculator().GetStorageConfiguration(cluster)
		Expect(configuration).ToNot(BeNil())
		Expect(err).ToNot(HaveOccurred())

		configuration, err = NewPgWalCalculator().GetStorageConfiguration(cluster)
		Expect(configuration).ToNot(BeNil())
		Expect(err).ToNot(HaveOccurred())
	})
})

var _ = Describe("Reconcile PVC Quantity", func() {
	var (
		clusterName = "cluster-pvc-quantity"
		cluster     *apiv1.Cluster
		pvc         corev1.PersistentVolumeClaim
		pvc2        corev1.PersistentVolumeClaim
		cli         client.Client
	)

	BeforeEach(func() {
		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterName,
			},
		}
		pvc = makePVC(clusterName, "1", "1", NewPgDataCalculator(), false)
		tbsName := "fragglerock"
		pvc2 = makePVC(clusterName, "2", "2", NewPgTablespaceCalculator(tbsName), false)
		pvc2.Spec.Resources.Requests = map[corev1.ResourceName]resource.Quantity{
			"storage": resource.MustParse("3Gi"),
		}

		cli = fake.NewClientBuilder().
			WithScheme(scheme.BuildWithAllKnownScheme()).
			WithObjects(cluster, &pvc, &pvc2).
			Build()
	})

	It("fail if we dont' have the proper role", func() {
		pvcRole, err := GetExpectedObjectCalculator(pvc.GetLabels())
		Expect(err).ToNot(HaveOccurred())

		storageConfiguration, err := pvcRole.GetStorageConfiguration(cluster)
		Expect(err).ToNot(HaveOccurred())

		err = reconcilePVCQuantity(context.Background(), cli, &storageConfiguration, &pvc)
		Expect(err).To(HaveOccurred())
	})

	It("Without the proper storage configuration it should always fail", func() {
		pvc.Labels = map[string]string{
			utils.PvcRoleLabelName: string(utils.PVCRolePgData),
		}

		pvcRole, err := GetExpectedObjectCalculator(pvc.GetLabels())
		Expect(err).ToNot(HaveOccurred())

		storageConfiguration, err := pvcRole.GetStorageConfiguration(cluster)
		Expect(err).ToNot(HaveOccurred())

		err = reconcilePVCQuantity(
			context.Background(),
			cli,
			&storageConfiguration,
			&pvc)
		Expect(err).To(HaveOccurred())
	})

	It("If we don't have the proper storage configuration it should fail", func() {
		cluster.Spec.StorageConfiguration = apiv1.StorageConfiguration{}

		pvcRole, err := GetExpectedObjectCalculator(pvc.GetLabels())
		Expect(err).ToNot(HaveOccurred())

		storageConfiguration, err := pvcRole.GetStorageConfiguration(cluster)
		Expect(err).ToNot(HaveOccurred())

		// If we don't have a proper storage configuration we should also fail
		err = reconcilePVCQuantity(
			context.Background(),
			cli,
			&storageConfiguration,
			&pvc)
		Expect(err).To(HaveOccurred())
	})

	It("It should not fail it's everything is ok", func() {
		pvc.Labels = map[string]string{
			utils.PvcRoleLabelName: string(utils.PVCRolePgData),
		}
		cluster.Spec.StorageConfiguration.Size = "1Gi"

		pvcRole, err := GetExpectedObjectCalculator(pvc.GetLabels())
		Expect(err).ToNot(HaveOccurred())

		storageConfiguration, err := pvcRole.GetStorageConfiguration(cluster)
		Expect(err).ToNot(HaveOccurred())

		err = reconcilePVCQuantity(
			context.Background(),
			cli,
			&storageConfiguration,
			&pvc)
		Expect(err).ToNot(HaveOccurred())
	})

	It("It should succeed increasing size of tablespaces", func() {
		// Now we set the proper storage configuration
		cluster.Spec.Tablespaces = []apiv1.TablespaceConfiguration{
			{
				Name: "fragglerock",
				Storage: apiv1.StorageConfiguration{
					Size: "4Gi",
				},
			},
		}

		pvcRole, err := GetExpectedObjectCalculator(pvc2.GetLabels())
		Expect(err).ToNot(HaveOccurred())

		storageConfiguration, err := pvcRole.GetStorageConfiguration(cluster)
		Expect(err).ToNot(HaveOccurred())

		err = reconcilePVCQuantity(
			context.Background(),
			cli,
			&storageConfiguration,
			&pvc2)
		Expect(err).ToNot(HaveOccurred())
	})

	It("It should succeed decreasing size of tablespaces", func() {
		// Now we set the proper storage configuration
		cluster.Spec.Tablespaces = []apiv1.TablespaceConfiguration{
			{
				Name: "fragglerock",
				Storage: apiv1.StorageConfiguration{
					Size: "2Gi",
				},
			},
		}

		pvcRole, err := GetExpectedObjectCalculator(pvc2.GetLabels())
		Expect(err).ToNot(HaveOccurred())

		storageConfiguration, err := pvcRole.GetStorageConfiguration(cluster)
		Expect(err).ToNot(HaveOccurred())

		err = reconcilePVCQuantity(
			context.Background(),
			cli,
			&storageConfiguration,
			&pvc2)
		Expect(err).ToNot(HaveOccurred())
	})
})

var _ = Describe("Reconcile Volume Attribute Class", func() {
	var (
		clusterName = "cluster-volume-attr"
		cluster     *apiv1.Cluster
		pvc         corev1.PersistentVolumeClaim
		cli         client.Client
		ctx         context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()
		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterName,
			},
		}
		pvc = makePVC(clusterName, "1", "1", NewPgDataCalculator(), false)
		cli = fake.NewClientBuilder().
			WithScheme(scheme.BuildWithAllKnownScheme()).
			WithObjects(cluster, &pvc).
			Build()
	})

	It("does nothing if PersistentVolumeClaimTemplate is nil", func() {
		storage := &apiv1.StorageConfiguration{
			Size:                          "1Gi",
			PersistentVolumeClaimTemplate: nil,
		}

		err := reconcileVolumeAttributeClass(ctx, cli, storage, &pvc)
		Expect(err).NotTo(HaveOccurred())
		Expect(pvc.Spec.VolumeAttributesClassName).To(BeNil())
	})

	It("does nothing if VolumeAttributesClassName is already the expected value", func() {
		className := "fast-class"
		pvc.Spec.VolumeAttributesClassName = &className

		storage := &apiv1.StorageConfiguration{
			Size: "1Gi",
			PersistentVolumeClaimTemplate: &apiv1.PVCTemplate{
				PersistentVolumeClaimSpec: corev1.PersistentVolumeClaimSpec{
					VolumeAttributesClassName: &className,
				},
			},
		}

		err := reconcileVolumeAttributeClass(ctx, cli, storage, &pvc)
		Expect(err).NotTo(HaveOccurred())
		Expect(*pvc.Spec.VolumeAttributesClassName).To(Equal(className))
	})

	It("updates VolumeAttributesClassName when it differs from the expected value", func() {
		currentClassName := "slow-class"
		expectedClassName := "fast-class"
		pvc.Spec.VolumeAttributesClassName = &currentClassName

		storage := &apiv1.StorageConfiguration{
			Size: "1Gi",
			PersistentVolumeClaimTemplate: &apiv1.PVCTemplate{
				PersistentVolumeClaimSpec: corev1.PersistentVolumeClaimSpec{
					VolumeAttributesClassName: &expectedClassName,
				},
			},
		}

		err := reconcileVolumeAttributeClass(ctx, cli, storage, &pvc)
		Expect(err).NotTo(HaveOccurred())
		Expect(*pvc.Spec.VolumeAttributesClassName).To(Equal(expectedClassName))
	})

	It("sets VolumeAttributesClassName to nil when template specifies nil", func() {
		className := "existing-class"
		pvc.Spec.VolumeAttributesClassName = &className

		storage := &apiv1.StorageConfiguration{
			Size: "1Gi",
			PersistentVolumeClaimTemplate: &apiv1.PVCTemplate{
				PersistentVolumeClaimSpec: corev1.PersistentVolumeClaimSpec{
					VolumeAttributesClassName: nil,
				},
			},
		}

		err := reconcileVolumeAttributeClass(ctx, cli, storage, &pvc)
		Expect(err).NotTo(HaveOccurred())
		Expect(pvc.Spec.VolumeAttributesClassName).To(BeNil())
	})
})
