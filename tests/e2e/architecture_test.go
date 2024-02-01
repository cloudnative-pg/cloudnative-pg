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

package e2e

import (
	"os"
	"strings"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/tests"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = FDescribe("Available Architectures", func() {
	const (
		clusterManifest = fixturesDir + "/architectures/cluster-architectures.yaml.template"
		namespacePrefix = "cluster-arch-e2e"
		level           = tests.Low
	)

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	getImageArchitectures := func() []string {
		// Fetching PLATFORMS env variable.
		// If not present, we assume the image to be built for just amd64
		imageArchitectures := []string{"amd64"}
		if architecturesFromUser, exist := os.LookupEnv("PLATFORMS"); exist {
			s := strings.ReplaceAll(architecturesFromUser, "linux/", "")
			arches := strings.Split(s, ",")
			imageArchitectures = arches
		}

		return imageArchitectures
	}

	verifyArchitectureStatus := func(
		architectureStatus []apiv1.AvailableArchitecture,
		expectedValue string,
	) bool {
		found := false
		for _, item := range architectureStatus {
			if expectedValue == item.GoArch {
				found = true
			}
		}
		return found
	}

	verifyArchitecturesAreUnique := func(
		architectureStatus []apiv1.AvailableArchitecture,
	) bool {
		m := make(map[apiv1.AvailableArchitecture]struct{})

		for _, item := range architectureStatus {
			if _, ok := m[item]; ok {
				return false
			}
			m[item] = struct{}{}
		}

		return true
	}

	It("manages each available architecture", func() {
		namespace, err := env.CreateUniqueNamespace(namespacePrefix)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() error {
			if CurrentSpecReport().Failed() {
				env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
			}
			return env.DeleteNamespace(namespace)
		})

		clusterName, err := env.GetResourceNameFromYAML(clusterManifest)
		Expect(err).ToNot(HaveOccurred())
		AssertCreateCluster(namespace, clusterName, clusterManifest, env)

		// Fetch the image's architectures
		imageArchitectures := getImageArchitectures()

		// Fetch the Cluster status
		cluster, err := env.GetCluster(namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		archStatus := cluster.Status.AvailableArchitectures

		By("verifying that each given architecture is found in the Cluster status", func() {
			for _, imageArch := range imageArchitectures {
				Expect(verifyArchitectureStatus(archStatus, imageArch)).To(BeTrue(),
					"Expected architecture %v to be present in the cluster's status."+
						"\nStatus:\n%v", imageArch, archStatus)
			}
		})

		By("checking architecture's hashes are correctly populated", func() {
			// Verify that hashes are not empty
			for _, item := range archStatus {
				Expect(item.Hash).ToNot(BeEmpty(),
					"Expected hash of %v to not be empty."+
						"\nStatus:\n%v", item.GoArch, archStatus)
			}
			// Verify that all status entries are unique
			Expect(verifyArchitecturesAreUnique(archStatus)).To(BeTrue(),
				"Expected each status entry to be unique."+
					"\nStatus:\n%v", archStatus)
		})
	})
})
