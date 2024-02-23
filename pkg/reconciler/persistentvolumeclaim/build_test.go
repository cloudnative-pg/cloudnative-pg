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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/api/v1/resources"

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
					PersistentVolumeClaimTemplate: &corev1.PersistentVolumeClaimSpec{
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{"storage": resource.MustParse("1Gi")},
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
					PersistentVolumeClaimTemplate: &corev1.PersistentVolumeClaimSpec{
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{"storage": resource.MustParse("1Gi")},
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
				ObjectMeta: v1.ObjectMeta{
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
					PersistentVolumeClaimTemplate: &corev1.PersistentVolumeClaimSpec{
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{"storage": resource.MustParse("1Gi")},
						},
					},
				},
				TablespaceName: tbsName,
			},
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(pvc.Name).To(Equal("thecluster-1-tbs-fragglerock"))
		Expect(pvc.Spec.Resources.Requests.Storage().String()).To(Equal("2Gi"))
		Expect(pvc.Labels[resources.TablespaceNameLabelName]).To(Equal(tbsName))
	})
})
