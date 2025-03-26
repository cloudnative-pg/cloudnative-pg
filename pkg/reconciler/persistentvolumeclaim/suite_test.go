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
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestSpecs(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "PersistentVolumeClaim reconciler")
}

func makePVC(
	clusterName string,
	suffix string,
	serial string,
	meta Meta,
	isResizing bool,
) corev1.PersistentVolumeClaim {
	annotations := map[string]string{
		utils.ClusterSerialAnnotationName: serial,
		utils.PVCStatusAnnotationName:     StatusReady,
	}

	var conditions []corev1.PersistentVolumeClaimCondition
	if isResizing {
		conditions = append(conditions, corev1.PersistentVolumeClaimCondition{
			Type:   corev1.PersistentVolumeClaimResizing,
			Status: corev1.ConditionTrue,
		})
	}

	var labels map[string]string
	if meta != nil {
		labels = meta.GetLabels(clusterName + "-" + suffix)
	}

	return corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:        clusterName + "-" + suffix,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: corev1.PersistentVolumeClaimSpec{},
		Status: corev1.PersistentVolumeClaimStatus{
			Phase:      corev1.ClaimBound,
			Conditions: conditions,
		},
	}
}

func makePod(clusterName, serial, role string) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterName + "-" + serial,
			Annotations: map[string]string{
				utils.ClusterSerialAnnotationName: serial,
			},
			Labels: map[string]string{
				utils.ClusterInstanceRoleLabelName: role,
			},
		},
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
