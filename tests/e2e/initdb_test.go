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
		fixturesInitdbDir = fixturesDir + "/initdb"
		level             = tests.Medium
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

		primary, err := clusterutils.GetClusterPrimary(env.Ctx, env.Client, namespace, clusterName)
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
			clusterName                        = "p-postinit-sql"
			postInitSQLCluster                 = fixturesInitdbDir + "/cluster-postinit-sql.yaml.template"
			postInitApplicationSQLSecretRef    = fixturesInitdbDir + "/cluster_post_init_application_secret.yaml"
			postInitApplicationSQLConfigMapRef = fixturesInitdbDir + "/cluster_post_init_application_configmap.yaml"
			postInitSQLSecretRef               = fixturesInitdbDir + "/cluster_post_init_secret.yaml"
			postInitSQLConfigMapRef            = fixturesInitdbDir + "/cluster_post_init_configmap.yaml"
			postInitTemplateSQLSecretRef       = fixturesInitdbDir + "/cluster_post_init_template_secret.yaml"
			postInitTemplateSQLConfigMapRef    = fixturesInitdbDir + "/cluster_post_init_template_configmap.yaml"
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
			CreateResourceFromFile(namespace, postInitApplicationSQLSecretRef)
			CreateResourceFromFile(namespace, postInitApplicationSQLConfigMapRef)
			CreateResourceFromFile(namespace, postInitTemplateSQLSecretRef)
			CreateResourceFromFile(namespace, postInitTemplateSQLConfigMapRef)

			AssertCreateCluster(namespace, clusterName, postInitSQLCluster, env)

			// Data defined by postInitSQL, postInitApplicationSQL and postInitTemplateSQL
			assertPostInitData(namespace, clusterName, "sql",
				"postgres", 10000)
			assertPostInitData(namespace, clusterName, "application_sql",
				"app", 10000)
			assertPostInitData(namespace, clusterName, "template_sql",
				"app", 10000)

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

			By("checking inside the database the default locale", func() {
				primary, err := clusterutils.GetClusterPrimary(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())

				stdout, _, err := exec.QueryInInstancePod(
					env.Ctx, env.Client, env.Interface, env.RestClientConfig,
					exec.PodLocator{
						Namespace: namespace,
						PodName:   primary.Name,
					}, "postgres",
					"select datcollate from pg_database where datname='template0'")
				Expect(err).ToNot(HaveOccurred())
				Expect(stdout, err).To(Equal("C\n"))
			})
		})
	})

	Context("custom default locale", func() {
		const (
			clusterName        = "p-locale"
			postInitSQLCluster = fixturesInitdbDir + "/cluster-custom-locale.yaml.template"
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
				primary, err := clusterutils.GetClusterPrimary(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())

				stdout, _, err := exec.QueryInInstancePod(
					env.Ctx, env.Client, env.Interface, env.RestClientConfig,
					exec.PodLocator{
						Namespace: namespace,
						PodName:   primary.Name,
					}, "postgres",
					"select datcollate from pg_database where datname='template0'")
				Expect(err).ToNot(HaveOccurred())
				Expect(stdout, err).To(Equal("en_US.utf8\n"))
			})
		})
	})
})
