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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("test createVolumesAndVolumeMountsForSQLRefs", func() {
	It("input is empty", func() {
		input := &apiv1.SQLRefs{}
		volumes, volumeMounts := createVolumesAndVolumeMountsForSQLRefs(postInitApplicationSQLRefsFolder, input)
		Expect(volumes).To(BeEmpty())
		Expect(volumeMounts).To(BeEmpty())
	})

	It("we have reference to secrets only", func() {
		input := &apiv1.SQLRefs{
			SecretRefs: []apiv1.SecretKeySelector{
				{
					LocalObjectReference: apiv1.LocalObjectReference{
						Name: "secretName1",
					},
					Key: "secretKey1",
				},
				{
					LocalObjectReference: apiv1.LocalObjectReference{
						Name: "secretName2",
					},
					Key: "secretKey2",
				},
			},
		}
		volumes, volumeMounts := createVolumesAndVolumeMountsForSQLRefs(postInitApplicationSQLRefsFolder, input)
		Expect(volumeMounts).To(Equal([]corev1.VolumeMount{
			{
				Name:      "0-post-init-application-sql",
				MountPath: postInitApplicationSQLRefsFolder.toString() + "/0.sql",
				SubPath:   "0.sql",
				ReadOnly:  true,
			},
			{
				Name:      "1-post-init-application-sql",
				MountPath: postInitApplicationSQLRefsFolder.toString() + "/1.sql",
				SubPath:   "1.sql",
				ReadOnly:  true,
			},
		}))

		Expect(volumes).To(Equal([]corev1.Volume{
			{
				Name: "0-post-init-application-sql",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: "secretName1",
						Items: []corev1.KeyToPath{
							{
								Key:  "secretKey1",
								Path: "0.sql",
							},
						},
					},
				},
			},
			{
				Name: "1-post-init-application-sql",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: "secretName2",
						Items: []corev1.KeyToPath{
							{
								Key:  "secretKey2",
								Path: "1.sql",
							},
						},
					},
				},
			},
		}))
	})

	It("we have reference to configmaps only", func() {
		input := &apiv1.SQLRefs{
			ConfigMapRefs: []apiv1.ConfigMapKeySelector{
				{
					LocalObjectReference: apiv1.LocalObjectReference{
						Name: "configMapName1",
					},
					Key: "configMapKey1",
				},
				{
					LocalObjectReference: apiv1.LocalObjectReference{
						Name: "configMapName2",
					},
					Key: "configMapKey2",
				},
			},
		}
		volumes, volumeMounts := createVolumesAndVolumeMountsForSQLRefs(postInitApplicationSQLRefsFolder, input)
		Expect(volumeMounts).To(Equal([]corev1.VolumeMount{
			{
				Name:      "0-post-init-application-sql",
				MountPath: postInitApplicationSQLRefsFolder.toString() + "/0.sql",
				SubPath:   "0.sql",
				ReadOnly:  true,
			},
			{
				Name:      "1-post-init-application-sql",
				MountPath: postInitApplicationSQLRefsFolder.toString() + "/1.sql",
				SubPath:   "1.sql",
				ReadOnly:  true,
			},
		}))

		Expect(volumes).To(Equal([]corev1.Volume{
			{
				Name: "0-post-init-application-sql",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "configMapName1",
						},
						Items: []corev1.KeyToPath{
							{
								Key:  "configMapKey1",
								Path: "0.sql",
							},
						},
					},
				},
			},
			{
				Name: "1-post-init-application-sql",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "configMapName2",
						},
						Items: []corev1.KeyToPath{
							{
								Key:  "configMapKey2",
								Path: "1.sql",
							},
						},
					},
				},
			},
		}))
	})

	It("we have reference to both configmaps and secrets", func() {
		input := &apiv1.SQLRefs{
			SecretRefs: []apiv1.SecretKeySelector{
				{
					LocalObjectReference: apiv1.LocalObjectReference{
						Name: "secretName1",
					},
					Key: "secretKey1",
				},
				{
					LocalObjectReference: apiv1.LocalObjectReference{
						Name: "secretName2",
					},
					Key: "secretKey2",
				},
			},
			ConfigMapRefs: []apiv1.ConfigMapKeySelector{
				{
					LocalObjectReference: apiv1.LocalObjectReference{
						Name: "configMapName1",
					},
					Key: "configMapKey1",
				},
				{
					LocalObjectReference: apiv1.LocalObjectReference{
						Name: "configMapName2",
					},
					Key: "configMapKey2",
				},
			},
		}
		volumes, volumeMounts := createVolumesAndVolumeMountsForSQLRefs(postInitApplicationSQLRefsFolder, input)
		Expect(volumeMounts).To(Equal([]corev1.VolumeMount{
			{
				Name:      "0-post-init-application-sql",
				MountPath: postInitApplicationSQLRefsFolder.toString() + "/0.sql",
				SubPath:   "0.sql",
				ReadOnly:  true,
			},
			{
				Name:      "1-post-init-application-sql",
				MountPath: postInitApplicationSQLRefsFolder.toString() + "/1.sql",
				SubPath:   "1.sql",
				ReadOnly:  true,
			},
			{
				Name:      "2-post-init-application-sql",
				MountPath: postInitApplicationSQLRefsFolder.toString() + "/2.sql",
				SubPath:   "2.sql",
				ReadOnly:  true,
			},
			{
				Name:      "3-post-init-application-sql",
				MountPath: postInitApplicationSQLRefsFolder.toString() + "/3.sql",
				SubPath:   "3.sql",
				ReadOnly:  true,
			},
		}))

		Expect(volumes).To(Equal([]corev1.Volume{
			{
				Name: "0-post-init-application-sql",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: "secretName1",
						Items: []corev1.KeyToPath{
							{
								Key:  "secretKey1",
								Path: "0.sql",
							},
						},
					},
				},
			},
			{
				Name: "1-post-init-application-sql",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: "secretName2",
						Items: []corev1.KeyToPath{
							{
								Key:  "secretKey2",
								Path: "1.sql",
							},
						},
					},
				},
			},
			{
				Name: "2-post-init-application-sql",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "configMapName1",
						},
						Items: []corev1.KeyToPath{
							{
								Key:  "configMapKey1",
								Path: "2.sql",
							},
						},
					},
				},
			},
			{
				Name: "3-post-init-application-sql",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "configMapName2",
						},
						Items: []corev1.KeyToPath{
							{
								Key:  "configMapKey2",
								Path: "3.sql",
							},
						},
					},
				},
			},
		}))
	})
})

