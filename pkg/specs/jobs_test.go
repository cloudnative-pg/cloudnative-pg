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

package specs

import (
	"slices"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

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

		job := batchv1.Job{
			Spec: batchv1.JobSpec{
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

		job := batchv1.Job{
			Spec: batchv1.JobSpec{
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

		job := batchv1.Job{
			Spec: batchv1.JobSpec{
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
						PostInitApplicationSQLRefs: &apiv1.SQLRefs{
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
		Expect(job.Spec.Template.Spec.Containers[0].Command).Should(ContainElement("testPostInitSql"))
		Expect(job.Spec.Template.Spec.Containers[0].Command).Should(ContainElement("testPostInitTemplateSql"))
		Expect(job.Spec.Template.Spec.Containers[0].Command).Should(ContainElement("testPostInitApplicationSql"))
		Expect(job.Spec.Template.Spec.Containers[0].Command).Should(ContainElement(
			postInitApplicationSQLRefsFolder.toString()))
	})

	It("contains icu configuration", func() {
		cluster := apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					InitDB: &apiv1.BootstrapInitDB{
						Encoding:       "UTF-8",
						LocaleProvider: "icu",
						IcuLocale:      "und",
						IcuRules:       "&A < z <<< Z",
					},
				},
			},
		}
		job := CreatePrimaryJobViaInitdb(cluster, 0)

		jobCommand := job.Spec.Template.Spec.Containers[0].Command
		Expect(jobCommand).Should(ContainElement("--initdb-flags"))
		initdbFlags := jobCommand[slices.Index(jobCommand, "--initdb-flags")+1]
		Expect(initdbFlags).Should(ContainSubstring("--encoding=UTF-8"))
		Expect(initdbFlags).Should(ContainSubstring("--locale-provider=icu"))
		Expect(initdbFlags).Should(ContainSubstring("--icu-locale=und"))
		Expect(initdbFlags).ShouldNot(ContainSubstring("--locale="))
		Expect(initdbFlags).Should(ContainSubstring("'--icu-rules=&A < z <<< Z'"))
	})

	It("contains correct labels", func() {
		cluster := apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "cluster",
			},
			Spec: apiv1.ClusterSpec{
				ImageName: "postgres:18.0",
				Bootstrap: &apiv1.BootstrapConfiguration{
					InitDB: &apiv1.BootstrapInitDB{
						PostInitSQL:            []string{"testPostInitSql"},
						PostInitTemplateSQL:    []string{"testPostInitTemplateSql"},
						PostInitApplicationSQL: []string{"testPostInitApplicationSql"},
						PostInitApplicationSQLRefs: &apiv1.SQLRefs{
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
		Expect(job.Labels).To(BeEquivalentTo(map[string]string{
			utils.ClusterLabelName:                cluster.Name,
			utils.JobRoleLabelName:                "initdb",
			utils.InstanceNameLabelName:           "cluster-0",
			utils.KubernetesAppLabelName:          utils.AppName,
			utils.KubernetesAppInstanceLabelName:  cluster.Name,
			utils.KubernetesAppVersionLabelName:   "18",
			utils.KubernetesAppComponentLabelName: utils.DatabaseComponentName,
			utils.KubernetesAppManagedByLabelName: utils.ManagerName,
		}))
		Expect(job.Spec.Template.Labels).To(BeEquivalentTo(map[string]string{
			utils.ClusterLabelName:                cluster.Name,
			utils.JobRoleLabelName:                "initdb",
			utils.InstanceNameLabelName:           "cluster-0",
			utils.KubernetesAppLabelName:          utils.AppName,
			utils.KubernetesAppInstanceLabelName:  cluster.Name,
			utils.KubernetesAppVersionLabelName:   "18",
			utils.KubernetesAppComponentLabelName: utils.DatabaseComponentName,
			utils.KubernetesAppManagedByLabelName: utils.ManagerName,
		}))
	})

	It("propagates HostUsers=true to job spec", func() {
		hostUsersTrue := true
		cluster := apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "cluster",
			},
			Spec: apiv1.ClusterSpec{
				ImageName: "postgres:18.0",
				HostUsers: &hostUsersTrue,
				Bootstrap: &apiv1.BootstrapConfiguration{
					InitDB: &apiv1.BootstrapInitDB{},
				},
			},
		}
		job := CreatePrimaryJobViaInitdb(cluster, 0)
		Expect(job.Spec.Template.Spec.HostUsers).To(Equal(&hostUsersTrue))
	})

	It("propagates HostUsers=false to job spec", func() {
		hostUsersFalse := false
		cluster := apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "cluster",
			},
			Spec: apiv1.ClusterSpec{
				ImageName: "postgres:18.0",
				HostUsers: &hostUsersFalse,
				Bootstrap: &apiv1.BootstrapConfiguration{
					InitDB: &apiv1.BootstrapInitDB{},
				},
			},
		}
		job := CreatePrimaryJobViaInitdb(cluster, 0)
		Expect(job.Spec.Template.Spec.HostUsers).To(Equal(&hostUsersFalse))
	})

	It("does not set HostUsers when unspecified", func() {
		cluster := apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "cluster",
			},
			Spec: apiv1.ClusterSpec{
				ImageName: "postgres:18.0",
				Bootstrap: &apiv1.BootstrapConfiguration{
					InitDB: &apiv1.BootstrapInitDB{},
				},
			},
		}
		job := CreatePrimaryJobViaInitdb(cluster, 0)
		Expect(job.Spec.Template.Spec.HostUsers).To(BeNil())
	})
})
