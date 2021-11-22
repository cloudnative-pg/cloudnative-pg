/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package specs

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
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
		addBarmanEndpointCA(cluster, &job)
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
		addBarmanEndpointCA(cluster, &job)
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
					},
				},
			},
		}
		job := CreatePrimaryJobViaInitdb(cluster, 0)
		Expect(job.Spec.Template.Spec.Containers[0].Command).Should(ContainElement("testPostInitSql"))
		Expect(job.Spec.Template.Spec.Containers[0].Command).Should(ContainElement("testPostInitTemplateSql"))
		Expect(job.Spec.Template.Spec.Containers[0].Command).Should(ContainElement("testPostInitApplicationSql"))
	})
})
