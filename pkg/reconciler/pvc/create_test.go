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

package pvc

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PVC Creation", func() {
	storageClass := "default"
	It("handles size properly only with size specified", func() {
		pvc, err := Create(
			apiv1.StorageConfiguration{
				Size:         "1Gi",
				StorageClass: &storageClass,
			},
			apiv1.Cluster{},
			0,
			utils.PVCRolePgData,
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(pvc.Spec.Resources.Requests.Storage().String()).To(Equal("1Gi"))
	})
	It("handles size properly with only template specified", func() {
		pvc, err := Create(
			apiv1.StorageConfiguration{
				StorageClass: &storageClass,
				PersistentVolumeClaimTemplate: &corev1.PersistentVolumeClaimSpec{
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{"storage": resource.MustParse("1Gi")},
					},
				},
			},
			apiv1.Cluster{},
			0,
			utils.PVCRolePgData,
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(pvc.Spec.Resources.Requests.Storage().String()).To(Equal("1Gi"))
	})
	It("handles size properly with both template and size specified, size taking precedence", func() {
		pvc, err := Create(
			apiv1.StorageConfiguration{
				Size:         "2Gi",
				StorageClass: &storageClass,
				PersistentVolumeClaimTemplate: &corev1.PersistentVolumeClaimSpec{
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{"storage": resource.MustParse("1Gi")},
					},
				},
			},
			apiv1.Cluster{},
			0,
			utils.PVCRolePgData,
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(pvc.Spec.Resources.Requests.Storage().String()).To(Equal("2Gi"))
	})
})