var _ = DescribeTable("test creation of volume mounts",
	func(cluster apiv1.Cluster, mounts []corev1.VolumeMount) {
		mts := CreatePostgresVolumeMounts(cluster, getExtensions(&cluster))
		Expect(mts).NotTo(BeEmpty())
		for _, mt := range mounts {
			Expect(mts).To(ContainElement(mt))
		}
	},
	Entry("creates pgdata mount for a plain cluster",
		apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Instances: 1,
			},
		},
		[]corev1.VolumeMount{
			{
				Name:             "pgdata",
				ReadOnly:         false,
				MountPath:        "/var/lib/postgresql/data",
				SubPath:          "",
				MountPropagation: nil,
				SubPathExpr:      "",
			},
		}),
	Entry("creates pgdata and pg-wal mounts for a cluster with walStorage configured",
		apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Instances: 1,
				WalStorage: &apiv1.StorageConfiguration{
					Size: "3Gi",
				},
			},
		},
		[]corev1.VolumeMount{
			{
				Name:             "pgdata",
				ReadOnly:         false,
				MountPath:        "/var/lib/postgresql/data",
				SubPath:          "",
				MountPropagation: nil,
				SubPathExpr:      "",
			},
			{
				Name:             "pg-wal",
				ReadOnly:         false,
				MountPath:        "/var/lib/postgresql/wal",
				SubPath:          "",
				MountPropagation: nil,
				SubPathExpr:      "",
			},
		}),
	Entry("creates a volume mount for each tablespace, with the expected mount point",
		apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Instances: 1,
				Tablespaces: []apiv1.TablespaceConfiguration{
					{
						Name: "fragglerock",
						Storage: apiv1.StorageConfiguration{
							Size: "3Gi",
						},
					},
					{
						Name: "futurama",
						Storage: apiv1.StorageConfiguration{
							Size: "2Gi",
						},
					},
				},
			},
		},
		[]corev1.VolumeMount{
			{
				Name:             "tbs-fragglerock",
				ReadOnly:         false,
				MountPath:        "/var/lib/postgresql/tablespaces/fragglerock",
				SubPath:          "",
				MountPropagation: nil,
				SubPathExpr:      "",
			},
			{
				Name:             "tbs-futurama",
				ReadOnly:         false,
				MountPath:        "/var/lib/postgresql/tablespaces/futurama",
				SubPath:          "",
				MountPropagation: nil,
				SubPathExpr:      "",
			},
		}),
)

