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
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	testsUtils "github.com/cloudnative-pg/cloudnative-pg/tests/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Separate pg_wal volume", func() {
	const (
		namespace   = "pg-wal-volume-e2e"
		sampleFile  = fixturesDir + "/pg_wal_volume/cluster-pg-wal-volume.yaml.template"
		clusterName = "cluster-pg-wal-volume"
		level       = tests.High
	)
	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	// This test checks for separate and dedicated pg_wal volume well behaving, by
	// ensuring WAL files are archived to the correct location and a symlink
	// to the PATH is present inside the PGDATA.
	It("having a dedicated WAL volume", func() {
		// Create a cluster in a namespace we'll delete after the test
		err := env.CreateNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() error {
			if CurrentSpecReport().Failed() {
				env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
			}
			return env.DeleteNamespace(namespace)
		})

		AssertCreateCluster(namespace, clusterName, sampleFile, env)

		PgWalDir := "/var/lib/postgresql/wal/pg_wal"

		podList, err := env.GetClusterPodList(namespace, clusterName)
		Expect(len(podList.Items), err).To(BeEquivalentTo(3))

		By("checking that pg_wal PVC has been created", func() {
			for _, pod := range podList.Items {
				pvcName := pod.GetName() + "-wal"
				pvc := &corev1.PersistentVolumeClaim{}
				namespacedPVCName := types.NamespacedName{
					Namespace: namespace,
					Name:      pvcName,
				}
				err = env.Client.Get(env.Ctx, namespacedPVCName, pvc)
				Expect(pvc.GetName(), err).To(BeEquivalentTo(pvcName))
			}
			AssertPvcHasLabels(namespace, clusterName)
		})
		By("checking that pg_wal is a symlink to the dedicated volume", func() {
			for _, pod := range podList.Items {
				commandTimeout := time.Second * 5
				out, _, err := env.EventuallyExecCommand(env.Ctx, pod, specs.PostgresContainerName, &commandTimeout,
					"readlink", "-f", specs.PgWalPath)
				Expect(strings.Trim(out, "\n"), err).To(BeEquivalentTo(PgWalDir))
			}
		})
		By("checking WALs are archived in the dedicated volume", func() {
			for _, pod := range podList.Items {
				cmd := fmt.Sprintf(
					"sh -c 'find %v -maxdepth 1 -type f -regextype sed -regex %v -print | wc -l'",
					PgWalDir,
					".*[0-9]$")
				timeout := 300
				Eventually(func() (int, error, error) {
					out, _, err := testsUtils.Run(fmt.Sprintf(
						"kubectl exec -n %v %v -- %v",
						namespace,
						pod.GetName(),
						cmd),
					)
					value, atoiErr := strconv.Atoi(strings.Trim(out, "\n"))
					return value, err, atoiErr
				}, timeout).Should(BeNumerically(">=", 1))
			}
		})
	})
})
