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

package e2e

import (
	"fmt"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	clusterasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/cluster"
	pgasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/postgres"
	secretsasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/secrets"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/exec"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/yaml"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Bootstrap with pg_basebackup", Label(tests.LabelRecovery), func() {
	const (
		namespacePrefix = "cluster-pg-basebackup"
		srcCluster      = fixturesDir + "/pg_basebackup/cluster-src.yaml.template"
		dstClusterBasic = fixturesDir + "/pg_basebackup/cluster-dst-basic-auth.yaml.template"
		dstClusterTLS   = fixturesDir + "/pg_basebackup/cluster-dst-tls.yaml.template"
		dstClusterTbs   = fixturesDir + "/pg_basebackup/cluster-dst-tablespace-mapping.yaml.template"
		tableName       = "to_bootstrap"
		appUser         = "appuser"
		level           = tests.High
	)
	var namespace, srcClusterName string
	var err error
	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	Context("can bootstrap via pg_basebackup", Ordered, func() {
		BeforeAll(func() {
			// Create a cluster in a namespace we'll delete after the test
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			// Create the source Cluster
			srcClusterName, err = yaml.GetResourceNameFromYAML(env.Scheme, srcCluster)
			Expect(err).ToNot(HaveOccurred())
			clusterasserts.AssertCreateCluster(env, testTimeouts, namespace, srcClusterName, srcCluster)
			tableLocator := pgasserts.TableLocator{
				Namespace:    namespace,
				ClusterName:  srcClusterName,
				DatabaseName: postgres.AppDBName,
				TableName:    tableName,
			}
			pgasserts.AssertCreateTestData(env, tableLocator)
		})

		It("using basic authentication", func() {
			// Create the destination Cluster
			dstClusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, dstClusterBasic)
			Expect(err).ToNot(HaveOccurred())
			clusterasserts.AssertCreateCluster(env, testTimeouts, namespace, dstClusterName, dstClusterBasic)
			// We give more time than the usual 600s, since the recovery is slower
			clusterasserts.AssertClusterIsReady(env, namespace, dstClusterName, testTimeouts[timeouts.ClusterIsReadySlow])

			secretName := dstClusterName + apiv1.ApplicationUserSecretSuffix

			By("checking the dst cluster with auto generated app password connectable", func() {
				pgasserts.AssertApplicationDatabaseConnection(env, namespace, dstClusterName,
					appUser, postgres.AppDBName, "", secretName)
			})

			By("update user application password for dst cluster and verify connectivity", func() {
				const newPassword = "eeh2Zahohx"
				secretsasserts.AssertUpdateSecret(env, namespace, dstClusterName, secretName, "password", newPassword, 30)
				pgasserts.AssertApplicationDatabaseConnection(env,
					namespace,
					dstClusterName,
					appUser,
					postgres.AppDBName,
					newPassword,
					secretName)
			})

			By("checking data have been copied correctly", func() {
				tableLocator := pgasserts.TableLocator{
					Namespace:    namespace,
					ClusterName:  dstClusterName,
					DatabaseName: postgres.AppDBName,
					TableName:    tableName,
				}
				pgasserts.AssertDataExpectedCount(env, tableLocator, 2)
			})

			By("writing some new data to the dst cluster", func() {
				forward, conn, err := postgres.ForwardPSQLConnection(
					env.Ctx,
					env.Client,
					env.Interface,
					env.RestClientConfig,
					namespace,
					dstClusterName,
					postgres.AppDBName,
					apiv1.ApplicationUserSecretSuffix,
				)
				defer func() {
					_ = conn.Close()
					forward.Close()
				}()
				Expect(err).ToNot(HaveOccurred())
				pgasserts.InsertRecordIntoTable(tableName, 3, conn)
			})

			By("checking the src cluster was not modified", func() {
				tableLocator := pgasserts.TableLocator{
					Namespace:    namespace,
					ClusterName:  srcClusterName,
					DatabaseName: postgres.AppDBName,
					TableName:    tableName,
				}
				pgasserts.AssertDataExpectedCount(env, tableLocator, 2)
			})
		})

		It("using TLS authentication", func() {
			// Create the destination Cluster
			dstClusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, dstClusterTLS)
			Expect(err).ToNot(HaveOccurred())
			clusterasserts.AssertCreateCluster(env, testTimeouts, namespace, dstClusterName, dstClusterTLS)
			// We give more time than the usual 600s, since the recovery is slower
			clusterasserts.AssertClusterIsReady(env, namespace, dstClusterName, testTimeouts[timeouts.ClusterIsReadySlow])

			By("checking data have been copied correctly", func() {
				tableLocator := pgasserts.TableLocator{
					Namespace:    namespace,
					ClusterName:  dstClusterName,
					DatabaseName: postgres.AppDBName,
					TableName:    tableName,
				}
				pgasserts.AssertDataExpectedCount(env, tableLocator, 2)
			})

			By("writing some new data to the dst cluster", func() {
				forward, conn, err := postgres.ForwardPSQLConnection(
					env.Ctx,
					env.Client,
					env.Interface,
					env.RestClientConfig,
					namespace,
					dstClusterName,
					postgres.AppDBName,
					apiv1.ApplicationUserSecretSuffix,
				)
				defer func() {
					_ = conn.Close()
					forward.Close()
				}()
				Expect(err).ToNot(HaveOccurred())
				pgasserts.InsertRecordIntoTable(tableName, 3, conn)
			})

			By("checking the src cluster was not modified", func() {
				tableLocator := pgasserts.TableLocator{
					Namespace:    namespace,
					ClusterName:  srcClusterName,
					DatabaseName: postgres.AppDBName,
					TableName:    tableName,
				}
				pgasserts.AssertDataExpectedCount(env, tableLocator, 2)
			})
		})
	})

	Context("can relocate the tablespaces of the source", Ordered, func() {
		const (
			tablespaceName  = "moved"
			sourceLocation  = "/var/lib/postgresql/data/moved"
			targetLocation  = "/var/lib/postgresql/tablespaces/moved/data"
			tablespaceTable = "in_moved_tablespace"
		)

		BeforeAll(func() {
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			srcClusterName, err = yaml.GetResourceNameFromYAML(env.Scheme, srcCluster)
			Expect(err).ToNot(HaveOccurred())
			clusterasserts.AssertCreateCluster(env, testTimeouts, namespace, srcClusterName, srcCluster)

			By("creating a tablespace outside the CloudNativePG layout in the source", func() {
				primary, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, srcClusterName)
				Expect(err).ToNot(HaveOccurred())
				podLocator := exec.PodLocator{Namespace: namespace, PodName: primary.Name}

				_, _, err = exec.CommandInInstancePod(
					env.Ctx, env.Client, env.Interface, env.RestClientConfig,
					podLocator, nil, "mkdir", sourceLocation)
				Expect(err).ToNot(HaveOccurred())

				for _, query := range []string{
					fmt.Sprintf("CREATE TABLESPACE %s LOCATION '%s'", tablespaceName, sourceLocation),
					fmt.Sprintf("CREATE TABLE %s TABLESPACE %s AS SELECT generate_series(1, 10) AS id",
						tablespaceTable, tablespaceName),
				} {
					_, _, err = exec.QueryInInstancePod(
						env.Ctx, env.Client, env.Interface, env.RestClientConfig,
						podLocator, postgres.AppDBName, query)
					Expect(err).ToNot(HaveOccurred())
				}
			})
		})

		It("maps the tablespace to its volume in every instance", func() {
			dstClusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, dstClusterTbs)
			Expect(err).ToNot(HaveOccurred())
			clusterasserts.AssertCreateCluster(env, testTimeouts, namespace, dstClusterName, dstClusterTbs)
			clusterasserts.AssertClusterIsReady(env, namespace, dstClusterName, testTimeouts[timeouts.ClusterIsReadySlow])

			dstCluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, dstClusterName)
			Expect(err).ToNot(HaveOccurred())
			AssertDatabaseContainsTablespaces(dstCluster, testTimeouts[timeouts.Short])

			By("checking the location and the content of the tablespace in every instance", func() {
				pods, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, dstClusterName)
				Expect(err).ToNot(HaveOccurred())
				Expect(pods.Items).To(HaveLen(2))
				for _, pod := range pods.Items {
					podLocator := exec.PodLocator{Namespace: namespace, PodName: pod.Name}
					Eventually(func(g Gomega) {
						location, _, err := exec.QueryInInstancePod(
							env.Ctx, env.Client, env.Interface, env.RestClientConfig,
							podLocator, postgres.AppDBName,
							fmt.Sprintf("SELECT pg_catalog.pg_tablespace_location(oid) "+
								"FROM pg_catalog.pg_tablespace WHERE spcname = '%s'", tablespaceName))
						g.Expect(err).ToNot(HaveOccurred())
						g.Expect(location).To(Equal(targetLocation+"\n"), "pod %s", pod.Name)

						count, _, err := exec.QueryInInstancePod(
							env.Ctx, env.Client, env.Interface, env.RestClientConfig,
							podLocator, postgres.AppDBName,
							fmt.Sprintf("SELECT count(*) FROM %s", tablespaceTable))
						g.Expect(err).ToNot(HaveOccurred())
						g.Expect(count).To(Equal("10\n"), "pod %s", pod.Name)
					}, testTimeouts[timeouts.Short]).Should(Succeed())
				}
			})
		})
	})
})
