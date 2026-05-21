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

package execute

import (
	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("setupExtensionEnvironment", func() {
	It("errors when PGDataImageInfo is missing", func() {
		cluster := &apiv1.Cluster{
			Status: apiv1.ClusterStatus{
				TargetPGDataImageInfo: &apiv1.ImageInfo{},
			},
		}
		err := setupExtensionEnvironment(cluster)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("PGDataImageInfo"))
	})

	It("errors when TargetPGDataImageInfo is missing", func() {
		cluster := &apiv1.Cluster{
			Status: apiv1.ClusterStatus{
				PGDataImageInfo: &apiv1.ImageInfo{},
			},
		}
		err := setupExtensionEnvironment(cluster)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("TargetPGDataImageInfo"))
	})

	It("succeeds with both image-info statuses present and no extensions", func() {
		cluster := &apiv1.Cluster{
			Status: apiv1.ClusterStatus{
				PGDataImageInfo:       &apiv1.ImageInfo{},
				TargetPGDataImageInfo: &apiv1.ImageInfo{},
			},
		}
		Expect(setupExtensionEnvironment(cluster)).To(Succeed())
	})
})
