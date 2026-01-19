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
	"k8s.io/utils/ptr"

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
		job, err := CreatePrimaryJobViaInitdb(cluster, 0)
		Expect(err).ToNot(HaveOccurred())
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
		job, err := CreatePrimaryJobViaInitdb(cluster, 0)
		Expect(err).ToNot(HaveOccurred())

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
		job, err := CreatePrimaryJobViaInitdb(cluster, 0)
		Expect(err).ToNot(HaveOccurred())
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
})

var _ = Describe("CreatePrimaryJob via InitDB with JobPatch", func() {
	It("applies JSON patch from annotation to job container image", func() {
		cluster := apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "default",
				Annotations: map[string]string{
					utils.InitDBJobPatchAnnotationName: `[{"op": "add", "path": "/spec/template/spec/terminationGracePeriodSeconds", "value": 30}]`, // nolint: lll
				},
			},
			Spec: apiv1.ClusterSpec{
				ImageName: "original-image:old",
				Bootstrap: &apiv1.BootstrapConfiguration{
					InitDB: &apiv1.BootstrapInitDB{},
				},
			},
		}

		job, err := CreatePrimaryJobViaInitdb(cluster, 0)
		Expect(err).ToNot(HaveOccurred())
		Expect(job).ToNot(BeNil())
		Expect(job.Spec.Template.Spec.TerminationGracePeriodSeconds).To(Equal(ptr.To[int64](30)))
	})

	It("applies JSON patch from annotation to job name", func() {
		cluster := apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "default",
				Annotations: map[string]string{
					utils.InitDBJobPatchAnnotationName: `[{"op": "replace", "path": "/metadata/name", "value": "custom-job-name"}]`, // nolint: lll
				},
			},
			Spec: apiv1.ClusterSpec{
				ImageName: "postgres:18.0",
				Bootstrap: &apiv1.BootstrapConfiguration{
					InitDB: &apiv1.BootstrapInitDB{},
				},
			},
		}

		job, err := CreatePrimaryJobViaInitdb(cluster, 0)
		Expect(err).ToNot(HaveOccurred())
		Expect(job).ToNot(BeNil())
		Expect(job.Name).To(Equal("custom-job-name"))
	})

	It("returns error if JSON patch is invalid", func() {
		cluster := apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "default",
				Annotations: map[string]string{
					utils.InitDBJobPatchAnnotationName: `invalid-json-patch`,
				},
			},
			Spec: apiv1.ClusterSpec{
				ImageName: "postgres:18.0",
				Bootstrap: &apiv1.BootstrapConfiguration{
					InitDB: &apiv1.BootstrapInitDB{},
				},
			},
		}

		_, err := CreatePrimaryJobViaInitdb(cluster, 0)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("while decoding JSON patch from annotation"))
	})

	It("returns error if JSON patch path is invalid", func() {
		cluster := apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "default",
				Annotations: map[string]string{
					utils.InitDBJobPatchAnnotationName: `[{"op": "replace", "path": "/nonexistent/path", "value": "test"}]`,
				},
			},
			Spec: apiv1.ClusterSpec{
				ImageName: "postgres:18.0",
				Bootstrap: &apiv1.BootstrapConfiguration{
					InitDB: &apiv1.BootstrapInitDB{},
				},
			},
		}

		_, err := CreatePrimaryJobViaInitdb(cluster, 0)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("while applying JSON patch from annotation"))
	})

	It("does not apply patch from wrong annotation type", func() {
		// Using joinJobPatch annotation should not affect initdb job
		cluster := apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "default",
				Annotations: map[string]string{
					utils.JoinJobPatchAnnotationName: `[{"op": "add", "path": "/spec/template/spec/terminationGracePeriodSeconds", "value": 99}]`, // nolint: lll
				},
			},
			Spec: apiv1.ClusterSpec{
				ImageName: "postgres:18.0",
				Bootstrap: &apiv1.BootstrapConfiguration{
					InitDB: &apiv1.BootstrapInitDB{},
				},
			},
		}

		job, err := CreatePrimaryJobViaInitdb(cluster, 0)
		Expect(err).ToNot(HaveOccurred())
		Expect(job).ToNot(BeNil())
		// initdb job should NOT have the patch applied (it's for join jobs)
		Expect(job.Spec.Template.Spec.TerminationGracePeriodSeconds).To(BeNil())
	})
})

var _ = Describe("CreatePrimaryJob via Join with JobPatch", func() {
	It("applies JSON patch from join annotation to join job", func() {
		cluster := apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "default",
				Annotations: map[string]string{
					utils.JoinJobPatchAnnotationName: `[{"op": "add", "path": "/spec/template/spec/terminationGracePeriodSeconds", "value": 45}]`, // nolint: lll
				},
			},
			Spec: apiv1.ClusterSpec{
				ImageName: "postgres:18.0",
				Bootstrap: &apiv1.BootstrapConfiguration{
					InitDB: &apiv1.BootstrapInitDB{},
				},
			},
		}

		job, err := JoinReplicaInstance(cluster, 0)
		Expect(err).ToNot(HaveOccurred())
		Expect(job).ToNot(BeNil())
		Expect(job.Spec.Template.Spec.TerminationGracePeriodSeconds).To(Equal(ptr.To[int64](45)))
	})

	It("does not apply patch from initdb annotation to join job", func() {
		cluster := apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "default",
				Annotations: map[string]string{
					utils.InitDBJobPatchAnnotationName: `[{"op": "add", "path": "/spec/template/spec/terminationGracePeriodSeconds", "value": 77}]`, // nolint: lll
				},
			},
			Spec: apiv1.ClusterSpec{
				ImageName: "postgres:18.0",
				Bootstrap: &apiv1.BootstrapConfiguration{
					InitDB: &apiv1.BootstrapInitDB{},
				},
			},
		}

		job, err := JoinReplicaInstance(cluster, 0)
		Expect(err).ToNot(HaveOccurred())
		Expect(job).ToNot(BeNil())
		Expect(job.Spec.Template.Spec.TerminationGracePeriodSeconds).To(BeNil())
	})
})

