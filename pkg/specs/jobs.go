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

	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/kballard/go-shellquote"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

type postInitFolder string

const (
	// Each SQLRefsFolder entry points to the related folder containing
	// its post init SQL files, in the primary job with initdb.
	postInitApplicationSQLRefsFolder postInitFolder = "/etc/post-init-application-sql"
	postInitTemplateQLRefsFolder     postInitFolder = "/etc/post-init-template-sql"
	postInitSQLRefsFolder            postInitFolder = "/etc/post-init-sql"
)

func (p postInitFolder) toString() string {
	return string(p)
}

// InstanceBootstrapCommand describes how an instance's PGDATA should be
// bootstrapped (init/restore/restoresnapshot/pgbasebackup/join), for use as
// the instance Pod's bootstrap init container.
type InstanceBootstrapCommand struct {
	Role    jobRole
	Command []string
}

// BuildPrimaryBootstrapCommandViaInitdb builds the bootstrap command for a new primary instance
func BuildPrimaryBootstrapCommandViaInitdb(cluster apiv1.Cluster) *InstanceBootstrapCommand {
	initCommand := []string{
		"/controller/manager",
		"instance",
		"init",
	}

	if cluster.Spec.Bootstrap != nil && cluster.Spec.Bootstrap.InitDB != nil {
		initCommand = append(initCommand, buildInitDBFlags(cluster)...)
	}

	if cluster.Spec.Bootstrap.InitDB.PostInitSQL != nil {
		initCommand = append(
			initCommand,
			"--post-init-sql",
			shellquote.Join(cluster.Spec.Bootstrap.InitDB.PostInitSQL...))
	}

	if cluster.Spec.Bootstrap.InitDB.PostInitApplicationSQL != nil {
		initCommand = append(
			initCommand,
			"--post-init-application-sql",
			shellquote.Join(cluster.Spec.Bootstrap.InitDB.PostInitApplicationSQL...))
	}

	if cluster.Spec.Bootstrap.InitDB.PostInitTemplateSQL != nil {
		initCommand = append(
			initCommand,
			"--post-init-template-sql",
			shellquote.Join(cluster.Spec.Bootstrap.InitDB.PostInitTemplateSQL...))
	}

	if cluster.ShouldInitDBCreateApplicationDatabase() {
		initCommand = append(initCommand,
			"--app-db-name", cluster.Spec.Bootstrap.InitDB.Database,
			"--app-user", cluster.Spec.Bootstrap.InitDB.Owner)
	}

	initCommand = append(initCommand, buildCommonInitJobFlags(cluster)...)

	if cluster.Spec.Bootstrap.InitDB.Import != nil {
		return &InstanceBootstrapCommand{Role: jobRoleImport, Command: initCommand}
	}

	if cluster.ShouldInitDBRunPostInitApplicationSQLRefs() {
		initCommand = append(initCommand,
			"--post-init-application-sql-refs-folder", postInitApplicationSQLRefsFolder.toString())
	}

	if cluster.ShouldInitDBRunPostInitTemplateSQLRefs() {
		initCommand = append(initCommand,
			"--post-init-template-sql-refs-folder", postInitTemplateQLRefsFolder.toString())
	}

	if cluster.ShouldInitDBRunPostInitSQLRefs() {
		initCommand = append(initCommand,
			"--post-init-sql-refs-folder", postInitSQLRefsFolder.toString())
	}

	return &InstanceBootstrapCommand{Role: jobRoleInitDB, Command: initCommand}
}

