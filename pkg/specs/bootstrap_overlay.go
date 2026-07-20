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
	"fmt"

	"github.com/kballard/go-shellquote"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// The bootstrap mode names passed to "instance run" via --bootstrap-mode. They
// MUST match the Mode constants in pkg/management/postgres/bootstrap, which this
// package cannot import without an import cycle (bootstrap imports postgres,
// which imports specs).
const (
	bootstrapModeInitDB          = "initdb"
	bootstrapModeJoin            = "join"
	bootstrapModePgBaseBackup    = "pgbasebackup"
	bootstrapModeRestore         = "restore"
	bootstrapModeRestoreSnapshot = "restoresnapshot"
)

// buildInitDBArgs builds the "instance init" flags, i.e. everything the initdb
// bootstrap Job passed after the "instance init" subcommand.
func buildInitDBArgs(cluster apiv1.Cluster) []string {
	var args []string

	if cluster.Spec.Bootstrap != nil && cluster.Spec.Bootstrap.InitDB != nil {
		args = append(args, buildInitDBFlags(cluster)...)
	}

	if cluster.Spec.Bootstrap.InitDB.PostInitSQL != nil {
		args = append(args, "--post-init-sql", shellquote.Join(cluster.Spec.Bootstrap.InitDB.PostInitSQL...))
	}

	if cluster.Spec.Bootstrap.InitDB.PostInitApplicationSQL != nil {
		args = append(args,
			"--post-init-application-sql",
			shellquote.Join(cluster.Spec.Bootstrap.InitDB.PostInitApplicationSQL...))
	}

	if cluster.Spec.Bootstrap.InitDB.PostInitTemplateSQL != nil {
		args = append(args,
			"--post-init-template-sql",
			shellquote.Join(cluster.Spec.Bootstrap.InitDB.PostInitTemplateSQL...))
	}

	if cluster.ShouldInitDBCreateApplicationDatabase() {
		args = append(args,
			"--app-db-name", cluster.Spec.Bootstrap.InitDB.Database,
			"--app-user", cluster.Spec.Bootstrap.InitDB.Owner)
	}

	args = append(args, buildCommonInitJobFlags(cluster)...)

	// The import variant returns before the SQL-refs folder flags, matching the
	// original Job builder.
	if cluster.Spec.Bootstrap.InitDB.Import != nil {
		return args
	}

	if cluster.ShouldInitDBRunPostInitApplicationSQLRefs() {
		args = append(args, "--post-init-application-sql-refs-folder", postInitApplicationSQLRefsFolder.toString())
	}

	if cluster.ShouldInitDBRunPostInitTemplateSQLRefs() {
		args = append(args, "--post-init-template-sql-refs-folder", postInitTemplateQLRefsFolder.toString())
	}

	if cluster.ShouldInitDBRunPostInitSQLRefs() {
		args = append(args, "--post-init-sql-refs-folder", postInitSQLRefsFolder.toString())
	}

	return args
}

// buildJoinArgs builds the "instance join" flags.
func buildJoinArgs(cluster apiv1.Cluster) []string {
	return append(
		[]string{"--parent-node", cluster.GetServiceReadWriteName()},
		buildCommonInitJobFlags(cluster)...)
}

// buildPgBaseBackupArgs builds the "instance pgbasebackup" flags.
func buildPgBaseBackupArgs(cluster apiv1.Cluster) []string {
	return buildCommonInitJobFlags(cluster)
}

// buildRecoveryArgs builds the "instance restore" flags.
func buildRecoveryArgs(cluster apiv1.Cluster) []string {
	return buildCommonInitJobFlags(cluster)
}

