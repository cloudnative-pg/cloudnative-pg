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

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

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

func buildCommonInitJobFlags(cluster apiv1.Cluster) []string {
	var flags []string

	if cluster.ShouldCreateWalArchiveVolume() {
		flags = append(flags, "--pg-wal", PgWalVolumePgWalPath)
	}

	return flags
}

type jobRole string

// getJobName returns a string indicating the job name
func (role jobRole) getJobName(instanceName string) string {
	return fmt.Sprintf("%s-%s", instanceName, role)
}

// CreateRoleJob create a job that executes the provided command.
// The role should describe the purpose of the executed job
func CreateRoleJob(
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
						{
							Name:            string(role),
							Image:           cluster.Status.Image,
							ImagePullPolicy: cluster.Spec.ImagePullPolicy,
							Env:             envConfig.EnvVars,
							EnvFrom:         envConfig.EnvFrom,
							Command:         initCommand,
							VolumeMounts:    CreatePostgresVolumeMounts(cluster, extList),
							Resources:       cluster.Spec.Resources,
							SecurityContext: GetSecurityContext(&cluster),
						},
					},
					Volumes:                   createPostgresVolumes(&cluster, instanceName, extList),
					SecurityContext:           GetPodSecurityContext(&cluster),
					Affinity:                  CreateAffinitySection(cluster.Name, cluster.Spec.Affinity),
					Tolerations:               cluster.Spec.Affinity.Tolerations,
					ServiceAccountName:        cluster.GetServiceAccountName(),
					RestartPolicy:             corev1.RestartPolicyNever,
					NodeSelector:              cluster.Spec.Affinity.NodeSelector,
					TopologySpreadConstraints: cluster.Spec.TopologySpreadConstraints,
				},
			},
		},
	}

	if configuration.Current.CreateAnyService {
		job.Spec.Template.Spec.Subdomain = cluster.GetServiceAnyName()
	}

	cluster.SetInheritedDataAndOwnership(&job.ObjectMeta)
	addManagerLoggingOptions(cluster, &job.Spec.Template.Spec.Containers[0])
	if utils.IsAnnotationAppArmorPresent(&job.Spec.Template.Spec, cluster.Annotations) {
		utils.AnnotateAppArmor(&job.ObjectMeta, &job.Spec.Template.Spec, cluster.Annotations)
	}

	if cluster.ShouldInitDBRunPostInitApplicationSQLRefs() {
		volumes, volumeMounts := createVolumesAndVolumeMountsForSQLRefs(
			postInitApplicationSQLRefsFolder,
			cluster.Spec.Bootstrap.InitDB.PostInitApplicationSQLRefs,
		)
		job.Spec.Template.Spec.Volumes = append(job.Spec.Template.Spec.Volumes, volumes...)
		job.Spec.Template.Spec.Containers[0].VolumeMounts = append(
			job.Spec.Template.Spec.Containers[0].VolumeMounts, volumeMounts...)
	}

	if cluster.ShouldInitDBRunPostInitTemplateSQLRefs() {
		volumes, volumeMounts := createVolumesAndVolumeMountsForSQLRefs(
			postInitTemplateQLRefsFolder,
			cluster.Spec.Bootstrap.InitDB.PostInitTemplateSQLRefs,
		)
		job.Spec.Template.Spec.Volumes = append(job.Spec.Template.Spec.Volumes, volumes...)
		job.Spec.Template.Spec.Containers[0].VolumeMounts = append(
			job.Spec.Template.Spec.Containers[0].VolumeMounts, volumeMounts...)
	}

	if cluster.ShouldInitDBRunPostInitSQLRefs() {
		volumes, volumeMounts := createVolumesAndVolumeMountsForSQLRefs(
			postInitSQLRefsFolder,
			cluster.Spec.Bootstrap.InitDB.PostInitSQLRefs,
		)
		job.Spec.Template.Spec.Volumes = append(job.Spec.Template.Spec.Volumes, volumes...)
		job.Spec.Template.Spec.Containers[0].VolumeMounts = append(
			job.Spec.Template.Spec.Containers[0].VolumeMounts, volumeMounts...)
	}

	if cluster.Spec.PriorityClassName != "" {
		job.Spec.Template.Spec.PriorityClassName = cluster.Spec.PriorityClassName
	}

	return job
}
