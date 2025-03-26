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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("metadataReconciler", func() {
	Describe("newLabelReconciler", func() {
		Context("when a PVC is not up-to-date", func() {
			It("should update the PVC with the correct labels", func() {
				cluster := &apiv1.Cluster{
					Spec: apiv1.ClusterSpec{
						InheritedMetadata: &apiv1.EmbeddedObjectMetadata{Labels: map[string]string{
							"label1": "value1",
							"label2": "value2",
						}},
					},
					Status: apiv1.ClusterStatus{
						InstanceNames: []string{"instance1", "instance2"},
					},
				}
				pvc := &corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pvc1",
						Labels: map[string]string{
							utils.PvcRoleLabelName: string(utils.PVCRolePgData),
						},
					},
				}
				reconciler := newLabelReconciler(cluster)

				// Assert that the PVC is not up-to-date
				Expect(reconciler.isUpToDate(pvc)).To(BeFalse())

				// Update the PVC labels
				reconciler.update(pvc)

				// Assert that the PVC is up-to-date with the correct labels
				Expect(pvc.Labels).To(HaveLen(3))
				Expect(pvc.Labels).To(HaveKeyWithValue("label1", "value1"))
				Expect(pvc.Labels).To(HaveKeyWithValue("label2", "value2"))
				Expect(pvc.Labels).To(HaveKeyWithValue(utils.PvcRoleLabelName, string(utils.PVCRolePgData)))
			})
		})

		Context("when a PVC is up-to-date", func() {
			It("should not update the PVC labels", func() {
				cluster := &apiv1.Cluster{
					Spec: apiv1.ClusterSpec{
						InheritedMetadata: &apiv1.EmbeddedObjectMetadata{Labels: map[string]string{
							"label1": "value1",
							"label2": "value2",
						}},
					},
					Status: apiv1.ClusterStatus{
						InstanceNames: []string{"instance1", "instance2"},
					},
				}
				pvc := &corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pvc1",
						Labels: map[string]string{
							utils.PvcRoleLabelName:      string(utils.PVCRolePgData),
							utils.InstanceNameLabelName: "instance1",
							"label1":                    "value1",
							"label2":                    "value2",
						},
						Annotations: map[string]string{},
					},
				}
				reconciler := newLabelReconciler(cluster)

				// Assert that the PVC is up-to-date
				Expect(reconciler.isUpToDate(pvc)).To(BeTrue())

				// Update the PVC labels
				reconciler.update(pvc)

				// Assert that the PVC labels are unchanged
				Expect(pvc.Labels).To(HaveLen(4))
				Expect(pvc.Labels).To(HaveKeyWithValue(utils.PvcRoleLabelName, string(utils.PVCRolePgData)))
				Expect(pvc.Labels).To(HaveKeyWithValue(utils.InstanceNameLabelName, "instance1"))
			})
		})
	})
})
