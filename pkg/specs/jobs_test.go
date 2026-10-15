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

	"github.com/kballard/go-shellquote"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

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
		cmd := BuildPrimaryBootstrapCommandViaInitdb(cluster)
		Expect(cmd.Command).Should(ContainElement("testPostInitSql"))
		Expect(cmd.Command).Should(ContainElement("testPostInitTemplateSql"))
		Expect(cmd.Command).Should(ContainElement("testPostInitApplicationSql"))
		Expect(cmd.Command).Should(ContainElement(
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
		cmd := BuildPrimaryBootstrapCommandViaInitdb(cluster)

		jobCommand := cmd.Command
		Expect(jobCommand).Should(ContainElement("--initdb-flags"))
		initdbFlags := jobCommand[slices.Index(jobCommand, "--initdb-flags")+1]
		Expect(initdbFlags).Should(ContainSubstring("--encoding=UTF-8"))
		Expect(initdbFlags).Should(ContainSubstring("--locale-provider=icu"))
		Expect(initdbFlags).Should(ContainSubstring("--icu-locale=und"))
		Expect(initdbFlags).ShouldNot(ContainSubstring("--locale="))
		Expect(initdbFlags).Should(ContainSubstring("'--icu-rules=&A < z <<< Z'"))
	})

	DescribeTable("leaves the initdb data checksums default alone when dataChecksums is unset",
		func(imageName string) {
			cluster := apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					ImageName: imageName,
					Bootstrap: &apiv1.BootstrapConfiguration{
						InitDB: &apiv1.BootstrapInitDB{},
					},
				},
			}
			cmd := BuildPrimaryBootstrapCommandViaInitdb(cluster)
			jobCommand := cmd.Command
			initdbFlags := jobCommand[slices.Index(jobCommand, "--initdb-flags")+1]
			Expect(initdbFlags).ShouldNot(ContainSubstring("--data-checksums"))
		},
		Entry("on PostgreSQL 17", "postgres:17.0"),
		Entry("on PostgreSQL 18", "postgres:18.0"),
	)

	It("does not set --data-checksums or --no-data-checksums when dataChecksums is true on PostgreSQL 18+", func() {
		cluster := apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				ImageName: "postgres:18.0",
				Bootstrap: &apiv1.BootstrapConfiguration{
					InitDB: &apiv1.BootstrapInitDB{
						DataChecksums: ptr.To(true),
					},
				},
			},
		}
		cmd := BuildPrimaryBootstrapCommandViaInitdb(cluster)
		jobCommand := cmd.Command
		initdbFlags := jobCommand[slices.Index(jobCommand, "--initdb-flags")+1]
		Expect(initdbFlags).ShouldNot(ContainSubstring("--data-checksums"))
	})

	It("sets --no-data-checksums when dataChecksums is false on PostgreSQL 18+", func() {
		cluster := apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				ImageName: "postgres:18.0",
				Bootstrap: &apiv1.BootstrapConfiguration{
					InitDB: &apiv1.BootstrapInitDB{
						DataChecksums: ptr.To(false),
					},
				},
			},
		}
		cmd := BuildPrimaryBootstrapCommandViaInitdb(cluster)
		jobCommand := cmd.Command
		initdbFlags := jobCommand[slices.Index(jobCommand, "--initdb-flags")+1]
		Expect(initdbFlags).Should(ContainSubstring("--no-data-checksums"))
	})

	It("sets --data-checksums when dataChecksums is true on PostgreSQL versions before 18", func() {
		cluster := apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				ImageName: "postgres:17.0",
				Bootstrap: &apiv1.BootstrapConfiguration{
					InitDB: &apiv1.BootstrapInitDB{
						DataChecksums: ptr.To(true),
					},
				},
			},
		}
		cmd := BuildPrimaryBootstrapCommandViaInitdb(cluster)
		jobCommand := cmd.Command
		initdbFlags := jobCommand[slices.Index(jobCommand, "--initdb-flags")+1]
		Expect(initdbFlags).Should(ContainSubstring("--data-checksums"))
		Expect(initdbFlags).ShouldNot(ContainSubstring("--no-data-checksums"))
	})

	It("does not set --data-checksums or --no-data-checksums when dataChecksums is false on PostgreSQL versions before 18",
		func() {
			cluster := apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					ImageName: "postgres:17.0",
					Bootstrap: &apiv1.BootstrapConfiguration{
						InitDB: &apiv1.BootstrapInitDB{
							DataChecksums: ptr.To(false),
						},
					},
				},
			}
			cmd := BuildPrimaryBootstrapCommandViaInitdb(cluster)
			jobCommand := cmd.Command
			initdbFlags := jobCommand[slices.Index(jobCommand, "--initdb-flags")+1]
			Expect(initdbFlags).ShouldNot(ContainSubstring("--no-data-checksums"))
			Expect(initdbFlags).ShouldNot(ContainSubstring("--data-checksums"))
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
		cmd := BuildPrimaryBootstrapCommandViaInitdb(cluster)
		job := CreatePrimaryJob(cluster, 0, cmd.Role, cmd.Command, getExtensions(&cluster))
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

