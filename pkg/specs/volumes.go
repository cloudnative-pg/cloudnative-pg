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
	"path"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
)

// PgWalVolumePath is the path used by the WAL volume when present
const PgWalVolumePath = "/var/lib/postgresql/wal"

// PgWalVolumePgWalPath is the path of pg_wal directory inside the WAL volume when present
const PgWalVolumePgWalPath = "/var/lib/postgresql/wal/pg_wal"

// PgTablespaceVolumePath is the base path used by tablespace when present
const PgTablespaceVolumePath = "/var/lib/postgresql/tablespaces"

// MountForTablespace returns the normalized tablespace volume name for a given
// tablespace, on a cluster pod
func MountForTablespace(tablespaceName string) string {
	return path.Join(PgTablespaceVolumePath, tablespaceName)
}

// LocationForTablespace returns the data location for tablespace on a cluster pod
func LocationForTablespace(tablespaceName string) string {
	return path.Join(MountForTablespace(tablespaceName), "data")
}

// convertPostgresIDToK8sName returns a postgres identifier without the characters
// that are illegal in K8s names and domains. (Lowercase RFC 1123)
func convertPostgresIDToK8sName(tablespaceName string) string {
	name := convertPostgresIDToK8s(tablespaceName)
	name = strings.ReplaceAll(name, "_", "-") // reversible
	name = strings.ToLower(name)              // irreversible
	return name
}

// convertPostgresIDToK8s transforms a postgres identifier to be a valid K8s
// label.
//
// NOTE: this is a reversible transformation, as we swap invalid K8s chars into invalid PG chars
func convertPostgresIDToK8s(tablespaceName string) string {
	// Postgres identifiers can begin with _ or a letter, K8's must begin
	// with an alphanumeric. We convert _ to 1 in this edge case
	if strings.HasPrefix(tablespaceName, "_") {
		tablespaceName = strings.Replace(tablespaceName, "_", "1", 1)
	}
	name := strings.ReplaceAll(tablespaceName, "$", "-")
	return name
}

// PvcNameForTablespace returns the normalized tablespace volume name for a given
// tablespace, on a cluster pod
func PvcNameForTablespace(podName, tablespaceName string) string {
	return podName + apiv1.TablespaceVolumeInfix + convertPostgresIDToK8sName(tablespaceName)
}

// VolumeMountNameForTablespace returns the normalized tablespace volume name for a given
// tablespace, on a cluster pod
func VolumeMountNameForTablespace(tablespaceName string) string {
	return convertPostgresIDToK8sName(tablespaceName)
}

// SnapshotBackupNameForTablespace returns the volume snapshot backup name for the tablespace
func SnapshotBackupNameForTablespace(backupName, tablespaceName string) string {
	return backupName + apiv1.TablespaceVolumeInfix + convertPostgresIDToK8sName(tablespaceName)
}

func createPostgresVolumes(cluster apiv1.Cluster, podName string) []corev1.Volume {
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
					SizeLimit: cluster.Spec.EphemeralVolumesSizeLimit.GetTemporaryDataLimit(),
				},
			},
		},
		{
			Name: "shm",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{
					Medium:    "Memory",
					SizeLimit: cluster.Spec.EphemeralVolumesSizeLimit.GetShmLimit(),
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

	// we should create volumeMounts in fixed sequence as podSpec will store it in annotation and
	// later it will be  retrieved to do deepEquals
	if cluster.ContainsTablespaces() {
		// Try to get a fix order of name
		tbsNames := getSortedTablespaceList(cluster)
		for i := range tbsNames {
			result = append(result,
				corev1.Volume{
					Name: VolumeMountNameForTablespace(tbsNames[i]),
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: PvcNameForTablespace(podName, tbsNames[i]),
						},
					},
				},
			)
		}
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

	// we should create volumeMounts in fixed sequence as podSpec will store it in annotation and
	// later it will be  retrieved to do deepEquals
	if cluster.ContainsTablespaces() {
		tbsNames := getSortedTablespaceList(cluster)
		for i := range tbsNames {
			volumeMounts = append(volumeMounts,
				corev1.VolumeMount{
					Name:      VolumeMountNameForTablespace(tbsNames[i]),
					MountPath: MountForTablespace(tbsNames[i]),
				},
			)
		}
	}
	return volumeMounts
}

func getSortedTablespaceList(cluster apiv1.Cluster) []string {
	// Try to get a fix order of name
	tbsNames := make([]string, len(cluster.Spec.Tablespaces))
	i := 0
	for name := range cluster.Spec.Tablespaces {
		tbsNames[i] = name
		i++
	}
	sort.Strings(tbsNames)
	return tbsNames
}

func createProjectedVolume(cluster apiv1.Cluster) corev1.Volume {
	return corev1.Volume{
		Name: "projected",
		VolumeSource: corev1.VolumeSource{
			Projected: cluster.Spec.ProjectedVolumeTemplate.DeepCopy(),
		},
	}
}
