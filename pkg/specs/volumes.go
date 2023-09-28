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
	"fmt"

	corev1 "k8s.io/api/core/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
)

// PgWalVolumePath its the path used by the WAL volume when present
const PgWalVolumePath = "/var/lib/postgresql/wal"

// PgWalVolumePgWalPath its the path of pg_wal directory inside the WAL volume when present
const PgWalVolumePgWalPath = "/var/lib/postgresql/wal/pg_wal"

func createPostgresVolumes(cluster apiv1.Cluster, podName string) []corev1.Volume {
	ephemeralStorageVolumeLimit := cluster.GetEmptyDirLimit()

	result := []corev1.Volume{
		{
			Name: "pgdata",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: podName,
				},
			},
		},
		{
			Name: "scratch-data",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{
					SizeLimit: ephemeralStorageVolumeLimit,
				},
			},
		},
		{
			Name: "shm",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{
					Medium:    "Memory",
					SizeLimit: ephemeralStorageVolumeLimit,
				},
			},
		},
	}

	if cluster.GetEnableSuperuserAccess() {
		result = append(result,
			corev1.Volume{
				Name: "superuser-secret",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: cluster.GetSuperuserSecretName(),
					},
				},
			},
		)
	}

	if cluster.ShouldCreateApplicationDatabase() {
		result = append(result,
			corev1.Volume{
				Name: "app-secret",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: cluster.GetApplicationSecretName(),
					},
				},
			},
		)
	}

	if cluster.ShouldCreateWalArchiveVolume() {
		result = append(result,
			corev1.Volume{
				Name: "pg-wal",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: podName + apiv1.WalArchiveVolumeSuffix,
					},
				},
			})
	}

	if cluster.ShouldCreateProjectedVolume() {
		result = append(result, createProjectedVolume(cluster))
	}
	return result
}

func createVolumesAndVolumeMountsForPostInitApplicationSQLRefs(
	refs *apiv1.PostInitApplicationSQLRefs,
) ([]corev1.Volume, []corev1.VolumeMount) {
	length := len(refs.ConfigMapRefs) + len(refs.SecretRefs)
	digitsCount := len(fmt.Sprintf("%d", length))
	volumes := make([]corev1.Volume, 0, length)
	volumeMounts := make([]corev1.VolumeMount, 0, length)

	for i := range refs.SecretRefs {
		volumes = append(volumes, corev1.Volume{
			Name: fmt.Sprintf("%0*d-post-init-application-sql", digitsCount, i),
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: refs.SecretRefs[i].Name,
					Items: []corev1.KeyToPath{
						{
							Key:  refs.SecretRefs[i].Key,
							Path: fmt.Sprintf("%0*d.sql", digitsCount, i),
						},
					},
				},
			},
		})

		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      fmt.Sprintf("%0*d-post-init-application-sql", digitsCount, i),
			MountPath: fmt.Sprintf("%s/%0*d.sql", postInitApplicationSQLRefsFolder, digitsCount, i),
			SubPath:   fmt.Sprintf("%0*d.sql", digitsCount, i),
			ReadOnly:  true,
		})
	}

	for i := range refs.ConfigMapRefs {
		volumes = append(volumes, corev1.Volume{
			Name: fmt.Sprintf("%0*d-post-init-application-sql", digitsCount, i+len(refs.SecretRefs)),
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: refs.ConfigMapRefs[i].Name,
					},
					Items: []corev1.KeyToPath{
						{
							Key:  refs.ConfigMapRefs[i].Key,
							Path: fmt.Sprintf("%0*d.sql", digitsCount, i+len(refs.SecretRefs)),
						},
					},
				},
			},
		})

		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      fmt.Sprintf("%0*d-post-init-application-sql", digitsCount, i+len(refs.SecretRefs)),
			MountPath: fmt.Sprintf("%s/%0*d.sql", postInitApplicationSQLRefsFolder, digitsCount, i+len(refs.SecretRefs)),
			SubPath:   fmt.Sprintf("%0*d.sql", digitsCount, i+len(refs.SecretRefs)),
			ReadOnly:  true,
		})
	}

	return volumes, volumeMounts
}

func createPostgresVolumeMounts(cluster apiv1.Cluster) []corev1.VolumeMount {
	volumeMounts := []corev1.VolumeMount{
		{
			Name:      "pgdata",
			MountPath: "/var/lib/postgresql/data",
		},
		{
			Name:      "scratch-data",
			MountPath: "/run",
		},
		{
			Name:      "scratch-data",
			MountPath: postgres.ScratchDataDirectory,
		},
		{
			Name:      "shm",
			MountPath: "/dev/shm",
		},
	}

	if cluster.GetEnableSuperuserAccess() {
		volumeMounts = append(volumeMounts,
			corev1.VolumeMount{
				Name:      "superuser-secret",
				MountPath: "/etc/superuser-secret",
			},
		)
	}

	if cluster.ShouldCreateApplicationDatabase() {
		volumeMounts = append(volumeMounts,
			corev1.VolumeMount{
				Name:      "app-secret",
				MountPath: "/etc/app-secret",
			},
		)
	}

	if cluster.ShouldCreateWalArchiveVolume() {
		volumeMounts = append(volumeMounts,
			corev1.VolumeMount{
				Name:      "pg-wal",
				MountPath: PgWalVolumePath,
			},
		)
	}

	if cluster.ShouldCreateProjectedVolume() {
		volumeMounts = append(volumeMounts,
			corev1.VolumeMount{
				Name:      "projected",
				MountPath: postgres.ProjectedVolumeDirectory,
			},
		)
	}

	return volumeMounts
}

func createProjectedVolume(cluster apiv1.Cluster) corev1.Volume {
	return corev1.Volume{
		Name: "projected",
		VolumeSource: corev1.VolumeSource{
			Projected: cluster.Spec.ProjectedVolumeTemplate.DeepCopy(),
		},
	}
}
