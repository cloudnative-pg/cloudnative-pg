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

package majorupgrade

import (
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

const jobMajorUpgrade = "major-upgrade"

// isMajorUpgradeJob tells if the passed Job definition corresponds to
// the job handling the major upgrade
func isMajorUpgradeJob(job *batchv1.Job) bool {
	return job.GetLabels()[utils.JobRoleLabelName] == string(jobMajorUpgrade)
}

// getTargetImageFromMajorUpgradeJob gets the image that is being used as
// target of the major upgrade process.
func getTargetImageFromMajorUpgradeJob(job *batchv1.Job) (string, bool) {
	if !isMajorUpgradeJob(job) {
		return "", false
	}

	for _, container := range job.Spec.Template.Spec.Containers {
		if container.Name == string(jobMajorUpgrade) {
			return container.Image, true
		}
	}

	return "", false
}

// createMajorUpgradeJobDefinition creates a job to upgrade the primary node to a new Postgres major version
func createMajorUpgradeJobDefinition(cluster *apiv1.Cluster, nodeSerial int) (*batchv1.Job, error) {
	prepareCommand := []string{
		"/controller/manager",
		"instance",
		"upgrade",
		"prepare",
		"/controller/old",
	}
	oldVersionInitContainer := corev1.Container{
		Name:            "prepare",
		Image:           cluster.Status.PGDataImageInfo.Image,
		ImagePullPolicy: cluster.Spec.ImagePullPolicy,
		Command:         prepareCommand,
		VolumeMounts:    specs.CreatePostgresVolumeMounts(*cluster),
		Resources:       cluster.Spec.Resources,
		SecurityContext: specs.GetSecurityContext(cluster),
	}

	majorUpgradeCommand := []string{
		"/controller/manager",
		"instance",
		"upgrade",
		"execute",
		"/controller/old/bindir.txt",
	}
	job, err := specs.CreatePrimaryJob(*cluster, nodeSerial, jobMajorUpgrade, majorUpgradeCommand)
	if err != nil {
		return nil, err
	}
	job.Spec.Template.Spec.InitContainers = append(job.Spec.Template.Spec.InitContainers, oldVersionInitContainer)

	return job, nil
}
