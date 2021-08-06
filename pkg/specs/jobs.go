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
)

// CreatePrimaryJobViaInitdb creates a new primary instance in a Pod
func CreatePrimaryJobViaInitdb(cluster apiv1.Cluster, nodeSerial int32) *batchv1.Job {
	podName := fmt.Sprintf("%s-%v", cluster.Name, nodeSerial)
	jobName := fmt.Sprintf("%s-%v-initdb", cluster.Name, nodeSerial)

	initCommand := []string{
		"/controller/manager",
		"instance",
		"init",
		"--pw-file", "/etc/superuser-secret/password",
		"--parent-node", cluster.GetServiceReadWriteName(),
	}

	if cluster.Spec.Bootstrap != nil && cluster.Spec.Bootstrap.InitDB != nil {
		initCommand = append(
			initCommand,
			"--initdb-flags",
			shellquote.Join(cluster.Spec.Bootstrap.InitDB.Options...))

		if cluster.Spec.Bootstrap.InitDB.PostInitSQL != nil {
			initCommand = append(
				initCommand,
				"--post-init-sql",
				shellquote.Join(cluster.Spec.Bootstrap.InitDB.PostInitSQL...))
		}
	}

	if cluster.ShouldCreateApplicationDatabase() {
		initCommand = append(initCommand,
			"--app-db-name", cluster.Spec.Bootstrap.InitDB.Database,
			"--app-user", cluster.Spec.Bootstrap.InitDB.Owner,
			"--app-pw-file", "/etc/app-secret/password")
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: cluster.Namespace,
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						createBootstrapContainer(cluster),
					},
					Containers: []corev1.Container{
						{
							Name:            "bootstrap-instance",
							Image:           cluster.GetImageName(),
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

	addManagerLoggingOptions(&job.Spec.Template.Spec.Containers[0])

	return job
}

// CreatePrimaryJobViaRecovery creates a new primary instance in a Pod
func CreatePrimaryJobViaRecovery(cluster apiv1.Cluster, nodeSerial int32, backup *apiv1.Backup) *batchv1.Job {
	podName := fmt.Sprintf("%s-%v", cluster.Name, nodeSerial)
	jobName := fmt.Sprintf("%s-%v-full-recovery", cluster.Name, nodeSerial)

	initCommand := []string{
		"/controller/manager",
		"instance",
		"restore",
		"--pw-file", "/etc/superuser-secret/password",
		"--backup-name", cluster.Spec.Bootstrap.Recovery.Backup.Name,
	}

	if cluster.Spec.Bootstrap.Recovery.RecoveryTarget != nil {
		initCommand = append(initCommand,
			"--target",
			cluster.Spec.Bootstrap.Recovery.RecoveryTarget.BuildPostgresOptions())
	}

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
							Name:  "bootstrap-full-recovery",
							Image: cluster.GetImageName(),
							Env: append(createEnvVarPostgresContainer(cluster, podName),
								corev1.EnvVar{
									Name: "AWS_ACCESS_KEY_ID",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: apiv1.SecretKeySelectorToCore(&backup.Status.S3Credentials.AccessKeyIDReference),
									},
								},
								corev1.EnvVar{
									Name: "AWS_SECRET_ACCESS_KEY",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: apiv1.SecretKeySelectorToCore(&backup.Status.S3Credentials.SecretAccessKeyReference),
									},
								}),
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

	addManagerLoggingOptions(&job.Spec.Template.Spec.Containers[0])

	return job
}

// CreatePrimaryJobViaPgBaseBackup creates a new primary instance in a Pod
func CreatePrimaryJobViaPgBaseBackup(
	cluster apiv1.Cluster, nodeSerial int32) *batchv1.Job {
	podName := fmt.Sprintf("%s-%v", cluster.Name, nodeSerial)
	jobName := fmt.Sprintf("%s-%v-pgbasebackup", cluster.Name, nodeSerial)

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
							Name:  "bootstrap-pgbasebackup",
							Image: cluster.GetImageName(),
							Env:   createEnvVarPostgresContainer(cluster, podName),
							Command: []string{
								"/controller/manager",
								"instance",
								"pgbasebackup",
								"--pw-file", "/etc/superuser-secret/password",
							},
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

	return job
}

// JoinReplicaInstance create a new PostgreSQL node, copying the contents from another Pod
func JoinReplicaInstance(cluster apiv1.Cluster, nodeSerial int32) *batchv1.Job {
	podName := fmt.Sprintf("%s-%v", cluster.Name, nodeSerial)
	jobName := fmt.Sprintf("%s-%v-join", cluster.Name, nodeSerial)

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
							Name:  "bootstrap-replica",
							Image: cluster.GetImageName(),
							Env:   createEnvVarPostgresContainer(cluster, podName),
							Command: []string{
								"/controller/manager",
								"instance",
								"join",
								"--parent-node", cluster.GetServiceReadWriteName(),
							},
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

	addManagerLoggingOptions(&job.Spec.Template.Spec.Containers[0])

	return job
}
