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
	"os"
	"strings"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	postgresConfig "github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"

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

	// Regression test for https://github.com/cloudnative-pg/cloudnative-pg/issues/11285:
	// when the same extension is mounted from both the source and target images during a
	// major upgrade, the TARGET paths must precede the SOURCE paths on LD_LIBRARY_PATH and
	// PATH, so a newer native dependency (e.g. GEOS) shipped in the target image wins over
	// the older copy in the source image instead of being shadowed by it.
	It("orders target (new) extension paths before source (old) on LD_LIBRARY_PATH and PATH", func() {
		// Establish a known-empty baseline; GinkgoT().Setenv restores prior values afterwards.
		GinkgoT().Setenv("LD_LIBRARY_PATH", "")
		GinkgoT().Setenv("PATH", "")

		ext := func(name string) apiv1.ExtensionConfiguration {
			return apiv1.ExtensionConfiguration{
				Name:          name,
				LdLibraryPath: []string{"system"},
				BinPath:       []string{"bin"},
			}
		}
		cluster := &apiv1.Cluster{
			Status: apiv1.ClusterStatus{
				PGDataImageInfo: &apiv1.ImageInfo{
					Extensions: []apiv1.ExtensionConfiguration{ext("postgis")},
				},
				TargetPGDataImageInfo: &apiv1.ImageInfo{
					Extensions: []apiv1.ExtensionConfiguration{ext("postgis")},
				},
			},
		}
		Expect(setupExtensionEnvironment(cluster)).To(Succeed())

		targetLib := postgresConfig.UpgradeTargetExtensionsBaseDirectory + "/postgis/system"
		sourceLib := postgresConfig.ExtensionsBaseDirectory + "/postgis/system"
		targetBin := postgresConfig.UpgradeTargetExtensionsBaseDirectory + "/postgis/bin"
		sourceBin := postgresConfig.ExtensionsBaseDirectory + "/postgis/bin"

		ldPath := os.Getenv("LD_LIBRARY_PATH")
		Expect(ldPath).To(ContainSubstring(targetLib))
		Expect(ldPath).To(ContainSubstring(sourceLib))
		Expect(strings.Index(ldPath, targetLib)).To(BeNumerically("<", strings.Index(ldPath, sourceLib)),
			"target lib path must precede source lib path on LD_LIBRARY_PATH")

		binPath := os.Getenv("PATH")
		Expect(binPath).To(ContainSubstring(targetBin))
		Expect(binPath).To(ContainSubstring(sourceBin))
		Expect(strings.Index(binPath, targetBin)).To(BeNumerically("<", strings.Index(binPath, sourceBin)),
			"target bin path must precede source bin path on PATH")
	})
})