var _ = Describe("CreatePrimaryJob via Recovery with JobPatch", func() {
	It("applies JSON patch from fullRecovery annotation", func() {
		cluster := apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "default",
				Annotations: map[string]string{
					utils.FullRecoveryJobPatchAnnotationName: `[{"op": "add", "path": "/spec/template/spec/terminationGracePeriodSeconds", "value": 50}]`, // nolint: lll
				},
			},
			Spec: apiv1.ClusterSpec{
				ImageName: "postgres:18.0",
				Bootstrap: &apiv1.BootstrapConfiguration{
					Recovery: &apiv1.BootstrapRecovery{},
				},
			},
		}

		job, err := CreatePrimaryJobViaRecovery(cluster, 0, nil)
		Expect(err).ToNot(HaveOccurred())
		Expect(job).ToNot(BeNil())
		Expect(job.Spec.Template.Spec.TerminationGracePeriodSeconds).To(Equal(ptr.To[int64](50)))
	})
})

var _ = Describe("CreatePrimaryJob via PgBaseBackup with JobPatch", func() {
	It("applies JSON patch from pgbasebackup annotation", func() {
		cluster := apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "default",
				Annotations: map[string]string{
					utils.PGBaseBackupJobPatchAnnotationName: `[{"op": "add", "path": "/spec/template/spec/terminationGracePeriodSeconds", "value": 55}]`, // nolint: lll
				},
			},
			Spec: apiv1.ClusterSpec{
				ImageName: "postgres:18.0",
				Bootstrap: &apiv1.BootstrapConfiguration{
					PgBaseBackup: &apiv1.BootstrapPgBaseBackup{},
				},
			},
		}

		job, err := CreatePrimaryJobViaPgBaseBackup(cluster, 0)
		Expect(err).ToNot(HaveOccurred())
		Expect(job).ToNot(BeNil())
		Expect(job.Spec.Template.Spec.TerminationGracePeriodSeconds).To(Equal(ptr.To[int64](55)))
	})
})

var _ = Describe("CreatePrimaryJob via Snapshot Recovery with JobPatch", func() {
	It("applies JSON patch from snapshotRecovery annotation", func() {
		cluster := apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "default",
				Annotations: map[string]string{
					utils.SnapshotRecoveryJobPatchAnnotationName: `[{"op": "add", "path": "/spec/template/spec/terminationGracePeriodSeconds", "value": 65}]`, // nolint: lll
				},
			},
			Spec: apiv1.ClusterSpec{
				ImageName: "postgres:18.0",
				Bootstrap: &apiv1.BootstrapConfiguration{
					InitDB: &apiv1.BootstrapInitDB{},
				},
			},
		}

		job, err := RestoreReplicaInstance(cluster, 0)
		Expect(err).ToNot(HaveOccurred())
		Expect(job).ToNot(BeNil())
		Expect(job.Spec.Template.Spec.TerminationGracePeriodSeconds).To(Equal(ptr.To[int64](65)))
	})
})

var _ = Describe("GetJobPatchAnnotationForRole", func() {
	It("returns correct annotation name for each job role", func() {
		Expect(utils.GetJobPatchAnnotationForRole("initdb")).To(Equal(utils.InitDBJobPatchAnnotationName))
		Expect(utils.GetJobPatchAnnotationForRole("import")).To(Equal(utils.ImportJobPatchAnnotationName))
		Expect(utils.GetJobPatchAnnotationForRole("pgbasebackup")).To(Equal(utils.PGBaseBackupJobPatchAnnotationName))
		Expect(utils.GetJobPatchAnnotationForRole("full-recovery")).To(Equal(utils.FullRecoveryJobPatchAnnotationName))
		Expect(utils.GetJobPatchAnnotationForRole("join")).To(Equal(utils.JoinJobPatchAnnotationName))
		Expect(utils.GetJobPatchAnnotationForRole("snapshot-recovery")).To(Equal(utils.SnapshotRecoveryJobPatchAnnotationName))
		Expect(utils.GetJobPatchAnnotationForRole("major-upgrade")).To(Equal(utils.MajorUpgradeJobPatchAnnotationName))
	})

	It("returns empty string for unknown role", func() {
		Expect(utils.GetJobPatchAnnotationForRole("unknown")).To(Equal(""))
		Expect(utils.GetJobPatchAnnotationForRole("")).To(Equal(""))
	})
})

var _ = Describe("KnownJobPatchAnnotations", func() {
	It("returns all known job patch annotations", func() {
		annotations := utils.KnownJobPatchAnnotations()
		Expect(annotations).To(ConsistOf(
			utils.InitDBJobPatchAnnotationName,
			utils.ImportJobPatchAnnotationName,
			utils.PGBaseBackupJobPatchAnnotationName,
			utils.FullRecoveryJobPatchAnnotationName,
			utils.JoinJobPatchAnnotationName,
			utils.SnapshotRecoveryJobPatchAnnotationName,
			utils.MajorUpgradeJobPatchAnnotationName,
		))
		Expect(len(annotations)).To(Equal(7))
	})
})