// buildRestoreSnapshotArgs builds the "instance restoresnapshot" flags for a
// primary restored from a volume snapshot.
func buildRestoreSnapshotArgs(cluster apiv1.Cluster, object *metav1.ObjectMeta) []string {
	var args []string

	if object.Annotations[utils.BackupLabelFileAnnotationName] != "" {
		args = append(args, fmt.Sprintf("--backuplabel=%s", object.Annotations[utils.BackupLabelFileAnnotationName]))
	}

	if object.Annotations[utils.BackupTablespaceMapFileAnnotationName] != "" {
		args = append(args,
			fmt.Sprintf("--tablespacemap=%s", object.Annotations[utils.BackupTablespaceMapFileAnnotationName]))
	}

	return append(args, buildCommonInitJobFlags(cluster)...)
}

// buildRestoreSnapshotReplicaArgs builds the "instance restoresnapshot" flags
// for a replica seeded from a volume snapshot.
func buildRestoreSnapshotReplicaArgs(cluster apiv1.Cluster) []string {
	return append([]string{"--immediate"}, buildCommonInitJobFlags(cluster)...)
}

// BootstrapInstruction is the operator-resolved overlay applied to a
// steady-state instance pod so that it bootstraps its own data directory
// in-process, instead of relying on a dedicated Job. It mirrors, field for
// field, what the bootstrap Job builders used to produce.
type BootstrapInstruction struct {
	cluster         apiv1.Cluster
	mode            string
	args            []string
	addInitDBExtras bool
}

// NewInitDBInstruction builds the overlay for the initdb bootstrap.
func NewInitDBInstruction(cluster apiv1.Cluster) BootstrapInstruction {
	return BootstrapInstruction{
		cluster:         cluster,
		mode:            bootstrapModeInitDB,
		args:            buildInitDBArgs(cluster),
		addInitDBExtras: true,
	}
}

// NewJoinInstruction builds the overlay for a replica joining via pg_basebackup.
func NewJoinInstruction(cluster apiv1.Cluster) BootstrapInstruction {
	return BootstrapInstruction{
		cluster: cluster,
		mode:    bootstrapModeJoin,
		args:    buildJoinArgs(cluster),
	}
}

// NewPgBaseBackupInstruction builds the overlay for the pgbasebackup bootstrap.
func NewPgBaseBackupInstruction(cluster apiv1.Cluster) BootstrapInstruction {
	return BootstrapInstruction{
		cluster: cluster,
		mode:    bootstrapModePgBaseBackup,
		args:    buildPgBaseBackupArgs(cluster),
	}
}

// NewRecoveryInstruction builds the overlay for a recovery from a backup. The
// recovery endpoint CA is not mounted here: it is written to disk during the
// in-process bootstrap (phase 0), the same way the instance manager owns the CA
// files in steady state. The backup argument is kept for call-site symmetry with
// the operator, which resolves it while checking the recovery source.
func NewRecoveryInstruction(cluster apiv1.Cluster, _ *apiv1.Backup) BootstrapInstruction {
	return BootstrapInstruction{
		cluster: cluster,
		mode:    bootstrapModeRestore,
		args:    buildRecoveryArgs(cluster),
	}
}

// NewRestoreSnapshotInstruction builds the overlay for a primary restored from a
// volume snapshot. As for the recovery overlay, the barman endpoint CA is written
// during the in-process bootstrap rather than mounted here.
func NewRestoreSnapshotInstruction(
	cluster apiv1.Cluster,
	object *metav1.ObjectMeta,
	_ *apiv1.Backup,
) BootstrapInstruction {
	return BootstrapInstruction{
		cluster: cluster,
		mode:    bootstrapModeRestoreSnapshot,
		args:    buildRestoreSnapshotArgs(cluster, object),
	}
}

// NewRestoreSnapshotReplicaInstruction builds the overlay for a replica seeded
// from a volume snapshot.
func NewRestoreSnapshotReplicaInstruction(cluster apiv1.Cluster) BootstrapInstruction {
	return BootstrapInstruction{
		cluster: cluster,
		mode:    bootstrapModeRestoreSnapshot,
		args:    buildRestoreSnapshotReplicaArgs(cluster),
	}
}