func buildInitDBFlags(cluster apiv1.Cluster) (initCommand []string) {
	config := cluster.Spec.Bootstrap.InitDB
	var options []string
	// Kept for backward compatibility.
	// If set we will ignore all the explicit parameters.
	if len(config.Options) > 0 { //nolint:staticcheck
		log.Warning("initdb.options is deprecated, consider migrating to initdb explicit configuration. "+
			"Ignoring explicit configurations if present",
			"cluster", cluster.Name,
			"namespace", cluster.Namespace)

		//nolint:staticcheck // still in use for backward compatibility
		options = append(options, config.Options...)
		initCommand = append(
			initCommand,
			"--initdb-flags",
			shellquote.Join(options...))
		return initCommand
	}
	if config.DataChecksums != nil &&
		*config.DataChecksums {
		options = append(options, "-k")
	}
	if logLevel := cluster.Spec.LogLevel; log.DebugLevelString == logLevel ||
		log.TraceLevelString == logLevel {
		options = append(options, "-d")
	}
	if encoding := config.Encoding; encoding != "" {
		options = append(options, fmt.Sprintf("--encoding=%s", encoding))
	}
	if localeCollate := config.LocaleCollate; localeCollate != "" {
		options = append(options, fmt.Sprintf("--lc-collate=%s", localeCollate))
	}
	if localeCType := config.LocaleCType; localeCType != "" {
		options = append(options, fmt.Sprintf("--lc-ctype=%s", localeCType))
	}
	if locale := config.Locale; locale != "" {
		options = append(options, fmt.Sprintf("--locale=%s", locale))
	}
	if localeProvider := config.LocaleProvider; localeProvider != "" {
		options = append(options, fmt.Sprintf("--locale-provider=%s", localeProvider))
	}
	if icuLocale := config.IcuLocale; icuLocale != "" {
		options = append(options, fmt.Sprintf("--icu-locale=%s", icuLocale))
	}
	if icuRules := config.IcuRules; icuRules != "" {
		options = append(options, fmt.Sprintf("--icu-rules=%s", icuRules))
	}
	if builtinLocale := config.BuiltinLocale; builtinLocale != "" {
		options = append(options, fmt.Sprintf("--builtin-locale=%s", builtinLocale))
	}
	if walSegmentSize := config.WalSegmentSize; walSegmentSize != 0 && utils.IsPowerOfTwo(walSegmentSize) {
		options = append(options, fmt.Sprintf("--wal-segsize=%v", walSegmentSize))
	}
	initCommand = append(
		initCommand,
		"--initdb-flags",
		shellquote.Join(options...))

	return initCommand
}

// BuildPrimaryBootstrapCommandViaRestoreSnapshot builds the bootstrap command
// for a new primary instance, restoring from a volumeSnapshot
func BuildPrimaryBootstrapCommandViaRestoreSnapshot(
	cluster apiv1.Cluster,
	object *metav1.ObjectMeta,
) *InstanceBootstrapCommand {
	initCommand := []string{
		"/controller/manager",
		"instance",
		"restoresnapshot",
	}

	if object.Annotations[utils.BackupLabelFileAnnotationName] != "" {
		flag := fmt.Sprintf("--backuplabel=%s", object.Annotations[utils.BackupLabelFileAnnotationName])
		initCommand = append(initCommand, flag)
	}

	if object.Annotations[utils.BackupTablespaceMapFileAnnotationName] != "" {
		flag := fmt.Sprintf("--tablespacemap=%s", object.Annotations[utils.BackupTablespaceMapFileAnnotationName])
		initCommand = append(initCommand, flag)
	}

	initCommand = append(initCommand, buildCommonInitJobFlags(cluster)...)

	return &InstanceBootstrapCommand{Role: jobRoleSnapshotRecovery, Command: initCommand}
}

// BuildPrimaryBootstrapCommandViaRecovery builds the bootstrap command for a
// new primary instance, restoring from a Backup
func BuildPrimaryBootstrapCommandViaRecovery(cluster apiv1.Cluster) *InstanceBootstrapCommand {
	commonFlags := buildCommonInitJobFlags(cluster)
	initCommand := make([]string, 0, 3+len(commonFlags))
	initCommand = append(initCommand,
		"/controller/manager",
		"instance",
		"restore",
	)

	initCommand = append(initCommand, commonFlags...)

	return &InstanceBootstrapCommand{Role: jobRoleFullRecovery, Command: initCommand}
}

// BuildPrimaryBootstrapCommandViaPgBaseBackup builds the bootstrap command for a new primary instance
func BuildPrimaryBootstrapCommandViaPgBaseBackup(cluster apiv1.Cluster) *InstanceBootstrapCommand {
	commonFlags := buildCommonInitJobFlags(cluster)
	initCommand := make([]string, 0, 3+len(commonFlags))
	initCommand = append(initCommand,
		"/controller/manager",
		"instance",
		"pgbasebackup",
	)

	initCommand = append(initCommand, commonFlags...)

	return &InstanceBootstrapCommand{Role: jobRolePGBaseBackup, Command: initCommand}
}

// BuildReplicaBootstrapCommandViaJoin builds the bootstrap command for a new
// replica instance, copying the contents from another Pod
func BuildReplicaBootstrapCommandViaJoin(cluster apiv1.Cluster) *InstanceBootstrapCommand {
	commonFlags := buildCommonInitJobFlags(cluster)
	initCommand := make([]string, 0, 5+len(commonFlags))
	initCommand = append(initCommand,
		"/controller/manager",
		"instance",
		"join",
		"--parent-node", cluster.GetServiceReadWriteName(),
	)

	initCommand = append(initCommand, commonFlags...)

	return &InstanceBootstrapCommand{Role: jobRoleJoin, Command: initCommand}
}

