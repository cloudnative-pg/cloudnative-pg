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

func makePVC(clusterName, serial string, isReady bool) corev1.PersistentVolumeClaim {
	annotations := map[string]string{
		ClusterSerialAnnotationName: serial,
	}
	if isReady {
		annotations[PVCStatusAnnotationName] = PVCStatusReady
	}
	return corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterName + "-" + serial,
			Labels: map[string]string{
				utils.PvcRoleLabelName: string(utils.PVCRolePgData),
			},
			Annotations: annotations,
		},
		Spec: corev1.PersistentVolumeClaimSpec{},
		Status: corev1.PersistentVolumeClaimStatus{
			Phase: corev1.ClaimBound,
		},
	}
}

func makePod(clusterName, serial string) corev1.Pod {
	return corev1.Pod{
		Spec: corev1.PodSpec{
			Volumes: []corev1.Volume{
				{
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: clusterName + "-" + serial,
						},
					},
				},
			},
		},
	}
}

func makeJob(clusterName, serial string) batchv1.Job {
	return batchv1.Job{
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Volumes: []corev1.Volume{
						{
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: clusterName + "-" + serial,
								},
							},
						},
					},
				},
			},
		},
	}
}

var _ = Describe("PVC detection", func() {
	It("will list PVCs with Jobs or Pods or which are Ready", func() {
		clusterName := "myCluster"
		pvcs := []corev1.PersistentVolumeClaim{
			makePVC(clusterName, "1", true),
			makePVC(clusterName, "2", true),
			makePVC(clusterName, "3", false),
			makePVC(clusterName, "4", true),
		}
		pvcUsage := DetectPVCs(
			context.TODO(),
			&apiv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName,
				},
			},
			[]corev1.Pod{makePod(clusterName, "1")},
			[]batchv1.Job{makeJob(clusterName, "2")},
			pvcs,
		)
		Expect(pvcUsage.InstanceNames).ShouldNot(BeEmpty())
		Expect(pvcUsage.InstanceNames).Should(HaveLen(3))
		// the PVC clusterName+"-3" is not ready, and has no Job nor Pod
		Expect(pvcUsage.InstanceNames).Should(ConsistOf(clusterName+"-1", clusterName+"-2", clusterName+"-4"))
	})
})
