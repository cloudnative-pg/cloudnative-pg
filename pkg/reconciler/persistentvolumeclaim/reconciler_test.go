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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type clientMock struct {
	client.Reader
	client.Writer
	client.StatusClient

	timesCalled int
}

func (cm *clientMock) Patch(
	ctx context.Context, obj client.Object,
	patch client.Patch, opts ...client.PatchOption,
) error {
	cm.timesCalled++
	return nil
}

func (cm *clientMock) Scheme() *runtime.Scheme {
	return &runtime.Scheme{}
}

func (cm *clientMock) RESTMapper() meta.RESTMapper {
	return nil
}

var _ = Describe("Reconcile Resources", func() {
	It("Reconcile existing resources shouldn't fail", func() {
		cl := clientMock{}
		clusterName := "Cluster-pvc-resources"
		pvcs := []corev1.PersistentVolumeClaim{
			makePVC(clusterName, "1", utils.PVCRolePgData, false),
			makePVC(clusterName, "2", utils.PVCRolePgWal, false),      // role is out of sync with name
			makePVC(clusterName, "3-wal", utils.PVCRolePgData, false), // role is out of sync with name
			makePVC(clusterName, "3", utils.PVCRolePgData, false),
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
		pods := []corev1.Pod{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName + "-3",
					Labels: map[string]string{
						specs.ClusterRoleLabelName: specs.ClusterRoleLabelPrimary,
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
				},
			},
		}
		configuration.Current.InheritedAnnotations = []string{"annotation1"}
		configuration.Current.InheritedLabels = []string{"label1"}
		_, err := ReconcileExistingResources(
			context.Background(),
			&cl,
			cluster,
			pods,
			pvcs,
		)
		Expect(err).ToNot(HaveOccurred())
		for _, pvc := range pvcs {
			Expect(pvc.Labels).Should(HaveKey("label1"))
			Expect(pvc.Labels).Should(HaveKey("label2"))
			Expect(pvc.Annotations).Should(HaveKey("annotation1"))
			Expect(pvc.Annotations).Should(HaveKey("annotation2"))
		}
	})
})

var _ = Describe("Reconcile resource requests", func() {
	cl := clientMock{}
	cluster := &apiv1.Cluster{}

	It("Reconcile resources with empty PVCs shouldn't fail", func() {
		err := reconcileResourceRequests(
			context.Background(),
			&cl,
			cluster,
			[]corev1.PersistentVolumeClaim{},
		)
		Expect(err).ToNot(HaveOccurred())
	})

	It("Reconcile resources with resize in use and empty PVCs shouldn't fail", func() {
		cluster.Spec = apiv1.ClusterSpec{
			StorageConfiguration: apiv1.StorageConfiguration{
				ResizeInUseVolumes: pointer.Bool(false),
			},
		}
		err := reconcileResourceRequests(
			context.Background(),
			&cl,
			cluster,
			[]corev1.PersistentVolumeClaim{},
		)
		Expect(err).ToNot(HaveOccurred())
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
		configuration, err := getStorageConfiguration(utils.PVCRolePgData, cluster)
		Expect(configuration).ToNot(BeNil())
		Expect(err).To(BeNil())

		configuration, err = getStorageConfiguration(utils.PVCRolePgWal, cluster)
		Expect(configuration).ToNot(BeNil())
		Expect(err).To(BeNil())
	})

	It("fail if we look for the wrong role ", func() {
		configuration, err := getStorageConfiguration("NoRol", cluster)
		Expect(configuration).To(BeNil())
		Expect(err).ToNot(BeNil())
	})
})

var _ = Describe("Reconcile PVC Quantity", func() {
	cl := clientMock{}
	clusterName := "cluster-pvc-quantity"
	cluster := &apiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterName,
		},
	}
	pvc := makePVC(clusterName, "1", "", false)

	It("fail if we dont' have the proper role", func() {
		err := reconcilePVCQuantity(
			context.Background(),
			&cl,
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
			&cl,
			cluster,
			&pvc)
		Expect(err).To(HaveOccurred())
	})

	It("If we don't have the proper storage configuration it should fail", func() {
		// We add the missing certification
		cluster.Spec.StorageConfiguration = apiv1.StorageConfiguration{}

		// If we don't have a proper storage configuration we should also fail
		err := reconcilePVCQuantity(
			context.Background(),
			&cl,
			cluster,
			&pvc)
		Expect(err).To(HaveOccurred())
	})

	It("It should not fail it's everything it's ok", func() {
		// Now we set the proper storage configuration
		cluster.Spec.StorageConfiguration.Size = "1Gi"

		err := reconcilePVCQuantity(
			context.Background(),
			&cl,
			cluster,
			&pvc)
		Expect(err).ToNot(HaveOccurred())
	})
})
