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

	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PodSpecDiff", func() {
	It("returns true for superuser-secret volume", func() {
		Expect(shouldIgnoreCurrentVolume("superuser-secret")).To(BeTrue())
	})

	It("returns true for app-secret volume", func() {
		Expect(shouldIgnoreCurrentVolume("app-secret")).To(BeTrue())
	})

	It("returns false for other volumes", func() {
		Expect(shouldIgnoreCurrentVolume("other-volume")).To(BeFalse())
	})

	It("returns false for empty volume name", func() {
		Expect(shouldIgnoreCurrentVolume("")).To(BeFalse())
	})

	It("return false when the startup probe do not match and true otherwise", func() {
		containerPre := corev1.Container{
			StartupProbe: &corev1.Probe{
				TimeoutSeconds: 23,
			},
		}
		containerPost := corev1.Container{
			StartupProbe: &corev1.Probe{
				TimeoutSeconds: 24,
			},
		}
		Expect(doContainersMatch(containerPre, containerPre)).To(BeTrue())
		status, diff := doContainersMatch(containerPre, containerPost)
		Expect(status).To(BeFalse())
		Expect(diff).To(Equal("startup-probe"))
	})

	It("return false when the liveness probe do not match and true otherwise", func() {
		containerPre := corev1.Container{
			LivenessProbe: &corev1.Probe{
				InitialDelaySeconds: 23,
			},
		}
		containerPost := corev1.Container{
			LivenessProbe: &corev1.Probe{
				InitialDelaySeconds: 24,
			},
		}
		Expect(doContainersMatch(containerPre, containerPre)).To(BeTrue())
		status, diff := doContainersMatch(containerPre, containerPost)
		Expect(status).To(BeFalse())
		Expect(diff).To(Equal("liveness-probe"))
	})

	It("return false when the readiness probe do not match and true otherwise", func() {
		containerPre := corev1.Container{
			ReadinessProbe: &corev1.Probe{
				SuccessThreshold: 23,
			},
		}
		containerPost := corev1.Container{
			ReadinessProbe: &corev1.Probe{
				SuccessThreshold: 24,
			},
		}
		Expect(doContainersMatch(containerPre, containerPre)).To(BeTrue())
		status, diff := doContainersMatch(containerPre, containerPost)
		Expect(status).To(BeFalse())
		Expect(diff).To(Equal("readiness-probe"))
	})
})

var _ = Describe("normalizeVolumeName", func() {
	It("adds ext- prefix to image volumes without it", func() {
		vol := corev1.Volume{
			Name: "postgis",
			VolumeSource: corev1.VolumeSource{
				Image: &corev1.ImageVolumeSource{Reference: "postgis:latest"},
			},
		}
		Expect(normalizeVolumeName(vol)).To(Equal("ext-postgis"))
	})

	It("replaces underscores for image volumes without ext- prefix", func() {
		vol := corev1.Volume{
			Name: "pg_ivm",
			VolumeSource: corev1.VolumeSource{
				Image: &corev1.ImageVolumeSource{Reference: "pg_ivm:latest"},
			},
		}
		Expect(normalizeVolumeName(vol)).To(Equal("ext-pg-ivm"))
	})

	It("does not modify image volumes that already have ext- prefix", func() {
		vol := corev1.Volume{
			Name: "ext-postgis",
			VolumeSource: corev1.VolumeSource{
				Image: &corev1.ImageVolumeSource{Reference: "postgis:latest"},
			},
		}
		Expect(normalizeVolumeName(vol)).To(Equal("ext-postgis"))
	})

	It("adds tbs- prefix to tablespace PVC volumes without it", func() {
		vol := corev1.Volume{
			Name: "myts",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: "pod-1-tbs-myts",
				},
			},
		}
		Expect(normalizeVolumeName(vol)).To(Equal("tbs-myts"))
	})

	It("does not modify tablespace PVC volumes that already have tbs- prefix", func() {
		vol := corev1.Volume{
			Name: "tbs-myts",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: "pod-1-tbs-myts",
				},
			},
		}
		Expect(normalizeVolumeName(vol)).To(Equal("tbs-myts"))
	})

	It("does not modify system volumes", func() {
		vol := corev1.Volume{
			Name: "pgdata",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: "pod-1",
				},
			},
		}
		Expect(normalizeVolumeName(vol)).To(Equal("pgdata"))
	})
})

