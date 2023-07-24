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
	"fmt"
	"os"

	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Test case for validating volume snapshots
// with different storage providers in different k8s environments
var _ = Describe("Verify volume snapshot", Label(tests.LabelBackupRestore, tests.LabelStorage), func() {
	const (
		sampleFile  = fixturesDir + "/volume_snapshot/cluster_volume_snapshot.yaml.template"
		clusterName = "volume-snapshot"
		level       = tests.Medium
	)
	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
		// This need to be removed later
		if IsLocal() {
			Skip("This test is only run on AKS, EKS and GKE clusters for now")
		}
	})
	// Initializing a global namespace variable to be used in each test case
	var namespace, namespacePrefix string
	// Gathering the default volumeSnapshot class for the current environment
	volumeSnapshotClassName := os.Getenv("E2E_DEFAULT_VOLUMESNAPSHOT_CLASS")

	Context("Can create a Volume Snapshot", Ordered, func() {
		BeforeAll(func() {
			var err error
			// Initializing namespace variable to be used in test case
			namespacePrefix = "volume-snapshot"
			namespace, err = env.CreateUniqueNamespace(namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(func() error {
				if CurrentSpecReport().Failed() {
					env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
				}
				return env.DeleteNamespace(namespace)
			})
			// Creating a cluster with three nodes
			AssertCreateCluster(namespace, clusterName, sampleFile, env)
		})

		It("Using the kubectl cnp plugin", func() {
			err := utils.CreateVolumeSnapshotBackup(volumeSnapshotClassName, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())

			out, _, err := utils.Run(fmt.Sprintf("kubectl get volumesnapshot -n %v", namespace))
			Expect(err).ToNot(HaveOccurred())
			GinkgoWriter.Print("output of current volumesnapshot")
			GinkgoWriter.Print(out)

			out, _, err = utils.Run(fmt.Sprintf("kubectl get volumesnapshotcontent -n %v", namespace))
			Expect(err).ToNot(HaveOccurred())
			GinkgoWriter.Print("output of current volumesnapshotcontent")
			GinkgoWriter.Print(out)
		})
	})
})