// ApplyBootstrapOverlay turns a steady-state instance pod (as produced by
// NewInstance) into a bootstrapping pod. It appends the --bootstrap-mode
// instruction plus the mode-specific flags to the instance run command, adds the
// volumes, mounts and environment the bootstrap needs, and stamps the bootstrap
// annotation. It deliberately leaves the podSpec and podEnvHash annotations
// untouched so the pod is not seen as drifted: those still describe the
// steady-state spec, and the drift check compares against a regenerated
// steady-state spec.
func ApplyBootstrapOverlay(pod *corev1.Pod, instruction BootstrapInstruction) error {
	if len(pod.Spec.Containers) == 0 || pod.Spec.Containers[0].Name != PostgresContainerName {
		return fmt.Errorf("cannot apply the bootstrap overlay: the first container is not %q", PostgresContainerName)
	}

	container := &pod.Spec.Containers[0]
	container.Command = append(container.Command, "--bootstrap-mode="+instruction.mode)
	container.Command = append(container.Command, instruction.args...)

	if instruction.addInitDBExtras {
		applyInitDBOverlayExtras(pod, instruction.cluster)
	}

	if pod.Annotations == nil {
		pod.Annotations = map[string]string{}
	}
	pod.Annotations[utils.BootstrapInstanceAnnotationName] = instruction.mode

	return nil
}

// applyInitDBOverlayExtras adds the environment and SQL-refs volumes that the
// initdb Job used to receive. It mirrors the corresponding branches of
// CreatePrimaryJob, including that the APP_USERNAME gate does not apply to the
// import variant.
func applyInitDBOverlayExtras(pod *corev1.Pod, cluster apiv1.Cluster) {
	isImport := cluster.Spec.Bootstrap != nil &&
		cluster.Spec.Bootstrap.InitDB != nil &&
		cluster.Spec.Bootstrap.InitDB.Import != nil

	if !isImport && cluster.ShouldInitDBCreateApplicationDatabase() &&
		cluster.GetApplicationSecretName() != "" {
		// The secret is not needed by the bootstrap itself. We reference it to
		// ensure it is available before proceeding with the cluster initialization.
		pod.Spec.Containers[0].Env = append(pod.Spec.Containers[0].Env, corev1.EnvVar{
			Name: "APP_USERNAME",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: cluster.GetApplicationSecretName()},
					Key:                  "username",
					Optional:             ptr.To(false),
				},
			},
		})
	}

	if cluster.ShouldInitDBRunPostInitApplicationSQLRefs() {
		volumes, volumeMounts := createVolumesAndVolumeMountsForSQLRefs(
			postInitApplicationSQLRefsFolder,
			cluster.Spec.Bootstrap.InitDB.PostInitApplicationSQLRefs,
		)
		pod.Spec.Volumes = append(pod.Spec.Volumes, volumes...)
		pod.Spec.Containers[0].VolumeMounts = append(pod.Spec.Containers[0].VolumeMounts, volumeMounts...)
	}

	if cluster.ShouldInitDBRunPostInitTemplateSQLRefs() {
		volumes, volumeMounts := createVolumesAndVolumeMountsForSQLRefs(
			postInitTemplateQLRefsFolder,
			cluster.Spec.Bootstrap.InitDB.PostInitTemplateSQLRefs,
		)
		pod.Spec.Volumes = append(pod.Spec.Volumes, volumes...)
		pod.Spec.Containers[0].VolumeMounts = append(pod.Spec.Containers[0].VolumeMounts, volumeMounts...)
	}

	if cluster.ShouldInitDBRunPostInitSQLRefs() {
		volumes, volumeMounts := createVolumesAndVolumeMountsForSQLRefs(
			postInitSQLRefsFolder,
			cluster.Spec.Bootstrap.InitDB.PostInitSQLRefs,
		)
		pod.Spec.Volumes = append(pod.Spec.Volumes, volumes...)
		pod.Spec.Containers[0].VolumeMounts = append(pod.Spec.Containers[0].VolumeMounts, volumeMounts...)
	}
}
