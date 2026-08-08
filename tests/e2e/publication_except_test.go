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
	clusterasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/cluster"
	pgasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/internal/resources"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/yaml"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// - spinning up a cluster and applying a declarative Publication whose
//   target combines `allTables` with `except`, a PostgreSQL 19+ feature,
//   then verifying that PostgreSQL excludes the listed tables while still
//   publishing every other table.

var _ = Describe("Publication with target.except", Label(tests.LabelPublicationSubscription), func() {
	const (
		clusterManifest     = fixturesDir + "/publication_except/cluster.yaml.template"
		publicationManifest = fixturesDir + "/publication_except/pub-except.yaml"
		level               = tests.Medium
		namespacePrefix     = "publication-except"
		dbname              = "app"
		pubName             = "pub-except"
		includedTable       = "included_table"
		excludedTable       = "excluded_table"
	)

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
		if env.PostgresVersion < 19 {
			Skip("This test requires PostgreSQL 19 or greater (target.except EXCEPT TABLE clause)")
		}
	})

	It("excludes the configured tables from a FOR ALL TABLES publication", func() {
		namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
		Expect(err).ToNot(HaveOccurred())

		clusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, clusterManifest)
		Expect(err).ToNot(HaveOccurred())

		clusterasserts.AssertCreateCluster(env, testTimeouts, namespace, clusterName, clusterManifest)

		By("creating the tables referenced by the publication", func() {
			for _, table := range []string{includedTable, excludedTable} {
				query := fmt.Sprintf("CREATE TABLE %s (column1 int)", table)
				_, err := postgres.RunExecOverForward(
					env.Ctx, env.Client, env.Interface, env.RestClientConfig,
					namespace, clusterName, dbname,
					apiv1.ApplicationUserSecretSuffix, query,
				)
				Expect(err).ToNot(HaveOccurred())
			}
		})

		pubObjectName, err := yaml.GetResourceNameFromYAML(env.Scheme, publicationManifest)
		Expect(err).ToNot(HaveOccurred())

		By("applying the Publication CRD manifest", func() {
			resources.CreateResourceFromFile(env, namespace, publicationManifest)
		})

		By("ensuring the Publication CRD succeeded reconciliation", func() {
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

		By("verifying the excluded table is not part of the publication", func() {
			primaryPodInfo, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())

			Eventually(pgasserts.QueryMatchExpectationPredicate(env, primaryPodInfo, dbname,
				publicationTableExistsQuery(pubName, excludedTable), "f"), 30).Should(Succeed())
		})

		By("verifying the included table is part of the publication", func() {
			primaryPodInfo, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())

			Eventually(pgasserts.QueryMatchExpectationPredicate(env, primaryPodInfo, dbname,
				publicationTableExistsQuery(pubName, includedTable), "t"), 30).Should(Succeed())
		})
	})
})

func publicationTableExistsQuery(pubName, tableName string) string {
	return fmt.Sprintf(
		"SELECT EXISTS(SELECT 1 FROM pg_catalog.pg_publication_tables WHERE pubname='%s' AND tablename='%s')",
		pubName, tableName,
	)
}
