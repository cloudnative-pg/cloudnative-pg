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

package specs

import (
	"context"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PVC detection", func() {
	It("will list PVCs with Jobs or Pods or which are Ready", func() {
		pvcs := []corev1.PersistentVolumeClaim{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "pvcForPod",
					Labels: map[string]string{
						utils.PvcRoleLabelName: string(utils.PVCRolePgData),
					},
				},
				Spec: corev1.PersistentVolumeClaimSpec{},
				Status: corev1.PersistentVolumeClaimStatus{
					Phase: corev1.ClaimBound,
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "pvcForJob",
					Labels: map[string]string{
						utils.PvcRoleLabelName: string(utils.PVCRolePgData),
					},
				},
				Spec: corev1.PersistentVolumeClaimSpec{},
				Status: corev1.PersistentVolumeClaimStatus{
					Phase: corev1.ClaimBound,
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "orphanUnreadyPvc",
					Labels: map[string]string{
						utils.PvcRoleLabelName: string(utils.PVCRolePgData),
					},
				},
				Spec: corev1.PersistentVolumeClaimSpec{},
				Status: corev1.PersistentVolumeClaimStatus{
					Phase: corev1.ClaimBound,
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "readyPvc",
					Labels: map[string]string{
						utils.PvcRoleLabelName: string(utils.PVCRolePgData),
					},
					Annotations: map[string]string{
						PVCStatusAnnotationName: PVCStatusReady,
					},
				},
				Spec: corev1.PersistentVolumeClaimSpec{},
				Status: corev1.PersistentVolumeClaimStatus{
					Phase: corev1.ClaimBound,
				},
			},
		}
		pvcUsage := DetectPVCs(
			context.TODO(),
			&apiv1.Cluster{},
			[]corev1.Pod{
				{
					Spec: corev1.PodSpec{
						Volumes: []corev1.Volume{
							{
								VolumeSource: corev1.VolumeSource{
									PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
										ClaimName: "pvcForPod",
									},
								},
							},
						},
					},
				},
			},
			[]batchv1.Job{
				{
					Spec: batchv1.JobSpec{
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Volumes: []corev1.Volume{
									{
										VolumeSource: corev1.VolumeSource{
											PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
												ClaimName: "pvcForJob",
											},
										},
									},
								},
							},
						},
					},
				},
			},
			pvcs,
		)
		Expect(pvcUsage.InstanceNames).ShouldNot(BeEmpty())
		Expect(pvcUsage.InstanceNames).Should(HaveLen(3))
		Expect(pvcUsage.InstanceNames).Should(ContainElements("pvcForPod", "pvcForJob", "readyPvc"))
	})
})
