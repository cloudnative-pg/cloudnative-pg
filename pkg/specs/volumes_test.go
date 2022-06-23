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
	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("test createVolumesAndVolumeMountsForPostInitApplicationSQLRefs", func() {
	It("input is empty", func() {
		input := &apiv1.PostInitApplicationSQLRefs{}
		volumes, volumeMounts := createVolumesAndVolumeMountsForPostInitApplicationSQLRefs(input)
		Expect(volumes).To(BeEmpty())
		Expect(volumeMounts).To(BeEmpty())
	})

	It("we have reference to secrets only", func() {
		input := &apiv1.PostInitApplicationSQLRefs{
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
		volumes, volumeMounts := createVolumesAndVolumeMountsForPostInitApplicationSQLRefs(input)
		Expect(volumeMounts).To(Equal([]corev1.VolumeMount{
			{
				Name:      "0-post-init-application-sql",
				MountPath: "/etc/post-init-application-sql-refs/0.sql",
				SubPath:   "0.sql",
				ReadOnly:  true,
			},
			{
				Name:      "1-post-init-application-sql",
				MountPath: "/etc/post-init-application-sql-refs/1.sql",
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
		input := &apiv1.PostInitApplicationSQLRefs{
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
		volumes, volumeMounts := createVolumesAndVolumeMountsForPostInitApplicationSQLRefs(input)
		Expect(volumeMounts).To(Equal([]corev1.VolumeMount{
			{
				Name:      "0-post-init-application-sql",
				MountPath: "/etc/post-init-application-sql-refs/0.sql",
				SubPath:   "0.sql",
				ReadOnly:  true,
			},
			{
				Name:      "1-post-init-application-sql",
				MountPath: "/etc/post-init-application-sql-refs/1.sql",
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
		input := &apiv1.PostInitApplicationSQLRefs{
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
		volumes, volumeMounts := createVolumesAndVolumeMountsForPostInitApplicationSQLRefs(input)
		Expect(volumeMounts).To(Equal([]corev1.VolumeMount{
			{
				Name:      "0-post-init-application-sql",
				MountPath: "/etc/post-init-application-sql-refs/0.sql",
				SubPath:   "0.sql",
				ReadOnly:  true,
			},
			{
				Name:      "1-post-init-application-sql",
				MountPath: "/etc/post-init-application-sql-refs/1.sql",
				SubPath:   "1.sql",
				ReadOnly:  true,
			},
			{
				Name:      "2-post-init-application-sql",
				MountPath: "/etc/post-init-application-sql-refs/2.sql",
				SubPath:   "2.sql",
				ReadOnly:  true,
			},
			{
				Name:      "3-post-init-application-sql",
				MountPath: "/etc/post-init-application-sql-refs/3.sql",
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
