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
	remoteClient "github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/webserver/client/remote"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/proxy"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/yaml"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = FDescribe("Operator mTLS authentication", Label(tests.LabelSecurity), func() {
	const (
		clusterFile     = fixturesDir + "/base/cluster-storage-class.yaml.template"
		namespacePrefix = "operator-mtls"
		level           = tests.High
	)

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	It("sets the operator certificate fingerprint and protects sensitive endpoints", func() {
		namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
		Expect(err).ToNot(HaveOccurred())

		clusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, clusterFile)
		Expect(err).ToNot(HaveOccurred())

		AssertCreateCluster(namespace, clusterName, clusterFile, env)

		By("verifying the operator certificate fingerprint is set in cluster status", func() {
			cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			Expect(cluster.Status.OperatorCertificateFingerprint).ToNot(BeEmpty())
		})

		By("verifying that unauthenticated access is rejected on protected endpoints", func() {
			podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			Expect(podList.Items).ToNot(BeEmpty())

			pod := podList.Items[0]
			tlsEnabled := remoteClient.GetStatusSchemeFromPod(&pod).IsHTTPS()

			// /pg/status is unauthenticated: proves the pod is reachable and the API
			// server proxy credentials are valid. If this fails, the next assertion
			// would be meaningless.
			By("confirming the unauthenticated status endpoint is reachable", func() {
				_, err := proxy.RetrievePgStatusFromInstance(env.Ctx, env.Interface, pod, tlsEnabled)
				Expect(err).ToNot(HaveOccurred())
			})

			// /pg/controldata is authenticated: a request without a client certificate
			// must be rejected. Since /pg/status succeeded above, any error here is
			// caused by our middleware, not by a network or credentials issue.
			By("confirming the authenticated pgcontroldata endpoint rejects unauthenticated access", func() {
				_, err := proxy.RetrievePgControlDataFromInstance(env.Ctx, env.Interface, pod, tlsEnabled)
				Expect(err).To(HaveOccurred())
			})
		})
	}, NodeTimeout(testTimeouts[timeouts.ClusterIsReady]))
})
