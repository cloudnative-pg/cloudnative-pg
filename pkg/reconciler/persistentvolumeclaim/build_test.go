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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PVC Creation", func() {
	storageClass := "default"
	It("handles size properly only with size specified", func() {
		pvc, err := Build(
			&apiv1.Cluster{},
			&CreateConfiguration{
				Status:     StatusInitializing,
				NodeSerial: 0,
				Calculator: NewPgDataCalculator(),
				Storage: apiv1.StorageConfiguration{
					Size:         "1Gi",
					StorageClass: &storageClass,
				},
			},
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(pvc.Spec.Resources.Requests.Storage().String()).To(Equal("1Gi"))
	})
	It("handles size properly with only template specified", func() {
		pvc, err := Build(
			&apiv1.Cluster{},
			&CreateConfiguration{
				Status: StatusInitializing,
				Storage: apiv1.StorageConfiguration{
					StorageClass: &storageClass,
					PersistentVolumeClaimTemplate: &apiv1.PVCTemplate{
						PersistentVolumeClaimSpec: corev1.PersistentVolumeClaimSpec{
							Resources: corev1.VolumeResourceRequirements{
								Requests: corev1.ResourceList{"storage": resource.MustParse("1Gi")},
							},
						},
					},
				},
				Calculator: NewPgDataCalculator(),
			},
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(pvc.Spec.Resources.Requests.Storage().String()).To(Equal("1Gi"))
	})
	It("handles size properly with both template and size specified, size taking precedence", func() {
		pvc, err := Build(
			&apiv1.Cluster{},
			&CreateConfiguration{
				Status:     StatusInitializing,
				NodeSerial: 0,
				Calculator: NewPgDataCalculator(),
				Storage: apiv1.StorageConfiguration{
					Size:         "2Gi",
					StorageClass: &storageClass,
					PersistentVolumeClaimTemplate: &apiv1.PVCTemplate{
						PersistentVolumeClaimSpec: corev1.PersistentVolumeClaimSpec{
							Resources: corev1.VolumeResourceRequirements{
								Requests: corev1.ResourceList{"storage": resource.MustParse("1Gi")},
							},
						},
					},
				},
			},
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(pvc.Spec.Resources.Requests.Storage().String()).To(Equal("2Gi"))
	})

	It("fail with the a zero size", func() {
		_, err := Build(
			&apiv1.Cluster{},
			&CreateConfiguration{
				Status:     StatusInitializing,
				NodeSerial: 0,
				Calculator: NewPgDataCalculator(),
				Storage: apiv1.StorageConfiguration{
					Size:         "0Gi",
					StorageClass: &storageClass,
				},
			},
		)
		Expect(err).To(HaveOccurred())
	})

	It("fail with the a wrong size", func() {
		_, err := Build(
			&apiv1.Cluster{},
			&CreateConfiguration{
				Status:     StatusInitializing,
				NodeSerial: 0,
				Calculator: NewPgDataCalculator(),
				Storage: apiv1.StorageConfiguration{
					Size:         "nil",
					StorageClass: &storageClass,
				},
			},
		)
		Expect(err).To(HaveOccurred())
	})

	It("builds pvc with correct size and the tablespace name for tablespaces", func() {
		tbsName := "fragglerock"
		pvc, err := Build(
			&apiv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "thecluster",
				},
				Spec: apiv1.ClusterSpec{},
			},
			&CreateConfiguration{
				Status:     StatusInitializing,
				NodeSerial: 1,
				Calculator: NewPgTablespaceCalculator(tbsName),
				Storage: apiv1.StorageConfiguration{
					Size:         "2Gi",
					StorageClass: &storageClass,
					PersistentVolumeClaimTemplate: &apiv1.PVCTemplate{
						PersistentVolumeClaimSpec: corev1.PersistentVolumeClaimSpec{
							Resources: corev1.VolumeResourceRequirements{
								Requests: corev1.ResourceList{"storage": resource.MustParse("1Gi")},
							},
						},
					},
				},
				TablespaceName: tbsName,
			},
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(pvc.Name).To(Equal("thecluster-1-tbs-fragglerock"))
		Expect(pvc.Spec.Resources.Requests.Storage().String()).To(Equal("2Gi"))
		Expect(pvc.Labels[utils.TablespaceNameLabelName]).To(Equal(tbsName))
	})

	It("should not add the default access mode when the PVC template specifies at least one value", func() {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test",
			},
		}
		pvc, err := Build(cluster, &CreateConfiguration{
			NodeSerial: 1,
			Calculator: NewPgDataCalculator(),
			Storage: apiv1.StorageConfiguration{
				Size: "1Gi",
				PersistentVolumeClaimTemplate: &apiv1.PVCTemplate{
					PersistentVolumeClaimSpec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOncePod},
					},
				},
			},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(pvc.Spec.AccessModes).To(HaveLen(1))
		Expect(pvc.Spec.AccessModes).To(ContainElement(corev1.ReadWriteOncePod))
	})

	It("should add readWriteOnce to the template if no access mode is specified", func() {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{},
		}
		pvc, err := Build(cluster, &CreateConfiguration{
			NodeSerial: 1,
			Calculator: NewPgDataCalculator(),
			Storage: apiv1.StorageConfiguration{
				Size: "1Gi",
				PersistentVolumeClaimTemplate: &apiv1.PVCTemplate{
					PersistentVolumeClaimSpec: corev1.PersistentVolumeClaimSpec{
						Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"test": "test"}},
					},
				},
			},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(pvc.Spec.AccessModes).To(HaveLen(1))
		Expect(pvc.Spec.AccessModes).To(ContainElement(corev1.ReadWriteOnce))
	})

	It("should propagate user-defined labels and annotations from PVC template metadata", func() {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-cluster",
			},
		}
		pvc, err := Build(cluster, &CreateConfiguration{
			Status:     StatusInitializing,
			NodeSerial: 1,
			Calculator: NewPgDataCalculator(),
			Storage: apiv1.StorageConfiguration{
				Size: "1Gi",
				PersistentVolumeClaimTemplate: &apiv1.PVCTemplate{
					Metadata: apiv1.PVCMetadata{
						Labels: map[string]string{
							"app.kubernetes.io/team": "database",
						},
						Annotations: map[string]string{
							"backup.velero.io/backup-volumes": "data",
						},
					},
				},
			},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(pvc.Labels).To(HaveKeyWithValue("app.kubernetes.io/team", "database"))
		Expect(pvc.Annotations).To(HaveKeyWithValue("backup.velero.io/backup-volumes", "data"))
	})

	It("should ensure operator metadata takes precedence over user metadata on collision", func() {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-cluster",
			},
		}
		pvc, err := Build(cluster, &CreateConfiguration{
			Status:     StatusInitializing,
			NodeSerial: 1,
			Calculator: NewPgDataCalculator(),
			Storage: apiv1.StorageConfiguration{
				Size: "1Gi",
				PersistentVolumeClaimTemplate: &apiv1.PVCTemplate{
					Metadata: apiv1.PVCMetadata{
						Annotations: map[string]string{
							utils.PVCStatusAnnotationName: "should-be-overwritten",
						},
					},
				},
			},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(pvc.Annotations[utils.PVCStatusAnnotationName]).To(Equal(StatusInitializing))
	})
})