var _ = DescribeTable("test creation of volumes",
	func(cluster apiv1.Cluster, volumes []corev1.Volume) {
		vols := createPostgresVolumes(&cluster, "pod-1", getExtensions(&cluster))
		Expect(vols).NotTo(BeEmpty())
		for _, v := range volumes {
			Expect(vols).To(ContainElement(v))
		}
	},
	Entry("should create a pgdata volume for a plain cluster",
		apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Instances: 1,
			},
		},
		[]corev1.Volume{
			{
				Name: "pgdata",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: "pod-1",
					},
				},
			},
		}),
	Entry("should create a pgdata and pgwal volumes for a cluster with walStorage",
		apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Instances: 1,
				WalStorage: &apiv1.StorageConfiguration{
					Size: "3Gi",
				},
			},
		},
		[]corev1.Volume{
			{
				Name: "pgdata",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: "pod-1",
					},
				},
			},
			{
				Name: "pg-wal",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: "pod-1" + apiv1.WalArchiveVolumeSuffix,
					},
				},
			},
		}),
	Entry("should create a volume for each tablespace",
		apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Instances: 1,
				Tablespaces: []apiv1.TablespaceConfiguration{
					{
						Name: "fragglerock",
						Storage: apiv1.StorageConfiguration{
							Size: "3Gi",
						},
					},
					{
						Name: "futurama",
						Storage: apiv1.StorageConfiguration{
							Size: "2Gi",
						},
					},
				},
			},
		},
		[]corev1.Volume{
			{
				Name: "tbs-fragglerock",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: "pod-1-tbs-fragglerock",
					},
				},
			},
			{
				Name: "tbs-futurama",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: "pod-1-tbs-futurama",
					},
				},
			},
		}),
)

var _ = Describe("createEphemeralVolume", func() {
	var cluster apiv1.Cluster

	BeforeEach(func() {
		cluster = apiv1.Cluster{}
	})

	It("should create an emptyDir volume by default", func() {
		ephemeralVolume := createEphemeralVolume(&cluster)
		Expect(ephemeralVolume.Name).To(Equal("scratch-data"))
		Expect(ephemeralVolume.VolumeSource.EmptyDir).NotTo(BeNil())
	})

	It("should create an ephemeral volume when specified in the cluster", func() {
		const storageClass = "test-storageclass"
		cluster.Spec.EphemeralVolumeSource = &corev1.EphemeralVolumeSource{
			VolumeClaimTemplate: &corev1.PersistentVolumeClaimTemplate{
				Spec: corev1.PersistentVolumeClaimSpec{
					StorageClassName: ptr.To(storageClass),
				},
			},
		}

		ephemeralVolume := createEphemeralVolume(&cluster)

		Expect(ephemeralVolume.Name).To(Equal("scratch-data"))
		Expect(ephemeralVolume.EmptyDir).To(BeNil())
		Expect(ephemeralVolume.VolumeSource.Ephemeral).NotTo(BeNil())
		Expect(*ephemeralVolume.VolumeSource.Ephemeral.VolumeClaimTemplate.Spec.StorageClassName).To(Equal(storageClass))
	})

	It("should set size limit when specified in the cluster", func() {
		quantity := resource.MustParse("1Gi")
		cluster.Spec.EphemeralVolumesSizeLimit = &apiv1.EphemeralVolumesSizeLimitConfiguration{
			TemporaryData: &quantity,
		}

		ephemeralVolume := createEphemeralVolume(&cluster)

		Expect(ephemeralVolume.Name).To(Equal("scratch-data"))
		Expect(*ephemeralVolume.VolumeSource.EmptyDir.SizeLimit).To(Equal(quantity))
	})
})

