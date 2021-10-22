/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package specs

import (
	"fmt"

	"github.com/kballard/go-shellquote"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
)

// CreatePrimaryJobViaInitdb creates a new primary instance in a Pod
func CreatePrimaryJobViaInitdb(cluster apiv1.Cluster, nodeSerial int32) *batchv1.Job {
	initCommand := []string{
		"/controller/manager",
		"instance",
		"init",
		"--parent-node", cluster.GetServiceReadWriteName(),
	}

	if cluster.GetEnableSuperuserAccess() {
		initCommand = append(initCommand, "--pw-file", "/etc/superuser-secret/password")
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

	if cluster.ShouldCreateApplicationDatabase() {
		initCommand = append(initCommand,
			"--app-db-name", cluster.Spec.Bootstrap.InitDB.Database,
			"--app-user", cluster.Spec.Bootstrap.InitDB.Owner,
			"--app-pw-file", "/etc/app-secret/password")
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
	if walSegmentSize := config.WalSegmentSize; walSegmentSize != "" {
		options = append(options, fmt.Sprintf("--wal-segmentsize=%s", walSegmentSize))
	}
	initCommand = append(
		initCommand,
		"--initdb-flags",
		shellquote.Join(options...))

	return initCommand
}

// CreatePrimaryJobViaRecovery creates a new primary instance in a Pod
func CreatePrimaryJobViaRecovery(cluster apiv1.Cluster, nodeSerial int32) *batchv1.Job {
	initCommand := []string{
		"/controller/manager",
		"instance",
		"restore",
	}

	if cluster.GetEnableSuperuserAccess() {
		initCommand = append(initCommand, "--pw-file", "/etc/superuser-secret/password")
	}

	if cluster.Spec.Bootstrap.Recovery.RecoveryTarget != nil {
		initCommand = append(initCommand,
			"--target",
			cluster.Spec.Bootstrap.Recovery.RecoveryTarget.BuildPostgresOptions())
	}

	return createPrimaryJob(cluster, nodeSerial, "full-recovery", initCommand)
}

// CreatePrimaryJobViaPgBaseBackup creates a new primary instance in a Pod
func CreatePrimaryJobViaPgBaseBackup(cluster apiv1.Cluster, nodeSerial int32) *batchv1.Job {
	initCommand := []string{
		"/controller/manager",
		"instance",
		"pgbasebackup",
	}

	if cluster.GetEnableSuperuserAccess() {
		initCommand = append(initCommand, "--pw-file", "/etc/superuser-secret/password")
	}

	return createPrimaryJob(cluster, nodeSerial, "pgbasebackup", initCommand)
}

// JoinReplicaInstance create a new PostgreSQL node, copying the contents from another Pod
func JoinReplicaInstance(cluster apiv1.Cluster, nodeSerial int32) *batchv1.Job {
	initCommand := []string{
		"/controller/manager",
		"instance",
		"join",
		"--parent-node", cluster.GetServiceReadWriteName(),
	}

	return createPrimaryJob(cluster, nodeSerial, "join", initCommand)
}

// createPrimaryJob create a job that executes the provided command
func createPrimaryJob(cluster apiv1.Cluster, nodeSerial int32, shortName string, initCommand []string) *batchv1.Job {
	podName := fmt.Sprintf("%s-%v", cluster.Name, nodeSerial)
	jobName := fmt.Sprintf("%s-%v-%s", cluster.Name, nodeSerial, shortName)

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
							Name:            shortName,
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

	addManagerLoggingOptions(cluster, &job.Spec.Template.Spec.Containers[0])

	return job
}