// BuildReplicaBootstrapCommandViaRestoreSnapshot builds the bootstrap command
// for a new replica instance, starting from a volume snapshot backup
func BuildReplicaBootstrapCommandViaRestoreSnapshot(cluster apiv1.Cluster) *InstanceBootstrapCommand {
	commonFlags := buildCommonInitJobFlags(cluster)
	initCommand := make([]string, 0, 4+len(commonFlags))
	initCommand = append(initCommand,
		"/controller/manager",
		"instance",
		"restoresnapshot",
		"--immediate",
	)

	initCommand = append(initCommand, commonFlags...)

	return &InstanceBootstrapCommand{Role: jobRoleSnapshotRecovery, Command: initCommand}
}

func buildCommonInitJobFlags(cluster apiv1.Cluster) []string {
	var flags []string

	if cluster.ShouldCreateWalArchiveVolume() {
		flags = append(flags, "--pg-wal", PgWalVolumePgWalPath)
	}

	return flags
}

// jobRole describe a possible type of job
type jobRole string

const (
	jobRoleImport           jobRole = "import"
	jobRoleInitDB           jobRole = "initdb"
	jobRolePGBaseBackup     jobRole = "pgbasebackup"
	jobRoleFullRecovery     jobRole = "full-recovery"
	jobRoleJoin             jobRole = "join"
	jobRoleSnapshotRecovery jobRole = "snapshot-recovery"
)

// getJobName returns a string indicating the job name
func (role jobRole) getJobName(instanceName string) string {
	return fmt.Sprintf("%s-%s", instanceName, role)
}

// InstanceInitContainerConfig describes the container that runs a given
// instance-bootstrap command (init/join/restore/restoresnapshot/pgbasebackup)
type InstanceInitContainerConfig struct {
	Cluster     apiv1.Cluster
	Name        string
	Role        jobRole
	EnvConfig   EnvConfig
	InitCommand []string
	Extensions  []apiv1.ExtensionConfiguration
}

// InstanceInitContainer is the container built from an
// InstanceInitContainerConfig, together with any extra Pod-level volumes it
// requires (e.g. for post-init SQL refs)
type InstanceInitContainer struct {
	Container corev1.Container
	Volumes   []corev1.Volume
}

