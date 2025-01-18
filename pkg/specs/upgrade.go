package specs

import (
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

// CreateMajorUpgradeJob creates a job to upgrade the primary node to a new major version
func CreateMajorUpgradeJob(cluster *apiv1.Cluster, nodeSerial int, oldImage string) *batchv1.Job {
	prepareCommand := []string{
		"/controller/manager",
		"instance",
		"upgrade",
		"prepare",
		"/controller/old",
	}

	upgradeCommand := []string{
		"/controller/manager",
		"instance",
		"upgrade",
		"execute",
		"/controller/old/bindir.txt",
	}
	job := createPrimaryJob(*cluster, nodeSerial, jobMajorUpgrade, upgradeCommand)

	oldVersionInitContainer := corev1.Container{
		Name:            "prepare",
		Image:           oldImage,
		ImagePullPolicy: cluster.Spec.ImagePullPolicy,
		Command:         prepareCommand,
		VolumeMounts:    createPostgresVolumeMounts(*cluster),
		Resources:       cluster.Spec.Resources,
		SecurityContext: CreateContainerSecurityContext(cluster.GetSeccompProfile()),
	}
	job.Spec.Template.Spec.InitContainers = append(job.Spec.Template.Spec.InitContainers, oldVersionInitContainer)

	return job
}
