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
	v1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Barman endpoint CA", func() {
	It("is not added to job specs if backup is not defined", func() {
		cluster := apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					Recovery: &apiv1.BootstrapRecovery{},
				},
			},
		}

		job := v1.Job{
			Spec: v1.JobSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{}},
						Volumes: []corev1.Volume{
							{
								VolumeSource: corev1.VolumeSource{
									Secret: &corev1.SecretVolumeSource{
										Items: []corev1.KeyToPath{
											{},
										},
									},
								},
							},
						},
					},
				},
			},
		}
		addBarmanEndpointCAToJobFromCluster(cluster, nil, &job)
		Expect(job.Spec.Template.Spec.Volumes[0].VolumeSource.Secret.Items[0].Key).To(BeEmpty())
	})

	It("is properly added to job specs", func() {
		cluster := apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					Recovery: &apiv1.BootstrapRecovery{
						Backup: &apiv1.BackupSource{
							LocalObjectReference: apiv1.LocalObjectReference{},
							EndpointCA: &apiv1.SecretKeySelector{
								LocalObjectReference: apiv1.LocalObjectReference{
									Name: "test_name_endpoint",
								},
								Key: "test_key_endpoint",
							},
						},
					},
				},
			},
		}

		job := v1.Job{
			Spec: v1.JobSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{},
						},
					},
				},
			},
		}
		addBarmanEndpointCAToJobFromCluster(cluster, nil, &job)
		Expect(job.Spec.Template.Spec.Volumes[0].VolumeSource.Secret.Items[0].Key).To(
			BeEquivalentTo("test_key_endpoint"))
	})

	It("is properly added to job specs when specified in the backup", func() {
		cluster := apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					Recovery: &apiv1.BootstrapRecovery{
						Backup: &apiv1.BackupSource{
							LocalObjectReference: apiv1.LocalObjectReference{Name: "test"},
						},
					},
				},
			},
		}

		backup := apiv1.Backup{ObjectMeta: metav1.ObjectMeta{Name: "test"}, Status: apiv1.BackupStatus{
			EndpointCA: &apiv1.SecretKeySelector{
				LocalObjectReference: apiv1.LocalObjectReference{
					Name: "test_name_endpoint",
				},
				Key: "test_key_endpoint",
			},
		}}

		job := v1.Job{
			Spec: v1.JobSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{},
						},
					},
				},
			},
		}
		addBarmanEndpointCAToJobFromCluster(cluster, &backup, &job)
		Expect(job.Spec.Template.Spec.Volumes[0].VolumeSource.Secret.Items[0].Key).To(
			BeEquivalentTo("test_key_endpoint"))
	})
})

var _ = Describe("Job created via InitDB", func() {
	It("contain cluster post-init SQL instructions", func() {
		cluster := apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					InitDB: &apiv1.BootstrapInitDB{
						PostInitSQL:            []string{"testPostInitSql"},
						PostInitTemplateSQL:    []string{"testPostInitTemplateSql"},
						PostInitApplicationSQL: []string{"testPostInitApplicationSql"},
						PostInitApplicationSQLRefs: &apiv1.PostInitApplicationSQLRefs{
							SecretRefs: []apiv1.SecretKeySelector{
								{
									Key: "secretKey1",
									LocalObjectReference: apiv1.LocalObjectReference{
										Name: "secretName1",
									},
								},
							},
						},
					},
				},
			},
		}
		job := CreatePrimaryJobViaInitdb(cluster, 0)
		Expect(job.Spec.Template.Spec.Containers[0].Args[1]).Should(ContainSubstring("testPostInitSql"))
		Expect(job.Spec.Template.Spec.Containers[0].Args[1]).Should(ContainSubstring("testPostInitTemplateSql"))
		Expect(job.Spec.Template.Spec.Containers[0].Args[1]).Should(ContainSubstring("testPostInitApplicationSql"))
		Expect(job.Spec.Template.Spec.Containers[0].Args[1]).Should(ContainSubstring(postInitApplicationSQLRefsFolder))
	})
})