var _ = Describe("normalizeVolumeMountName", func() {
	It("adds ext- prefix to extension mounts without it", func() {
		mount := corev1.VolumeMount{
			Name:      "postgis",
			MountPath: postgres.ExtensionsBaseDirectory + "/postgis",
		}
		Expect(normalizeVolumeMountName(mount)).To(Equal("ext-postgis"))
	})

	It("replaces underscores for extension mounts without ext- prefix", func() {
		mount := corev1.VolumeMount{
			Name:      "pg_ivm",
			MountPath: postgres.ExtensionsBaseDirectory + "/pg_ivm",
		}
		Expect(normalizeVolumeMountName(mount)).To(Equal("ext-pg-ivm"))
	})

	It("does not modify extension mounts that already have ext- prefix", func() {
		mount := corev1.VolumeMount{
			Name:      "ext-postgis",
			MountPath: postgres.ExtensionsBaseDirectory + "/postgis",
		}
		Expect(normalizeVolumeMountName(mount)).To(Equal("ext-postgis"))
	})

	It("adds tbs- prefix to tablespace mounts without it", func() {
		mount := corev1.VolumeMount{
			Name:      "myts",
			MountPath: PgTablespaceVolumePath + "/myts",
		}
		Expect(normalizeVolumeMountName(mount)).To(Equal("tbs-myts"))
	})

	It("does not modify tablespace mounts that already have tbs- prefix", func() {
		mount := corev1.VolumeMount{
			Name:      "tbs-myts",
			MountPath: PgTablespaceVolumePath + "/myts",
		}
		Expect(normalizeVolumeMountName(mount)).To(Equal("tbs-myts"))
	})

	It("does not modify system mounts", func() {
		mount := corev1.VolumeMount{
			Name:      "pgdata",
			MountPath: "/var/lib/postgresql/data",
		}
		Expect(normalizeVolumeMountName(mount)).To(Equal("pgdata"))
	})
})

var _ = Describe("compareVolumes migration", func() {
	It("matches old unprefixed extension volume with new prefixed one", func() {
		current := []corev1.Volume{
			{
				Name: "postgis",
				VolumeSource: corev1.VolumeSource{
					Image: &corev1.ImageVolumeSource{Reference: "postgis:latest"},
				},
			},
		}
		target := []corev1.Volume{
			{
				Name: "ext-postgis",
				VolumeSource: corev1.VolumeSource{
					Image: &corev1.ImageVolumeSource{Reference: "postgis:latest"},
				},
			},
		}
		match, _ := compareVolumes(current, target)
		Expect(match).To(BeTrue())
	})

	It("matches old unprefixed tablespace volume with new prefixed one", func() {
		current := []corev1.Volume{
			{
				Name: "myts",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: "pod-1-tbs-myts",
					},
				},
			},
		}
		target := []corev1.Volume{
			{
				Name: "tbs-myts",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: "pod-1-tbs-myts",
					},
				},
			},
		}
		match, _ := compareVolumes(current, target)
		Expect(match).To(BeTrue())
	})
})

var _ = Describe("compareVolumeMounts migration", func() {
	It("matches old unprefixed extension mount with new prefixed one", func() {
		current := []corev1.VolumeMount{
			{
				Name:      "postgis",
				MountPath: postgres.ExtensionsBaseDirectory + "/postgis",
			},
		}
		target := []corev1.VolumeMount{
			{
				Name:      "ext-postgis",
				MountPath: postgres.ExtensionsBaseDirectory + "/postgis",
			},
		}
		match, _ := compareVolumeMounts(current, target)
		Expect(match).To(BeTrue())
	})

	It("matches old unprefixed tablespace mount with new prefixed one", func() {
		current := []corev1.VolumeMount{
			{
				Name:      "myts",
				MountPath: PgTablespaceVolumePath + "/myts",
			},
		}
		target := []corev1.VolumeMount{
			{
				Name:      "tbs-myts",
				MountPath: PgTablespaceVolumePath + "/myts",
			},
		}
		match, _ := compareVolumeMounts(current, target)
		Expect(match).To(BeTrue())
	})
})
