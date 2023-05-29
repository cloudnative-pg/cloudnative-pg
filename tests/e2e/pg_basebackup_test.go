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
	"strings"

	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Bootstrap with pg_basebackup using basic auth", Label(tests.LabelRecovery), func() {
	const (
		namespacePrefix = "cluster-pg-basebackup-basic-auth"
		srcCluster      = fixturesDir + "/pg_basebackup/cluster-src.yaml.template"
		srcClusterName  = "pg-basebackup-src"
		dstCluster      = fixturesDir + "/pg_basebackup/cluster-dst-basic-auth.yaml.template"
		dstClusterName  = "pg-basebackup-dst-basic-auth"
		checkQuery      = "psql -U postgres app -tAc 'SELECT count(*) FROM to_bootstrap'"
		level           = tests.High
	)
	var namespace string
	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	It("can bootstrap with pg_basebackup using basic auth", func() {
		var err error
		namespace, err = env.CreateUniqueNamespace(namespacePrefix)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() error {
			if CurrentSpecReport().Failed() {
				env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
			}
			return env.DeleteNamespace(namespace)
		})
		primarySrc := AssertSetupPgBasebackup(namespace, srcClusterName, srcCluster)

		primaryDst := dstClusterName + "-1"

		By("creating the dst cluster", func() {
			AssertCreateCluster(namespace, dstClusterName, dstCluster, env)

			// We give more time than the usual 600s, since the recovery is slower
			AssertClusterIsReady(namespace, dstClusterName, testTimeouts[utils.ClusterIsReadySlow], env)
		})

		By("checking the dst cluster with auto generated app password connectable", func() {
			secretName := dstClusterName + "-app"
			AssertApplicationDatabaseConnection(namespace, dstClusterName, "appuser", "app", "", secretName, psqlClientPod)
		})

		By("update user application password for dst cluster and verify connectivity", func() {
			secretName := dstClusterName + "-app"
			const newPassword = "eeh2Zahohx" //nolint:gosec
			AssertUpdateSecret("password", newPassword, secretName, namespace, dstClusterName, 30, env)
			AssertApplicationDatabaseConnection(
				namespace,
				dstClusterName,
				"appuser",
				"app",
				newPassword,
				secretName,
				psqlClientPod)
		})

		By("checking data have been copied correctly", func() {
			// Test data should be present on restored primary
			out, _, err := utils.Run(fmt.Sprintf(
				"kubectl exec -n %v %v -- %v",
				namespace,
				primaryDst,
				checkQuery))
			Expect(strings.Trim(out, "\n"), err).To(BeEquivalentTo("2"))
		})

		By("writing some new data to the dst cluster", func() {
			insertRecordIntoTable(namespace, dstClusterName, "to_bootstrap", 3, psqlClientPod)
		})

		By("checking the src cluster was not modified", func() {
			out, _, err := utils.Run(fmt.Sprintf(
				"kubectl exec -n %v %v -- %v",
				namespace,
				primarySrc,
				checkQuery))
			Expect(strings.Trim(out, "\n"), err).To(BeEquivalentTo("2"))
			Expect(err).ToNot(HaveOccurred())
		})
	})
})

var _ = Describe("Bootstrap with pg_basebackup using TLS auth", Label(tests.LabelRecovery), func() {
	const namespacePrefix = "cluster-pg-basebackup-tls-auth"

	const srcCluster = fixturesDir + "/pg_basebackup/cluster-src.yaml.template"
	const srcClusterName = "pg-basebackup-src"

	const dstCluster = fixturesDir + "/pg_basebackup/cluster-dst-tls.yaml.template"
	const dstClusterName = "pg-basebackup-dst-tls-auth"

	const checkQuery = "psql -U postgres app -tAc 'SELECT count(*) FROM to_bootstrap'"
	var namespace string
	It("can bootstrap with pg_basebackup using TLS auth", func() {
		var err error
		namespace, err = env.CreateUniqueNamespace(namespacePrefix)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() error {
			if CurrentSpecReport().Failed() {
				env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
			}
			return env.DeleteNamespace(namespace)
		})
		primarySrc := AssertSetupPgBasebackup(namespace, srcClusterName, srcCluster)

		primaryDst := dstClusterName + "-1"
		By("creating the dst cluster", func() {
			AssertCreateCluster(namespace, dstClusterName, dstCluster, env)

			// We give more time than the usual 600s, since the recovery is slower
			AssertClusterIsReady(namespace, dstClusterName, testTimeouts[utils.ClusterIsReadySlow], env)
		})

		By("checking data have been copied correctly", func() {
			// Test data should be present on restored primary
			out, _, err := utils.Run(fmt.Sprintf(
				"kubectl exec -n %v %v -- %v",
				namespace,
				primaryDst,
				checkQuery))
			Expect(err).ToNot(HaveOccurred())
			Expect(strings.Trim(out, "\n"), err).To(BeEquivalentTo("2"))
		})

		By("writing some new data to the dst cluster", func() {
			insertRecordIntoTable(namespace, dstClusterName, "to_bootstrap", 3, psqlClientPod)
		})

		By("checking the src cluster was not modified", func() {
			out, _, err := utils.Run(fmt.Sprintf(
				"kubectl exec -n %v %v -- %v",
				namespace,
				primarySrc,
				checkQuery))
			Expect(strings.Trim(out, "\n"), err).To(BeEquivalentTo("2"))
			Expect(err).ToNot(HaveOccurred())
		})
	})
})
