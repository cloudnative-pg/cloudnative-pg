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

	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
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
			tableName       = "test"
		)
		var (
			sourceClusterName, destinationClusterName, namespace string
			databaseObjectName, pubObjectName, subObjectName     string
			pub                                                  *apiv1.Publication
			sub                                                  *apiv1.Subscription
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

		assertCreateDatabase := func(namespace, clusterName, databaseManifest, databaseName string) {
			databaseObjectName, err = env.GetResourceNameFromYAML(databaseManifest)
			Expect(err).NotTo(HaveOccurred())

			By(fmt.Sprintf("applying the %s Database CRD manifest", databaseObjectName), func() {
				CreateResourceFromFile(namespace, databaseManifest)
			})

			By(fmt.Sprintf("ensuring the %s Database CRD succeeded reconciliation", databaseObjectName), func() {
				databaseObject := &apiv1.Database{}
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

			By(fmt.Sprintf("verifying the %s database has been created", databaseName), func() {
				primaryPodInfo, err := env.GetClusterPrimary(namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())

				AssertDatabaseExists(primaryPodInfo, databaseName, true)
			})
		}

		assertPublicationExists := func(namespace, primaryPod string, pub *apiv1.Publication) {
			query := fmt.Sprintf("select count(*) from pg_publication where pubname = '%s'",
				pub.Spec.Name)
			Eventually(func(g Gomega) {
				stdout, _, err := env.ExecQueryInInstancePod(
					testUtils.PodLocator{
						Namespace: namespace,
						PodName:   primaryPod,
					},
					dbname,
					query)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(stdout).Should(ContainSubstring("1"), "expected publication not found")
			}, 30).Should(Succeed())
		}

		assertSubscriptionExists := func(namespace, primaryPod string, sub *apiv1.Subscription) {
			query := fmt.Sprintf("select count(*) from pg_subscription where subname = '%s'",
				sub.Spec.Name)
			Eventually(func(g Gomega) {
				stdout, _, err := env.ExecQueryInInstancePod(
					testUtils.PodLocator{
						Namespace: namespace,
						PodName:   primaryPod,
					},
					dbname,
					query)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(stdout).Should(ContainSubstring("1"), "expected subscription not found")
			}, 30).Should(Succeed())
		}

		It("can perform logical replication", func() {
			assertCreateDatabase(namespace, sourceClusterName, sourceDatabaseManifest, dbname)

			tableLocator := TableLocator{
				Namespace:    namespace,
				ClusterName:  sourceClusterName,
				DatabaseName: dbname,
				TableName:    tableName,
			}
			AssertCreateTestData(env, tableLocator)

			assertCreateDatabase(namespace, destinationClusterName, destinationDatabaseManifest, dbname)

			By("creating an empty table inside the destination database", func() {
				query := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %v (column1 int) ;", tableName)
				_, err = testUtils.RunExecOverForward(env, namespace, destinationClusterName, dbname,
					apiv1.ApplicationUserSecretSuffix, query)
				Expect(err).ToNot(HaveOccurred())
			})

			By("applying Publication CRD manifest", func() {
				CreateResourceFromFile(namespace, pubManifest)
				pubObjectName, err = env.GetResourceNameFromYAML(pubManifest)
				Expect(err).NotTo(HaveOccurred())
			})

			By("ensuring the Publication CRD succeeded reconciliation", func() {
				// get publication object
				pub = &apiv1.Publication{}
				pubNamespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      pubObjectName,
				}

				Eventually(func(g Gomega) {
					err := env.Client.Get(env.Ctx, pubNamespacedName, pub)
					Expect(err).ToNot(HaveOccurred())
					g.Expect(pub.Status.Ready).Should(BeTrue())
				}, 300).WithPolling(10 * time.Second).Should(Succeed())
			})

			By("verifying new publication has been created", func() {
				primaryPodInfo, err := env.GetClusterPrimary(namespace, sourceClusterName)
				Expect(err).ToNot(HaveOccurred())

				assertPublicationExists(namespace, primaryPodInfo.Name, pub)
			})

			By("applying Subscription CRD manifest", func() {
				CreateResourceFromFile(namespace, subManifest)
				subObjectName, err = env.GetResourceNameFromYAML(subManifest)
				Expect(err).NotTo(HaveOccurred())
			})

			By("ensuring the Subscription CRD succeeded reconciliation", func() {
				// get subscription object
				sub = &apiv1.Subscription{}
				pubNamespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      subObjectName,
				}

				Eventually(func(g Gomega) {
					err := env.Client.Get(env.Ctx, pubNamespacedName, sub)
					Expect(err).ToNot(HaveOccurred())
					g.Expect(sub.Status.Ready).Should(BeTrue())
				}, 300).WithPolling(10 * time.Second).Should(Succeed())
			})

			By("verifying new subscription has been created", func() {
				primaryPodInfo, err := env.GetClusterPrimary(namespace, destinationClusterName)
				Expect(err).ToNot(HaveOccurred())

				assertSubscriptionExists(namespace, primaryPodInfo.Name, sub)
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

			// TODO: remove once finalizers cleanup is handled by the operator
			deleteObjectWithFinalizer := func(object client.Object, finalizerName string) error {
				if err := testUtils.DeleteObject(env, object); err != nil {
					return err
				}

				updatedObj := object.DeepCopyObject().(client.Object)
				controllerutil.RemoveFinalizer(updatedObj, finalizerName)
				if err := env.Client.Patch(env.Ctx, updatedObj, client.MergeFrom(object)); err != nil {
					if apierrs.IsNotFound(err) {
						return nil
					}
					return err
				}

				return nil
			}

			err = deleteObjectWithFinalizer(pub, utils.PublicationFinalizerName)
			Expect(err).ToNot(HaveOccurred())

			err = deleteObjectWithFinalizer(sub, utils.SubscriptionFinalizerName)
			Expect(err).ToNot(HaveOccurred())
		})
	})
})