var _ = Describe("Job created via pg_basebackup", func() {
	It("passes the additional arguments to the pgbasebackup command", func() {
		args := []string{"--max-rate=50M", "--label", "my 'quoted' label"}
		cluster := apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					PgBaseBackup: &apiv1.BootstrapPgBaseBackup{
						Source:         "source",
						AdditionalArgs: args,
					},
				},
			},
		}
		jobCommand := BuildPrimaryBootstrapCommandViaPgBaseBackup(cluster).Command
		Expect(jobCommand).Should(ContainElement("--pgbasebackup-additional-args"))
		parsedArgs, err := shellquote.Split(
			jobCommand[slices.Index(jobCommand, "--pgbasebackup-additional-args")+1])
		Expect(err).ToNot(HaveOccurred())
		Expect(parsedArgs).To(Equal(args))
	})

	It("passes the tablespace mappings before the additional arguments", func() {
		cluster := apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					PgBaseBackup: &apiv1.BootstrapPgBaseBackup{
						Source: "origin",
						TablespaceMapping: []apiv1.TablespaceMappingEntry{
							{Name: "fast", SourceLocation: "/data/pg/fast"},
							{Name: "archive", SourceLocation: "/data/pg/my archive"},
						},
						AdditionalArgs: []string{"--max-rate=50M"},
					},
				},
			},
		}

		command := BuildPrimaryBootstrapCommandViaPgBaseBackup(cluster).Command
		Expect(command).To(ContainElement("--pgbasebackup-additional-args"))
		parsedArgs, err := shellquote.Split(
			command[slices.Index(command, "--pgbasebackup-additional-args")+1])
		Expect(err).ToNot(HaveOccurred())
		Expect(parsedArgs).To(Equal([]string{
			"--tablespace-mapping=/data/pg/fast=/var/lib/postgresql/tablespaces/fast/data",
			"--tablespace-mapping=/data/pg/my archive=/var/lib/postgresql/tablespaces/archive/data",
			"--max-rate=50M",
		}))
	})

	It("doesn't pass the tablespace mapping when joining a replica", func() {
		cluster := apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					PgBaseBackup: &apiv1.BootstrapPgBaseBackup{
						Source: "origin",
						TablespaceMapping: []apiv1.TablespaceMappingEntry{
							{Name: "fast", SourceLocation: "/data/pg/fast"},
						},
					},
				},
			},
		}

		command := BuildReplicaBootstrapCommandViaJoin(cluster).Command
		for _, arg := range command {
			Expect(arg).ToNot(ContainSubstring("--tablespace-mapping"))
		}
	})
})

var _ = Describe("Job service account token", func() {
	const tokenMountPath = "/var/run/secrets/kubernetes.io/serviceaccount"

	cluster := apiv1.Cluster{
		Spec: apiv1.ClusterSpec{
			Bootstrap: &apiv1.BootstrapConfiguration{
				InitDB: &apiv1.BootstrapInitDB{},
			},
		},
	}

	It("always disables the automount and projects the token for non-bootstrap containers", func() {
		cmd := BuildPrimaryBootstrapCommandViaInitdb(cluster)
		job := CreatePrimaryJob(cluster, 0, cmd.Role, cmd.Command, getExtensions(&cluster))
		podSpec := job.Spec.Template.Spec
		Expect(podSpec.AutomountServiceAccountToken).To(HaveValue(BeFalse()))
		Expect(podSpec.Volumes).To(ContainElement(HaveField("Name", kubeAPIAccessVolumeName)))

		// The bootstrap init container only copies the manager binary and does
		// not need Kubernetes API access.
		Expect(podSpec.InitContainers).To(HaveLen(1))
		bootstrapContainer := podSpec.InitContainers[0]
		Expect(bootstrapContainer.Name).To(Equal(BootstrapControllerContainerName))
		Expect(bootstrapContainer.VolumeMounts).NotTo(ContainElement(corev1.VolumeMount{
			Name:      kubeAPIAccessVolumeName,
			MountPath: tokenMountPath,
			ReadOnly:  true,
		}), "bootstrap container should not mount the projected token")

		// All other containers need to reach the Kubernetes API.
		Expect(podSpec.Containers).ToNot(BeEmpty())
		for _, container := range podSpec.Containers {
			Expect(container.VolumeMounts).To(ContainElement(corev1.VolumeMount{
				Name:      kubeAPIAccessVolumeName,
				MountPath: tokenMountPath,
				ReadOnly:  true,
			}), "container %s should mount the projected token", container.Name)
		}
	})
})