var _ = Describe("ImageVolume Extensions", func() {
	var cluster apiv1.Cluster

	BeforeEach(func() {
		extensionsConfig := []apiv1.ExtensionConfiguration{
			{
				Name: "foo",
				ImageVolumeSource: corev1.ImageVolumeSource{
					Reference: "foo:dev",
				},
			},
			{
				Name: "bar",
				ImageVolumeSource: corev1.ImageVolumeSource{
					Reference: "bar:dev",
				},
			},
		}

		cluster = apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cluster-example",
				Namespace: "default",
			},
			Spec: apiv1.ClusterSpec{
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Extensions: extensionsConfig,
				},
			},
			Status: apiv1.ClusterStatus{
				PGDataImageInfo: &apiv1.ImageInfo{
					Extensions: extensionsConfig,
				},
			},
		}
	})

	Context("createExtensionVolumes", func() {
		When("Extensions are disabled", func() {
			It("shouldn't create Volumes", func() {
				cluster.Spec.PostgresConfiguration.Extensions = []apiv1.ExtensionConfiguration{}
				cluster.Status.PGDataImageInfo.Extensions = []apiv1.ExtensionConfiguration{}
				extensionVolumes := createExtensionVolumes(getExtensions(&cluster))
				Expect(extensionVolumes).To(BeEmpty())
			})
		})
		When("Extensions are enabled", func() {
			It("should create a Volume for each Extension", func() {
				extensionVolumes := createExtensionVolumes(getExtensions(&cluster))
				Expect(len(extensionVolumes)).To(BeEquivalentTo(2))
				Expect(extensionVolumes[0].Name).To(Equal("ext-foo"))
				Expect(extensionVolumes[0].VolumeSource.Image.Reference).To(Equal("foo:dev"))
				Expect(extensionVolumes[1].Name).To(Equal("ext-bar"))
				Expect(extensionVolumes[1].VolumeSource.Image.Reference).To(Equal("bar:dev"))
			})
			It("should sanitize extension names with underscores for volume names", func() {
				extensionsConfig := []apiv1.ExtensionConfiguration{
					{
						Name: "pg_ivm",
						ImageVolumeSource: corev1.ImageVolumeSource{
							Reference: "pg_ivm:latest",
						},
					},
				}
				cluster.Spec.PostgresConfiguration.Extensions = extensionsConfig
				cluster.Status.PGDataImageInfo.Extensions = extensionsConfig

				extensionVolumes := createExtensionVolumes(getExtensions(&cluster))
				Expect(len(extensionVolumes)).To(BeEquivalentTo(1))
				Expect(extensionVolumes[0].Name).To(Equal("ext-pg-ivm"))
				Expect(extensionVolumes[0].VolumeSource.Image.Reference).To(Equal("pg_ivm:latest"))
			})
		})
	})

	Context("createExtensionVolumeMounts", func() {
		When("Extensions are disabled", func() {
			It("shouldn't create VolumeMounts", func() {
				cluster.Spec.PostgresConfiguration.Extensions = []apiv1.ExtensionConfiguration{}
				cluster.Status.PGDataImageInfo.Extensions = []apiv1.ExtensionConfiguration{}
				extensionVolumeMounts := createExtensionVolumeMounts(getExtensions(&cluster))
				Expect(extensionVolumeMounts).To(BeEmpty())
			})
		})
		When("Extensions are enabled", func() {
			It("should create a VolumeMount for each Extension", func() {
				const (
					fooMountPath = postgres.ExtensionsBaseDirectory + "/foo"
					barMountPath = postgres.ExtensionsBaseDirectory + "/bar"
				)
				extensionVolumeMounts := createExtensionVolumeMounts(getExtensions(&cluster))
				Expect(len(extensionVolumeMounts)).To(BeEquivalentTo(2))
				Expect(extensionVolumeMounts[0].Name).To(Equal("ext-foo"))
				Expect(extensionVolumeMounts[0].MountPath).To(Equal(fooMountPath))
				Expect(extensionVolumeMounts[1].Name).To(Equal("ext-bar"))
				Expect(extensionVolumeMounts[1].MountPath).To(Equal(barMountPath))
			})
			It("should sanitize extension names with underscores for volume mount names", func() {
				extensionsConfig := []apiv1.ExtensionConfiguration{
					{
						Name: "pg_ivm",
						ImageVolumeSource: corev1.ImageVolumeSource{
							Reference: "pg_ivm:latest",
						},
					},
				}
				cluster.Spec.PostgresConfiguration.Extensions = extensionsConfig
				cluster.Status.PGDataImageInfo.Extensions = extensionsConfig

				extensionVolumeMounts := createExtensionVolumeMounts(getExtensions(&cluster))
				Expect(len(extensionVolumeMounts)).To(BeEquivalentTo(1))
				Expect(extensionVolumeMounts[0].Name).To(Equal("ext-pg-ivm"))
				Expect(extensionVolumeMounts[0].MountPath).To(Equal(postgres.ExtensionsBaseDirectory + "/pg_ivm"))
			})
		})
	})

	Context("SanitizeExtensionNameForVolume", func() {
		It("should add ext- prefix and replace underscores with hyphens", func() {
			Expect(SanitizeExtensionNameForVolume("pg_ivm")).To(Equal("ext-pg-ivm"))
		})

		It("should handle multiple underscores", func() {
			Expect(SanitizeExtensionNameForVolume("my_custom_extension")).To(Equal("ext-my-custom-extension"))
		})

		It("should add ext- prefix to names without underscores", func() {
			Expect(SanitizeExtensionNameForVolume("foo")).To(Equal("ext-foo"))
			Expect(SanitizeExtensionNameForVolume("foo-bar")).To(Equal("ext-foo-bar"))
		})

		It("should handle mixed underscores and hyphens", func() {
			Expect(SanitizeExtensionNameForVolume("pg_foo-bar")).To(Equal("ext-pg-foo-bar"))
		})

		It("should handle consecutive underscores", func() {
			Expect(SanitizeExtensionNameForVolume("pg__stat")).To(Equal("ext-pg--stat"))
		})
	})
})
