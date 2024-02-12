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

var _ = Describe("Available Architectures", Label(tests.LabelBasic), func() {
	const (
		clusterManifest = fixturesDir + "/architectures/cluster-architectures.yaml.template"
		namespacePrefix = "cluster-arch-e2e"
		level           = tests.Low
	)

	// we assume the image to be built for just amd64 as default. We try to calculate other envs inside the beforeEach
	// block
	imageArchitectures := []string{"amd64"}

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}

		// TODO: instead of fetching the current architectures using the
		// PLATFORMS env variable, we should have a manager command which
		// returns all the architectures available in the current image.

		// Fetch the current image architectures via the PLATFORMS env variable.
		if architecturesFromUser, exist := os.LookupEnv("PLATFORMS"); exist {
			s := strings.ReplaceAll(architecturesFromUser, "linux/", "")
			arches := strings.Split(s, ",")
			imageArchitectures = arches
		}
	})

	// verifyArchitectureStatus checks that a given expectedValue (e.g. amd64)
	// is present in the Cluster's status AvailableArchitecture entries
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

	// verifyArchitecturesAreUnique checks that each Cluster's status
	// AvailableArchitecture entry is unique
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
