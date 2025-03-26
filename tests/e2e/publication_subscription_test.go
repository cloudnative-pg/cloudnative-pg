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
	"time"

	"k8s.io/apimachinery/pkg/types"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/exec"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/objects"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/yaml"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// - spinning up a cluster, apply a declarative publication/subscription on it

// Set of tests in which we use the declarative publication and subscription CRDs on an existing cluster
var _ = Describe("Publication and Subscription", Label(tests.LabelPublicationSubscription), func() {
	const (
		sourceClusterManifest       = fixturesDir + "/declarative_pub_sub/source-cluster.yaml.template"
		destinationClusterManifest  = fixturesDir + "/declarative_pub_sub/destination-cluster.yaml.template"
		sourceDatabaseManifest      = fixturesDir + "/declarative_pub_sub/source-database.yaml"
		destinationDatabaseManifest = fixturesDir + "/declarative_pub_sub/destination-database.yaml"
		pubManifest                 = fixturesDir + "/declarative_pub_sub/pub.yaml"
		subManifest                 = fixturesDir + "/declarative_pub_sub/sub.yaml"
		level                       = tests.Medium
	)

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	Context("in a plain vanilla cluster", Ordered, func() {
		const (
			namespacePrefix = "declarative-pub-sub"
			dbname          = "declarative"
			subName         = "sub"
			pubName         = "pub"
			tableName       = "test"
		)
		var (
			sourceClusterName, destinationClusterName, namespace string
			err                                                  error
		)

		BeforeAll(func() {
			// Create a cluster in a namespace we'll delete after the test
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			sourceClusterName, err = yaml.GetResourceNameFromYAML(env.Scheme, sourceClusterManifest)
			Expect(err).ToNot(HaveOccurred())

			destinationClusterName, err = yaml.GetResourceNameFromYAML(env.Scheme, destinationClusterManifest)
			Expect(err).ToNot(HaveOccurred())

			By("setting up source cluster", func() {
				AssertCreateCluster(namespace, sourceClusterName, sourceClusterManifest, env)
			})

			By("setting up destination cluster", func() {
				AssertCreateCluster(namespace, destinationClusterName, destinationClusterManifest, env)
			})
		})

		AfterEach(func() {
			// We want to reuse the same source and destination Cluster, so
			// we need to drop each Postgres object that has been created.
			// We need to make sure that publication/subscription have been removed before
			// attempting to drop the database, otherwise the DROP DATABASE will fail because
			// there's an  active logical replication slot.
			destPrimaryPod, err := clusterutils.GetPrimary(
				env.Ctx, env.Client,
				namespace, destinationClusterName,
			)
			Expect(err).ToNot(HaveOccurred())
			_, _, err = exec.EventuallyExecQueryInInstancePod(
				env.Ctx, env.Client, env.Interface, env.RestClientConfig,
				exec.PodLocator{
					Namespace: destPrimaryPod.Namespace,
					PodName:   destPrimaryPod.Name,
				},
				dbname,
				fmt.Sprintf("DROP SUBSCRIPTION IF EXISTS %s", subName),
				RetryTimeout,
				PollingTime,
			)
			Expect(err).ToNot(HaveOccurred())

			sourcePrimaryPod, err := clusterutils.GetPrimary(
				env.Ctx, env.Client,
				namespace, sourceClusterName,
			)
			Expect(err).ToNot(HaveOccurred())
			_, _, err = exec.EventuallyExecQueryInInstancePod(
				env.Ctx, env.Client, env.Interface, env.RestClientConfig,
				exec.PodLocator{
					Namespace: sourcePrimaryPod.Namespace,
					PodName:   sourcePrimaryPod.Name,
				},
				dbname,
				fmt.Sprintf("DROP PUBLICATION IF EXISTS %s", pubName),
				RetryTimeout,
				PollingTime,
			)
			Expect(err).ToNot(HaveOccurred())

			Expect(DeleteResourcesFromFile(namespace, destinationDatabaseManifest)).To(Succeed())
			Expect(DeleteResourcesFromFile(namespace, sourceDatabaseManifest)).To(Succeed())
			Eventually(QueryMatchExpectationPredicate(sourcePrimaryPod, postgres.PostgresDBName,
				databaseExistsQuery(dbname), "f"), 30).Should(Succeed())
			Eventually(QueryMatchExpectationPredicate(destPrimaryPod, postgres.PostgresDBName,
				databaseExistsQuery(dbname), "f"), 30).Should(Succeed())
		})

		assertCreateDatabase := func(namespace, clusterName, databaseManifest string) {
			databaseObject := &apiv1.Database{}
			databaseObjectName, err := yaml.GetResourceNameFromYAML(env.Scheme, databaseManifest)
			Expect(err).NotTo(HaveOccurred())

			By(fmt.Sprintf("applying the %s Database CRD manifest", databaseObjectName), func() {
				CreateResourceFromFile(namespace, databaseManifest)
			})

			By(fmt.Sprintf("ensuring the %s Database CRD succeeded reconciliation", databaseObjectName), func() {
				databaseNamespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      databaseObjectName,
				}

				Eventually(func(g Gomega) {
					err := env.Client.Get(env.Ctx, databaseNamespacedName, databaseObject)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(databaseObject.Status.Applied).Should(HaveValue(BeTrue()))
				}, 300).WithPolling(10 * time.Second).Should(Succeed())
			})

			By(fmt.Sprintf("verifying the %s database has been created", databaseObject.Spec.Name), func() {
				primaryPodInfo, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())

				Eventually(QueryMatchExpectationPredicate(primaryPodInfo, postgres.PostgresDBName,
					databaseExistsQuery(databaseObject.Spec.Name), "t"), 30).Should(Succeed())
			})
		}

		// nolint:dupl
		assertCreatePublication := func(namespace, clusterName, publicationManifest string) {
			pubObjectName, err := yaml.GetResourceNameFromYAML(env.Scheme, publicationManifest)
			Expect(err).NotTo(HaveOccurred())

			By("applying Publication CRD manifest", func() {
				CreateResourceFromFile(namespace, publicationManifest)
			})

			By("ensuring the Publication CRD succeeded reconciliation", func() {
				// get publication object
				pubNamespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      pubObjectName,
				}

				Eventually(func(g Gomega) {
					pub := &apiv1.Publication{}
					err := env.Client.Get(env.Ctx, pubNamespacedName, pub)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(pub.Status.Applied).Should(HaveValue(BeTrue()))
				}, 300).WithPolling(10 * time.Second).Should(Succeed())
			})

			By("verifying new publication has been created", func() {
				primaryPodInfo, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())

				Eventually(QueryMatchExpectationPredicate(primaryPodInfo, dbname,
					publicationExistsQuery(pubName), "t"), 30).Should(Succeed())
			})
		}

		// nolint:dupl
		assertCreateSubscription := func(namespace, clusterName, subscriptionManifest string) {
			subObjectName, err := yaml.GetResourceNameFromYAML(env.Scheme, subscriptionManifest)
			Expect(err).NotTo(HaveOccurred())

			By("applying Subscription CRD manifest", func() {
				CreateResourceFromFile(namespace, subscriptionManifest)
			})

			By("ensuring the Subscription CRD succeeded reconciliation", func() {
				// get subscription object
				pubNamespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      subObjectName,
				}

				Eventually(func(g Gomega) {
					sub := &apiv1.Subscription{}
					err := env.Client.Get(env.Ctx, pubNamespacedName, sub)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(sub.Status.Applied).Should(HaveValue(BeTrue()))
				}, 300).WithPolling(10 * time.Second).Should(Succeed())
			})

			By("verifying new subscription has been created", func() {
				primaryPodInfo, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())

				Eventually(QueryMatchExpectationPredicate(primaryPodInfo, dbname,
					subscriptionExistsQuery(subName), "t"), 30).Should(Succeed())
			})
		}

		assertTestPubSub := func(retainOnDeletion bool) {
			assertCreateDatabase(namespace, sourceClusterName, sourceDatabaseManifest)

			tableLocator := TableLocator{
				Namespace:    namespace,
				ClusterName:  sourceClusterName,
				DatabaseName: dbname,
				TableName:    tableName,
			}
			AssertCreateTestData(env, tableLocator)

			assertCreateDatabase(namespace, destinationClusterName, destinationDatabaseManifest)

			By("creating an empty table inside the destination database", func() {
				query := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %v (column1 int) ;", tableName)
				_, err = postgres.RunExecOverForward(
					env.Ctx, env.Client, env.Interface, env.RestClientConfig,
					namespace, destinationClusterName, dbname,
					apiv1.ApplicationUserSecretSuffix, query,
				)
				Expect(err).ToNot(HaveOccurred())
			})

			assertCreatePublication(namespace, sourceClusterName, pubManifest)
			assertCreateSubscription(namespace, destinationClusterName, subManifest)

			var (
				publication  apiv1.Publication
				subscription apiv1.Subscription
			)
			By("setting the reclaimPolicy", func() {
				publicationReclaimPolicy := apiv1.PublicationReclaimDelete
				subscriptionReclaimPolicy := apiv1.SubscriptionReclaimDelete
				if retainOnDeletion {
					publicationReclaimPolicy = apiv1.PublicationReclaimRetain
					subscriptionReclaimPolicy = apiv1.SubscriptionReclaimRetain
				}
				// Get the object names
				pubObjectName, err := yaml.GetResourceNameFromYAML(env.Scheme, pubManifest)
				Expect(err).NotTo(HaveOccurred())
				subObjectName, err := yaml.GetResourceNameFromYAML(env.Scheme, subManifest)
				Expect(err).NotTo(HaveOccurred())

				Eventually(func(g Gomega) {
					err = objects.Get(
						env.Ctx, env.Client,
						types.NamespacedName{Namespace: namespace, Name: pubObjectName},
						&publication,
					)
					g.Expect(err).ToNot(HaveOccurred())
					publication.Spec.ReclaimPolicy = publicationReclaimPolicy
					err = env.Client.Update(env.Ctx, &publication)
					g.Expect(err).ToNot(HaveOccurred())

					err = objects.Get(
						env.Ctx, env.Client,
						types.NamespacedName{Namespace: namespace, Name: subObjectName},
						&subscription,
					)
					g.Expect(err).ToNot(HaveOccurred())
					subscription.Spec.ReclaimPolicy = subscriptionReclaimPolicy
					err = env.Client.Update(env.Ctx, &subscription)
					g.Expect(err).ToNot(HaveOccurred())
				}, 60, 5).Should(Succeed())
			})

			By("checking that the data is present inside the destination cluster database", func() {
				tableLocator := TableLocator{
					Namespace:    namespace,
					ClusterName:  destinationClusterName,
					DatabaseName: dbname,
					TableName:    tableName,
				}
				AssertDataExpectedCount(env, tableLocator, 2)
			})

			By("removing the objects", func() {
				Expect(objects.Delete(env.Ctx, env.Client, &publication)).To(Succeed())
				Expect(objects.Delete(env.Ctx, env.Client, &subscription)).To(Succeed())
			})

			By("verifying the publication reclaim policy outcome", func() {
				primaryPodInfo, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, sourceClusterName)
				Expect(err).ToNot(HaveOccurred())

				Eventually(QueryMatchExpectationPredicate(primaryPodInfo, dbname,
					publicationExistsQuery(pubName), boolPGOutput(retainOnDeletion)), 30).Should(Succeed())
			})

			By("verifying the subscription reclaim policy outcome", func() {
				primaryPodInfo, err := clusterutils.GetPrimary(
					env.Ctx, env.Client,
					namespace, destinationClusterName,
				)
				Expect(err).ToNot(HaveOccurred())

				Eventually(QueryMatchExpectationPredicate(primaryPodInfo, dbname,
					subscriptionExistsQuery(subName), boolPGOutput(retainOnDeletion)), 30).Should(Succeed())
			})
		}

		When("Reclaim policy is set to delete", func() {
			It("can manage Publication and Subscription and delete them in Postgres", func() {
				assertTestPubSub(false)
			})
		})

		When("Reclaim policy is set to retain", func() {
			It("can manage Publication and Subscription and release it", func() {
				assertTestPubSub(true)
			})
		})
	})
})

func publicationExistsQuery(pubName string) string {
	return fmt.Sprintf("SELECT EXISTS(SELECT 1 FROM pg_catalog.pg_publication WHERE pubname='%s')", pubName)
}

func subscriptionExistsQuery(subName string) string {
	return fmt.Sprintf("SELECT EXISTS(SELECT 1 FROM pg_catalog.pg_subscription WHERE subname='%s')", subName)
}
