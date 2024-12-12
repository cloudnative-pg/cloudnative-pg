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
	"time"

	"k8s.io/apimachinery/pkg/types"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	testUtils "github.com/cloudnative-pg/cloudnative-pg/tests/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// - spinning up a cluster, apply a declarative publication/subscription on it

// Set of tests in which we use the declarative publication and subscription CRDs on an existing cluster
var _ = Describe("Publication and Subscription", Label(tests.LabelDeclarativePubSub), func() {
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
			namespace, err = env.CreateUniqueTestNamespace(namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			sourceClusterName, err = env.GetResourceNameFromYAML(sourceClusterManifest)
			Expect(err).ToNot(HaveOccurred())

			destinationClusterName, err = env.GetResourceNameFromYAML(destinationClusterManifest)
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
			destPrimaryPod, err := env.GetClusterPrimary(namespace, destinationClusterName)
			Expect(err).ToNot(HaveOccurred())
			_, _, err = env.EventuallyExecQueryInInstancePod(
				testUtils.PodLocator{
					Namespace: destPrimaryPod.Namespace,
					PodName:   destPrimaryPod.Name,
				},
				dbname,
				fmt.Sprintf("DROP SUBSCRIPTION IF EXISTS %s", subName),
				RetryTimeout,
				PollingTime,
			)
			Expect(err).ToNot(HaveOccurred())

			sourcePrimaryPod, err := env.GetClusterPrimary(namespace, sourceClusterName)
			Expect(err).ToNot(HaveOccurred())
			_, _, err = env.EventuallyExecQueryInInstancePod(
				testUtils.PodLocator{
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
			AssertDatabaseExists(destPrimaryPod, dbname, false)
			AssertDatabaseExists(sourcePrimaryPod, dbname, false)
		})

		assertPublicationExists := func(namespace, clusterName string, pub *apiv1.Publication, expectedValue bool) {
			primaryPodInfo, err := env.GetClusterPrimary(namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())

			query := fmt.Sprintf("select count(*) from pg_publication where pubname = '%s'",
				pub.Spec.Name)
			Eventually(func(g Gomega) {
				stdout, _, err := env.ExecQueryInInstancePod(
					testUtils.PodLocator{
						Namespace: primaryPodInfo.Namespace,
						PodName:   primaryPodInfo.Name,
					},
					testUtils.DatabaseName(pub.Spec.DBName),
					query)
				g.Expect(err).ToNot(HaveOccurred())
				if expectedValue {
					g.Expect(stdout).To(ContainSubstring("1"), "expected a publication to be found")
				} else {
					g.Expect(stdout).To(ContainSubstring("0"), "expected a publication to be present")
				}
			}, 30).Should(Succeed())
		}

		assertSubscriptionExists := func(namespace, clusterName string, sub *apiv1.Subscription, expectedValue bool) {
			primaryPodInfo, err := env.GetClusterPrimary(namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())

			query := fmt.Sprintf("select count(*) from pg_subscription where subname = '%s'",
				sub.Spec.Name)
			Eventually(func(g Gomega) {
				stdout, _, err := env.ExecQueryInInstancePod(
					testUtils.PodLocator{
						Namespace: primaryPodInfo.Namespace,
						PodName:   primaryPodInfo.Name,
					},
					testUtils.DatabaseName(sub.Spec.DBName),
					query)
				g.Expect(err).ToNot(HaveOccurred())
				if expectedValue {
					g.Expect(stdout).To(ContainSubstring("1"), "expected a subscription to be found")
				} else {
					g.Expect(stdout).To(ContainSubstring("0"), "expected a subscription to be present")
				}
			}, 30).Should(Succeed())
		}

		assertCreateDatabase := func(namespace, clusterName, databaseManifest string) {
			databaseObject := &apiv1.Database{}
			databaseObjectName, err := env.GetResourceNameFromYAML(databaseManifest)
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
					Expect(err).ToNot(HaveOccurred())
					g.Expect(databaseObject.Status.Applied).Should(HaveValue(BeTrue()))
				}, 300).WithPolling(10 * time.Second).Should(Succeed())
			})

			By(fmt.Sprintf("verifying the %s database has been created", databaseObject.Spec.Name), func() {
				primaryPodInfo, err := env.GetClusterPrimary(namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())

				AssertDatabaseExists(primaryPodInfo, databaseObject.Spec.Name, true)
			})
		}

		// nolint:dupl
		assertCreatePublication := func(namespace, clusterName, publicationManifest string) {
			pub := &apiv1.Publication{}
			pubObjectName, err := env.GetResourceNameFromYAML(publicationManifest)
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
					err := env.Client.Get(env.Ctx, pubNamespacedName, pub)
					Expect(err).ToNot(HaveOccurred())
					g.Expect(pub.Status.Applied).Should(HaveValue(BeTrue()))
				}, 300).WithPolling(10 * time.Second).Should(Succeed())
			})

			By("verifying new publication has been created", func() {
				assertPublicationExists(namespace, clusterName, pub, true)
			})
		}

		// nolint:dupl
		assertCreateSubscription := func(namespace, clusterName, subscriptionManifest string) {
			sub := &apiv1.Subscription{}
			subObjectName, err := env.GetResourceNameFromYAML(subscriptionManifest)
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
					err := env.Client.Get(env.Ctx, pubNamespacedName, sub)
					Expect(err).ToNot(HaveOccurred())
					g.Expect(sub.Status.Applied).Should(HaveValue(BeTrue()))
				}, 300).WithPolling(10 * time.Second).Should(Succeed())
			})

			By("verifying new subscription has been created", func() {
				assertSubscriptionExists(namespace, clusterName, sub, true)
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
				_, err = testUtils.RunExecOverForward(env, namespace, destinationClusterName, dbname,
					apiv1.ApplicationUserSecretSuffix, query)
				Expect(err).ToNot(HaveOccurred())
			})

			assertCreatePublication(namespace, sourceClusterName, pubManifest)
			assertCreateSubscription(namespace, destinationClusterName, subManifest)

			var (
				publication  *apiv1.Publication
				subscription *apiv1.Subscription
			)
			By("setting the reclaimPolicy", func() {
				publicationReclaimPolicy := apiv1.PublicationReclaimDelete
				subscriptionReclaimPolicy := apiv1.SubscriptionReclaimDelete
				if retainOnDeletion {
					publicationReclaimPolicy = apiv1.PublicationReclaimRetain
					subscriptionReclaimPolicy = apiv1.SubscriptionReclaimRetain

				}
				// Get the object names
				pubObjectName, err := env.GetResourceNameFromYAML(pubManifest)
				Expect(err).NotTo(HaveOccurred())
				subObjectName, err := env.GetResourceNameFromYAML(subManifest)
				Expect(err).NotTo(HaveOccurred())

				Eventually(func(g Gomega) {
					publication, err = testUtils.GetPublicationObject(namespace, pubObjectName, env)
					g.Expect(err).ToNot(HaveOccurred())
					publication.Spec.ReclaimPolicy = publicationReclaimPolicy
					err = env.Client.Update(env.Ctx, publication)
					g.Expect(err).ToNot(HaveOccurred())

					subscription, err = testUtils.GetSubscriptionObject(namespace, subObjectName, env)
					g.Expect(err).ToNot(HaveOccurred())
					subscription.Spec.ReclaimPolicy = subscriptionReclaimPolicy
					err = env.Client.Update(env.Ctx, subscription)
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
				Expect(testUtils.DeleteObject(env, publication)).To(Succeed())
				Expect(testUtils.DeleteObject(env, subscription)).To(Succeed())
			})

			By("verifying the retention policy in the postgres database", func() {
				assertPublicationExists(namespace, sourceClusterName, publication, retainOnDeletion)
				assertSubscriptionExists(namespace, destinationClusterName, subscription, retainOnDeletion)
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
