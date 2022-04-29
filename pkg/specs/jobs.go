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

	"github.com/kballard/go-shellquote"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// CreatePrimaryJobViaInitdb creates a new primary instance in a Pod
func CreatePrimaryJobViaInitdb(cluster apiv1.Cluster, nodeSerial int) *batchv1.Job {
	initCommand := []string{
		"/controller/manager",
		"instance",
		"init",
		"--parent-node", cluster.GetServiceReadWriteName(),
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

	return createPrimaryJob(cluster, nodeSerial, "initdb", initCommand)
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
	if encoding := config.Encoding; encoding != "" {
		options = append(options, fmt.Sprintf("--encoding=%s", encoding))
	}
	if localeCollate := config.LocaleCollate; localeCollate != "" {
		options = append(options, fmt.Sprintf("--lc-collate=%s", localeCollate))
	}
	if localeCType := config.LocaleCType; localeCType != "" {
		options = append(options, fmt.Sprintf("--lc-ctype=%s", localeCType))
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

// CreatePrimaryJobViaRecovery creates a new primary instance in a Pod
func CreatePrimaryJobViaRecovery(cluster apiv1.Cluster, nodeSerial int, backup *apiv1.Backup) *batchv1.Job {
	initCommand := []string{
		"/controller/manager",
		"instance",
		"restore",
	}

	if cluster.ShouldRecoveryCreateApplicationDatabase() {
		initCommand = append(initCommand,
			"--app-db-name", cluster.Spec.Bootstrap.Recovery.Database,
			"--app-user", cluster.Spec.Bootstrap.Recovery.Owner)
	}

	job := createPrimaryJob(cluster, nodeSerial, "full-recovery", initCommand)

	addBarmanEndpointCAToJob(cluster, backup, job)

	return job
}

func addBarmanEndpointCAToJob(cluster apiv1.Cluster, backup *apiv1.Backup, job *batchv1.Job) {
	var secretName, secretKey string
	var isAzure bool
	switch {
	case cluster.Spec.Bootstrap.Recovery.Backup != nil && cluster.Spec.Bootstrap.Recovery.Backup.EndpointCA != nil:
		secretName = cluster.Spec.Bootstrap.Recovery.Backup.EndpointCA.Name
		secretKey = cluster.Spec.Bootstrap.Recovery.Backup.EndpointCA.Key
	case backup != nil && backup.Status.EndpointCA != nil:
		secretName = backup.Status.EndpointCA.Name
		secretKey = backup.Status.EndpointCA.Key
		if backup.Status.AzureCredentials != nil {
			isAzure = true
		}
	case cluster.Spec.Bootstrap.Recovery.Source != "":
		externalCluster, ok := cluster.ExternalCluster(cluster.Spec.Bootstrap.Recovery.Source)
		if ok && externalCluster.BarmanObjectStore != nil && externalCluster.BarmanObjectStore.EndpointCA != nil {
			secretName = externalCluster.BarmanObjectStore.EndpointCA.Name
			secretKey = externalCluster.BarmanObjectStore.EndpointCA.Key
			if externalCluster.BarmanObjectStore.AzureCredentials != nil {
				isAzure = true
			}
		}
	}

	if secretName != "" && secretKey != "" {
		job.Spec.Template.Spec.Volumes = append(job.Spec.Template.Spec.Volumes, corev1.Volume{
			Name: "barman-endpoint-ca",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: secretName,
					Items: []corev1.KeyToPath{
						{
							Key:  secretKey,
							Path: postgres.BarmanRestoreEndpointCACertificateFileName,
						},
					},
				},
			},
		})

		job.Spec.Template.Spec.Containers[0].VolumeMounts = append(job.Spec.Template.Spec.Containers[0].VolumeMounts,
			corev1.VolumeMount{
				Name:      "barman-endpoint-ca",
				MountPath: postgres.CertificatesDir,
			},
		)

		var CAEnvVariable string
		if isAzure {
			CAEnvVariable = "REQUESTS_CA_BUNDLE"
		} else {
			CAEnvVariable = "AWS_CA_BUNDLE"
		}

		job.Spec.Template.Spec.Containers[0].Env = append(job.Spec.Template.Spec.Containers[0].Env, corev1.EnvVar{
			Name:  CAEnvVariable,
			Value: postgres.BarmanRestoreEndpointCACertificateLocation,
		})
	}
}

// CreatePrimaryJobViaPgBaseBackup creates a new primary instance in a Pod
func CreatePrimaryJobViaPgBaseBackup(cluster apiv1.Cluster, nodeSerial int) *batchv1.Job {
	initCommand := []string{
		"/controller/manager",
		"instance",
		"pgbasebackup",
	}
	if cluster.ShouldPgBaseBackupCreateApplicationDatabase() {
		initCommand = append(initCommand,
			"--app-db-name", cluster.Spec.Bootstrap.PgBaseBackup.Database,
			"--app-user", cluster.Spec.Bootstrap.PgBaseBackup.Owner)
	}
	return createPrimaryJob(cluster, nodeSerial, "pgbasebackup", initCommand)
}

// JoinReplicaInstance create a new PostgreSQL node, copying the contents from another Pod
func JoinReplicaInstance(cluster apiv1.Cluster, nodeSerial int) *batchv1.Job {
	initCommand := []string{
		"/controller/manager",
		"instance",
		"join",
		"--parent-node", cluster.GetServiceReadWriteName(),
	}

	return createPrimaryJob(cluster, nodeSerial, "join", initCommand)
}

// createPrimaryJob create a job that executes the provided command.
// The role should describe the purpose of the executed job
func createPrimaryJob(cluster apiv1.Cluster, nodeSerial int, role string, initCommand []string) *batchv1.Job {
	podName := fmt.Sprintf("%s-%v", cluster.Name, nodeSerial)
	jobName := fmt.Sprintf("%s-%v-%s", cluster.Name, nodeSerial, role)

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: cluster.Namespace,
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Hostname:  jobName,
					Subdomain: cluster.GetServiceAnyName(),
					InitContainers: []corev1.Container{
						createBootstrapContainer(cluster),
					},
					Containers: []corev1.Container{
						{
							Name:            role,
							Image:           cluster.GetImageName(),
							ImagePullPolicy: cluster.Spec.ImagePullPolicy,
							Env:             createEnvVarPostgresContainer(cluster, podName),
							Command:         initCommand,
							VolumeMounts:    createPostgresVolumeMounts(cluster),
							Resources:       cluster.Spec.Resources,
							SecurityContext: CreateContainerSecurityContext(),
						},
					},
					Volumes:            createPostgresVolumes(cluster, podName),
					SecurityContext:    CreatePostgresSecurityContext(cluster.GetPostgresUID(), cluster.GetPostgresGID()),
					Affinity:           CreateAffinitySection(cluster.Name, cluster.Spec.Affinity),
					Tolerations:        cluster.Spec.Affinity.Tolerations,
					ServiceAccountName: cluster.Name,
					RestartPolicy:      corev1.RestartPolicyNever,
					NodeSelector:       cluster.Spec.Affinity.NodeSelector,
				},
			},
		},
	}

	utils.LabelJobRole(&job.ObjectMeta, role)
	utils.LabelClusterName(&job.ObjectMeta, cluster.Name)
	addManagerLoggingOptions(cluster, &job.Spec.Template.Spec.Containers[0])
	if utils.IsAnnotationAppArmorPresent(cluster.Annotations) {
		utils.AnnotateAppArmor(&job.ObjectMeta, cluster.Annotations)
	}

	return job
}
