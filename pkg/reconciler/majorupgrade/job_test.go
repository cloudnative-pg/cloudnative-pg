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

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Major upgrade Job generation", func() {
	oldImageInfo := &apiv1.ImageInfo{
		Image:        "postgres:16",
		MajorVersion: 16,
	}
	newImageName := "postgres:17"

	cluster := apiv1.Cluster{
		Spec: apiv1.ClusterSpec{
			ImageName: newImageName,
			Bootstrap: &apiv1.BootstrapConfiguration{
				InitDB: &apiv1.BootstrapInitDB{},
			},
		},
		Status: apiv1.ClusterStatus{
			Image:           newImageName,
			PGDataImageInfo: oldImageInfo.DeepCopy(),
		},
	}

	It("creates major upgrade jobs", func() {
		majorUpgradeJob := createMajorUpgradeJobDefinition(&cluster, 1, nil)
		Expect(majorUpgradeJob).ToNot(BeNil())
		Expect(majorUpgradeJob.Spec.Template.Spec.Containers[0].Image).To(Equal(newImageName))
	})

	It("is able to discover which target image was used", func() {
		majorUpgradeJob := createMajorUpgradeJobDefinition(&cluster, 1, nil)
		Expect(majorUpgradeJob).ToNot(BeNil())

		imgName, found := getTargetImageFromMajorUpgradeJob(majorUpgradeJob)
		Expect(found).To(BeTrue())
		Expect(imgName).To(Equal(newImageName))
	})

	DescribeTable(
		"Tells major upgrade jobs apart from jobs of other types",
		func(job *batchv1.Job, isMajorUpgrade bool) {
			Expect(isMajorUpgradeJob(job)).To(Equal(isMajorUpgrade))
		},
		Entry("initdb jobs are not major upgrades", specs.CreatePrimaryJobViaInitdb(cluster, 1), false),
		Entry("major-upgrade jobs are major upgrades", createMajorUpgradeJobDefinition(&cluster, 1, nil), true),
	)
})
