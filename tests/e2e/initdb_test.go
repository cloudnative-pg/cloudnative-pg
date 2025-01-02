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

	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/exec"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// - spinning up a cluster with some post-init-sql query and verifying that they are really executed

// Set of tests in which we check that the initdb options are really applied
var _ = Describe("InitDB settings", Label(tests.LabelSmoke, tests.LabelBasic), func() {
	const (
		fixturesCertificatesDir = fixturesDir + "/initdb"
		level                   = tests.Medium
	)

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	assertPostInitData := func(
		namespace,
		clusterName,
		tableName string,
		dbName exec.DatabaseName,
		expectedCount int,
	) {
		query := fmt.Sprintf("SELECT count(*) FROM %s", tableName)

		primary, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())

		By(fmt.Sprintf(
			"querying the %s table in the %s database defined by postInit SQL",
			tableName, dbName), func() {
			stdout, _, err := exec.QueryInInstancePod(
				env.Ctx, env.Client, env.Interface, env.RestClientConfig,
				exec.PodLocator{
					Namespace: namespace,
					PodName:   primary.Name,
				}, dbName,
				query)
			Expect(err).ToNot(HaveOccurred())
			nRows, atoiErr := strconv.Atoi(strings.Trim(stdout, "\n"))
			Expect(nRows, atoiErr).To(BeEquivalentTo(expectedCount))
		})
	}

	Context("initdb custom post-init SQL scripts", func() {
		const (
			clusterName             = "p-postinit-sql"
			postInitSQLCluster      = fixturesCertificatesDir + "/cluster-postinit-sql.yaml.template"
			postInitSQLSecretRef    = fixturesCertificatesDir + "/cluster_post_init_secret.yaml"
			postInitSQLConfigMapRef = fixturesCertificatesDir + "/cluster_post_init_configmap.yaml"
		)

		var namespace string

		It("can find the tables created by the post-init SQL queries", func() {
			// Create a cluster in a namespace we'll delete after the test
			const namespacePrefix = "initdb-postqueries"
			var err error
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			CreateResourceFromFile(namespace, postInitSQLSecretRef)
			CreateResourceFromFile(namespace, postInitSQLConfigMapRef)

			AssertCreateCluster(namespace, clusterName, postInitSQLCluster, env)

			primaryDst := clusterName + "-1"

			// Data defined by postInitSQLRefs, postInitApplicationSQLRefs and postInitTemplateSQLRefs
			// via secret
			assertPostInitData(namespace, clusterName, "secrets",
				"postgres", 10000)
			assertPostInitData(namespace, clusterName, "application_secrets",
				"app", 10000)
			assertPostInitData(namespace, clusterName, "template_secrets",
				"app", 10000)

			// Data defined by postInitSQLRefs, postInitApplicationSQLRefs and postInitTemplateSQLRefs
			// via configmap
			assertPostInitData(namespace, clusterName, "configmaps",
				"postgres", 10000)
			assertPostInitData(namespace, clusterName, "application_configmaps",
				"app", 10000)
			assertPostInitData(namespace, clusterName, "template_configmaps",
				"app", 10000)

			By("querying the database to ensure the installed extension is there", func() {
				stdout, _, err := exec.QueryInInstancePod(
					env.Ctx, env.Client, env.Interface, env.RestClientConfig,
					exec.PodLocator{
						Namespace: namespace,
						PodName:   primaryDst,
					}, exec.DatabaseName("postgres"),
					"SELECT count(*) FROM pg_available_extensions WHERE name LIKE 'intarray'")
				Expect(err).ToNot(HaveOccurred())
				Expect(stdout, err).To(Equal("1\n"))
			})
			By("checking inside the database the default locale", func() {
				stdout, _, err := exec.QueryInInstancePod(
					env.Ctx, env.Client, env.Interface, env.RestClientConfig,
					exec.PodLocator{
						Namespace: namespace,
						PodName:   primaryDst,
					}, exec.DatabaseName("postgres"),
					"select datcollate from pg_database where datname='template0'")
				Expect(err).ToNot(HaveOccurred())
				Expect(stdout, err).To(Equal("C\n"))
			})
		})
	})

	Context("custom default locale", func() {
		const (
			clusterName        = "p-locale"
			postInitSQLCluster = fixturesCertificatesDir + "/cluster-custom-locale.yaml.template"
		)

		var namespace string

		It("use the custom default locale specified", func() {
			// Create a cluster in a namespace we'll delete after the test
			const namespacePrefix = "initdb-locale"
			var err error
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			AssertCreateCluster(namespace, clusterName, postInitSQLCluster, env)

			By("checking inside the database", func() {
				primary, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())

				stdout, _, err := exec.QueryInInstancePod(
					env.Ctx, env.Client, env.Interface, env.RestClientConfig,
					exec.PodLocator{
						Namespace: namespace,
						PodName:   primary.Name,
					}, exec.DatabaseName("postgres"),
					"select datcollate from pg_database where datname='template0'")
				Expect(err).ToNot(HaveOccurred())
				Expect(stdout, err).To(Equal("en_US.utf8\n"))
			})
		})
	})
})