// createInstanceInitContainer builds the container that runs the given
// instance-bootstrap command. It is shared between CreatePrimaryJob (Job
// path) and the instance Pod's bootstrap init container.
func createInstanceInitContainer(config InstanceInitContainerConfig) InstanceInitContainer {
	cluster := config.Cluster
	role := config.Role

	container := corev1.Container{
		Name:            config.Name,
		Image:           cluster.Status.Image,
		ImagePullPolicy: cluster.Spec.ImagePullPolicy,
		Env:             config.EnvConfig.EnvVars,
		EnvFrom:         config.EnvConfig.EnvFrom,
		Command:         config.InitCommand,
		VolumeMounts: CreatePostgresVolumeMounts(VolumeMountsConfig{
			Cluster:            cluster,
			Extensions:         config.Extensions,
			NeedsKubeAPIAccess: true,
		}),
		Resources:       cluster.Spec.Resources,
		SecurityContext: GetSecurityContext(&cluster),
	}
	addManagerLoggingOptions(cluster, &container)

	if role == jobRoleInitDB && cluster.ShouldInitDBCreateApplicationDatabase() &&
		cluster.GetApplicationSecretName() != "" {
		// The secret is not needed by the initdb job. We do this to ensure that the secret is available
		// before proceeding with the cluster initialization
		container.Env = append(container.Env, corev1.EnvVar{
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

	result := InstanceInitContainer{Container: container}

	if cluster.ShouldInitDBRunPostInitApplicationSQLRefs() {
		volumes, volumeMounts := createVolumesAndVolumeMountsForSQLRefs(
			postInitApplicationSQLRefsFolder,
			cluster.Spec.Bootstrap.InitDB.PostInitApplicationSQLRefs,
		)
		result.Volumes = append(result.Volumes, volumes...)
		result.Container.VolumeMounts = append(result.Container.VolumeMounts, volumeMounts...)
	}

	if cluster.ShouldInitDBRunPostInitTemplateSQLRefs() {
		volumes, volumeMounts := createVolumesAndVolumeMountsForSQLRefs(
			postInitTemplateQLRefsFolder,
			cluster.Spec.Bootstrap.InitDB.PostInitTemplateSQLRefs,
		)
		result.Volumes = append(result.Volumes, volumes...)
		result.Container.VolumeMounts = append(result.Container.VolumeMounts, volumeMounts...)
	}

	if cluster.ShouldInitDBRunPostInitSQLRefs() {
		volumes, volumeMounts := createVolumesAndVolumeMountsForSQLRefs(
			postInitSQLRefsFolder,
			cluster.Spec.Bootstrap.InitDB.PostInitSQLRefs,
		)
		result.Volumes = append(result.Volumes, volumes...)
		result.Container.VolumeMounts = append(result.Container.VolumeMounts, volumeMounts...)
	}

	return result
}

// CreatePrimaryJob create a job that executes the provided command.
// The role should describe the purpose of the executed job
func CreatePrimaryJob(
	cluster apiv1.Cluster,
	nodeSerial int,
	role jobRole,
	initCommand []string,
	extList []apiv1.ExtensionConfiguration,
) *batchv1.Job {
	instanceName := GetInstanceName(cluster.Name, nodeSerial)
	jobName := role.getJobName(instanceName)
	version, _ := cluster.GetPostgresqlMajorVersion()

	envConfig := CreatePodEnvConfig(cluster, jobName)
	initContainer := createInstanceInitContainer(InstanceInitContainerConfig{
		Cluster:     cluster,
		Name:        string(role),
		Role:        role,
		EnvConfig:   envConfig,
		InitCommand: initCommand,
		Extensions:  extList,
	})

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: cluster.Namespace,
			Labels: map[string]string{
				utils.InstanceNameLabelName:           instanceName,
				utils.ClusterLabelName:                cluster.Name,
				utils.JobRoleLabelName:                string(role),
				utils.KubernetesAppLabelName:          utils.AppName,
				utils.KubernetesAppInstanceLabelName:  cluster.Name,
				utils.KubernetesAppVersionLabelName:   fmt.Sprint(version),
				utils.KubernetesAppComponentLabelName: utils.DatabaseComponentName,
				utils.KubernetesAppManagedByLabelName: utils.ManagerName,
			},
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						utils.InstanceNameLabelName:           instanceName,
						utils.ClusterLabelName:                cluster.Name,
						utils.JobRoleLabelName:                string(role),
						utils.KubernetesAppLabelName:          utils.AppName,
						utils.KubernetesAppInstanceLabelName:  cluster.Name,
						utils.KubernetesAppVersionLabelName:   fmt.Sprint(version),
						utils.KubernetesAppComponentLabelName: utils.DatabaseComponentName,
						utils.KubernetesAppManagedByLabelName: utils.ManagerName,
					},
				},
				Spec: corev1.PodSpec{
					Hostname: jobName,
					InitContainers: []corev1.Container{
						createBootstrapContainer(cluster, extList),
					},
					SchedulerName: cluster.Spec.SchedulerName,
					Containers: []corev1.Container{
						initContainer.Container,
					},
					Volumes: append(
						createPostgresVolumes(&cluster, instanceName, extList),
						initContainer.Volumes...,
					),
					SecurityContext:              GetPodSecurityContext(&cluster),
					Affinity:                     CreateAffinitySection(cluster.Name, cluster.Spec.Affinity),
					Tolerations:                  cluster.Spec.Affinity.Tolerations,
					ServiceAccountName:           cluster.GetServiceAccountName(),
					AutomountServiceAccountToken: ptr.To(false),
					RestartPolicy:                corev1.RestartPolicyNever,
					NodeSelector:                 cluster.Spec.Affinity.NodeSelector,
					TopologySpreadConstraints:    cluster.Spec.TopologySpreadConstraints,
				},
			},
		},
	}

	if configuration.Current.CreateAnyService {
		job.Spec.Template.Spec.Subdomain = cluster.GetServiceAnyName()
	}

	cluster.SetInheritedDataAndOwnership(&job.ObjectMeta)
	if utils.IsAnnotationAppArmorPresent(&job.Spec.Template.Spec, cluster.Annotations) {
		utils.AnnotateAppArmor(&job.ObjectMeta, &job.Spec.Template.Spec, cluster.Annotations)
	}

	if cluster.Spec.PriorityClassName != "" {
		job.Spec.Template.Spec.PriorityClassName = cluster.Spec.PriorityClassName
	}

	return job
}
