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
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

// CreateMajorUpgradeJob creates a job to upgrade the primary node to a new Postgres major version
func CreateMajorUpgradeJob(cluster *apiv1.Cluster, nodeSerial int, oldImage string) *batchv1.Job {
	prepareCommand := []string{
		"/controller/manager",
		"instance",
		"upgrade",
		"prepare",
		"/controller/old",
	}
	oldVersionInitContainer := corev1.Container{
		Name:            "prepare",
		Image:           oldImage,
		ImagePullPolicy: cluster.Spec.ImagePullPolicy,
		Command:         prepareCommand,
		VolumeMounts:    createPostgresVolumeMounts(*cluster),
		Resources:       cluster.Spec.Resources,
		SecurityContext: CreateContainerSecurityContext(cluster.GetSeccompProfile()),
	}

	majorUpgradeCommand := []string{
		"/controller/manager",
		"instance",
		"upgrade",
		"execute",
		"/controller/old/bindir.txt",
	}
	job := createPrimaryJob(*cluster, nodeSerial, JobMajorUpgrade, majorUpgradeCommand)
	job.Spec.Template.Spec.InitContainers = append(job.Spec.Template.Spec.InitContainers, oldVersionInitContainer)

	return job
}
