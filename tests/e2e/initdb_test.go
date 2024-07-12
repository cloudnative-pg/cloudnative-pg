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
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils"

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
			namespace, err = env.CreateUniqueNamespace(namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(func() error {
				if CurrentSpecReport().Failed() {
					env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
				}
				return env.DeleteNamespace(namespace)
			})

			CreateResourceFromFile(namespace, postInitSQLSecretRef)
			CreateResourceFromFile(namespace, postInitSQLConfigMapRef)
			CreateResourceFromFile(namespace, postInitApplicationSQLSecretRef)
			CreateResourceFromFile(namespace, postInitApplicationSQLConfigMapRef)
			CreateResourceFromFile(namespace, postInitTemplateSQLSecretRef)
			CreateResourceFromFile(namespace, postInitTemplateSQLConfigMapRef)

			AssertCreateCluster(namespace, clusterName, postInitSQLCluster, env)

			primaryDst := clusterName + "-1"

			By("querying the postgres database tables defined by SQL", func() {
				_, _, err := env.ExecQueryInInstancePod(
					utils.PodLocator{
						Namespace: namespace,
						PodName:   primaryDst,
					}, "postgres",
					"SELECT count(*) FROM sql")
				Expect(err).ToNot(HaveOccurred())
			})

			By("querying the app database tables defined by SQL", func() {
				_, _, err := env.ExecQueryInInstancePod(
					utils.PodLocator{
						Namespace: namespace,
						PodName:   primaryDst,
					}, "app",
					"SELECT count(*) FROM application_sql")
				Expect(err).ToNot(HaveOccurred())
			})

			By("querying the app database tables from the template defined by SQL", func() {
				_, _, err := env.ExecQueryInInstancePod(
					utils.PodLocator{
						Namespace: namespace,
						PodName:   primaryDst,
					}, "app",
					"SELECT count(*) FROM template_sql")
				Expect(err).ToNot(HaveOccurred())
			})

			By("querying the postgres database tables defined by secretRefs", func() {
				_, _, err := env.ExecQueryInInstancePod(
					utils.PodLocator{
						Namespace: namespace,
						PodName:   primaryDst,
					}, "postgres",
					"SELECT count(*) FROM secrets")
				Expect(err).ToNot(HaveOccurred())
			})

			By("querying the postgres database tables defined by configMapRefs", func() {
				_, _, err := env.ExecQueryInInstancePod(
					utils.PodLocator{
						Namespace: namespace,
						PodName:   primaryDst,
					}, "postgres",
					"SELECT count(*) FROM configmaps")
				Expect(err).ToNot(HaveOccurred())
			})

			By("querying the app database tables defined by secretRefs", func() {
				_, _, err := env.ExecQueryInInstancePod(
					utils.PodLocator{
						Namespace: namespace,
						PodName:   primaryDst,
					}, "app",
					"SELECT count(*) FROM application_secrets")
				Expect(err).ToNot(HaveOccurred())
			})

			By("querying the app database tables defined by configMapRefs", func() {
				_, _, err := env.ExecQueryInInstancePod(
					utils.PodLocator{
						Namespace: namespace,
						PodName:   primaryDst,
					}, "app",
					"SELECT count(*) FROM application_configmaps")
				Expect(err).ToNot(HaveOccurred())
			})

			By("querying the app database tables from the template defined by secretRefs", func() {
				_, _, err := env.ExecQueryInInstancePod(
					utils.PodLocator{
						Namespace: namespace,
						PodName:   primaryDst,
					}, "app",
					"SELECT count(*) FROM template_secrets")
				Expect(err).ToNot(HaveOccurred())
			})

			By("querying the app database tables from the template defined by configMapRefs", func() {
				_, _, err := env.ExecQueryInInstancePod(
					utils.PodLocator{
						Namespace: namespace,
						PodName:   primaryDst,
					}, "app",
					"SELECT count(*) FROM template_configmaps")
				Expect(err).ToNot(HaveOccurred())
			})

			By("checking inside the database the default locale", func() {
				stdout, _, err := env.ExecQueryInInstancePod(
					utils.PodLocator{
						Namespace: namespace,
						PodName:   primaryDst,
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
			namespace, err = env.CreateUniqueNamespace(namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(func() error {
				if CurrentSpecReport().Failed() {
					env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
				}
				return env.DeleteNamespace(namespace)
			})
			AssertCreateCluster(namespace, clusterName, postInitSQLCluster, env)

			primaryDst := clusterName + "-1"

			By("checking inside the database", func() {
				stdout, _, err := env.ExecQueryInInstancePod(
					utils.PodLocator{
						Namespace: namespace,
						PodName:   primaryDst,
					}, utils.DatabaseName("postgres"),
					"select datcollate from pg_database where datname='template0'")
				Expect(err).ToNot(HaveOccurred())
				Expect(stdout, err).To(Equal("en_US.utf8\n"))
			})
		})
	})
})
