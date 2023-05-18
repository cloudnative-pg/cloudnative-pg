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

	corev1 "k8s.io/api/core/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs/pgbouncer"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PGBouncer Types", Ordered, Label(tests.LabelServiceConnectivity), func() {
	const (
		sampleFile                    = fixturesDir + "/pgbouncer/cluster-pgbouncer.yaml.template"
		poolerCertificateRWSampleFile = fixturesDir + "/pgbouncer/pgbouncer_types/pgbouncer-pooler-rw.yaml"
		poolerCertificateROSampleFile = fixturesDir + "/pgbouncer/pgbouncer_types/pgbouncer-pooler-ro.yaml"
		level                         = tests.Low
		poolerResourceNameRW          = "pooler-connection-rw"
		poolerResourceNameRO          = "pooler-connection-ro"
		poolerServiceRW               = "cluster-pgbouncer-rw"
		poolerServiceRO               = "cluster-pgbouncer-ro"
	)
	var err error
	var namespace, clusterName string

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	JustAfterEach(func() {
		if CurrentSpecReport().Failed() {
			env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
		}
	})

	BeforeAll(func() {
		// Create a cluster in a namespace we'll delete after the test
		// This cluster will be shared by the next tests
		namespace, err = env.CreateUniqueNamespace("pgbouncer-types")
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() error {
			return env.DeleteNamespace(namespace)
		})
		clusterName, err = env.GetResourceNameFromYAML(sampleFile)
		Expect(err).ToNot(HaveOccurred())
		AssertCreateCluster(namespace, clusterName, sampleFile, env)
	})

	It("should have proper service ip and host details for ro and rw with default installation", func() {
		By(fmt.Sprintf("setting up read write type pgbouncer pooler in %s", namespace), func() {
			createAndAssertPgBouncerPoolerIsSetUp(namespace, poolerCertificateRWSampleFile, 2)
		})

		By("setting up read only type pgbouncer pooler", func() {
			createAndAssertPgBouncerPoolerIsSetUp(namespace, poolerCertificateROSampleFile, 2)
		})

		By("verify that read-only pooler endpoints contain the correct pod addresses", func() {
			assertPGBouncerEndpointsContainsPodsIP(namespace, poolerCertificateROSampleFile, 2)
		})

		By("verify that read-only pooler pgbouncer.ini contains the correct host service", func() {
			poolerName, err := env.GetResourceNameFromYAML(poolerCertificateROSampleFile)
			Expect(err).ToNot(HaveOccurred())
			podList := &corev1.PodList{}
			err = env.Client.List(env.Ctx, podList, ctrlclient.InNamespace(namespace),
				ctrlclient.MatchingLabels{pgbouncer.PgbouncerNameLabel: poolerName})
			Expect(err).ToNot(HaveOccurred())

			assertPGBouncerHasServiceNameInsideHostParameter(namespace, poolerServiceRO, podList)
		})

		By("verify that read-write pooler endpoints contain the correct pod addresses.", func() {
			assertPGBouncerEndpointsContainsPodsIP(namespace, poolerCertificateRWSampleFile, 2)
		})

		By("verify that read-write pooler pgbouncer.ini contains the correct host service", func() {
			poolerName, err := env.GetResourceNameFromYAML(poolerCertificateRWSampleFile)
			Expect(err).ToNot(HaveOccurred())
			podList := &corev1.PodList{}
			err = env.Client.List(env.Ctx, podList, ctrlclient.InNamespace(namespace),
				ctrlclient.MatchingLabels{pgbouncer.PgbouncerNameLabel: poolerName})
			Expect(err).ToNot(HaveOccurred())

			assertPGBouncerHasServiceNameInsideHostParameter(namespace, poolerServiceRW, podList)
		})
	})

	scalingTest := func(instances int) func() {
		return func() {
			By(fmt.Sprintf("scaling PGBouncer to %v instances", instances), func() {
				command := fmt.Sprintf("kubectl scale pooler %s -n %s --replicas=%v",
					poolerResourceNameRO, namespace, instances)
				_, _, err := utils.Run(command)
				Expect(err).ToNot(HaveOccurred())

				// verifying if PGBouncer pooler pods are ready after scale up
				assertPGBouncerPodsAreReady(namespace, poolerCertificateROSampleFile, instances)

				// // scale up command for 3 replicas for read write
				command = fmt.Sprintf("kubectl scale pooler %s -n %s --replicas=%v",
					poolerResourceNameRW, namespace, instances)
				_, _, err = utils.Run(command)
				Expect(err).ToNot(HaveOccurred())

				// verifying if PGBouncer pooler pods are ready after scale up
				assertPGBouncerPodsAreReady(namespace, poolerCertificateRWSampleFile, instances)
			})

			By("verifying that read-only pooler endpoints contain the correct pod addresses", func() {
				assertPGBouncerEndpointsContainsPodsIP(namespace, poolerCertificateROSampleFile, instances)
			})

			By("verifying that read-only pooler pgbouncer.ini contains the correct host service", func() {
				poolerName, err := env.GetResourceNameFromYAML(poolerCertificateROSampleFile)
				Expect(err).ToNot(HaveOccurred())
				podList := &corev1.PodList{}
				err = env.Client.List(env.Ctx, podList, ctrlclient.InNamespace(namespace),
					ctrlclient.MatchingLabels{pgbouncer.PgbouncerNameLabel: poolerName})
				Expect(err).ToNot(HaveOccurred())

				assertPGBouncerHasServiceNameInsideHostParameter(namespace, poolerServiceRO, podList)
			})

			By("verifying that read-write pooler endpoints contain the correct pod addresses.", func() {
				assertPGBouncerEndpointsContainsPodsIP(namespace, poolerCertificateRWSampleFile, instances)
			})

			By("verifying that read-write pooler pgbouncer.ini contains the correct host service", func() {
				poolerName, err := env.GetResourceNameFromYAML(poolerCertificateRWSampleFile)
				Expect(err).ToNot(HaveOccurred())
				podList := &corev1.PodList{}
				err = env.Client.List(env.Ctx, podList, ctrlclient.InNamespace(namespace),
					ctrlclient.MatchingLabels{pgbouncer.PgbouncerNameLabel: poolerName})
				Expect(err).ToNot(HaveOccurred())
				assertPGBouncerHasServiceNameInsideHostParameter(namespace, poolerServiceRW, podList)
			})
		}
	}

	It("has proper service ip and host details for ro and rw scaling up", scalingTest(3))
	It("has proper service ip and host details for ro and rw scaling down", scalingTest(1))
})
