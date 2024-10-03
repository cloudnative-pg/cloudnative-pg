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
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	testsUtils "github.com/cloudnative-pg/cloudnative-pg/tests/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Update user and superuser password", Label(tests.LabelServiceConnectivity), func() {
	const (
		namespacePrefix = "cluster-update-user-password"
		sampleFile      = fixturesDir + "/secrets/cluster-secrets.yaml"
		level           = tests.Low
	)
	var namespace string

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	It("can update the user application password", func() {
		var err error
		// Create a cluster in a namespace we'll delete after the test
		namespace, err = env.CreateUniqueTestNamespace(namespacePrefix)
		Expect(err).ToNot(HaveOccurred())
		clusterName, err := env.GetResourceNameFromYAML(sampleFile)
		Expect(err).ToNot(HaveOccurred())
		AssertCreateCluster(namespace, clusterName, sampleFile, env)

		host, err := testsUtils.GetHostName(namespace, clusterName, env)
		Expect(err).ToNot(HaveOccurred())

		appSecretName := clusterName + apiv1.ApplicationUserSecretSuffix
		superUserSecretName := clusterName + apiv1.SuperUserSecretSuffix

		primaryPod, err := env.GetClusterPrimary(namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())

		By("update user application password", func() {
			const newPassword = "eeh2Zahohx" //nolint:gosec

			AssertUpdateSecret("password", newPassword, appSecretName, namespace, clusterName, 30, env)
			AssertConnection(host, testsUtils.AppUser, testsUtils.AppDBName, newPassword, primaryPod, 60, env)
		})

		By("fail updating user application password with wrong user in secret", func() {
			const newUser = "postgres"
			const newPassword = "newpassword"

			AssertUpdateSecret("password", newPassword, appSecretName, namespace, clusterName, 30, env)
			AssertUpdateSecret("username", newUser, appSecretName, namespace, clusterName, 30, env)

			timeout := time.Second * 10
			dsn := testsUtils.CreateDSN(host, newUser, testsUtils.AppDBName, newPassword, testsUtils.Require, 5432)

			_, _, err := env.ExecCommand(env.Ctx, *primaryPod,
				specs.PostgresContainerName, &timeout,
				"psql", dsn, "-tAc", "SELECT 1")
			Expect(err).To(HaveOccurred())

			// Revert the username change
			AssertUpdateSecret("username", testsUtils.AppUser, appSecretName, namespace, clusterName, 30, env)
		})

		By("update superuser password", func() {
			// Setting EnableSuperuserAccess to true
			Eventually(func() error {
				cluster, err := env.GetCluster(namespace, clusterName)
				Expect(err).NotTo(HaveOccurred())
				cluster.Spec.EnableSuperuserAccess = ptr.To(true)
				return env.Client.Update(env.Ctx, cluster)
			}, 60, 5).Should(Not(HaveOccurred()))

			// We should now have a secret
			var secret corev1.Secret
			namespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      superUserSecretName,
			}
			Eventually(func(g Gomega) {
				err = env.Client.Get(env.Ctx, namespacedName, &secret)
				g.Expect(err).ToNot(HaveOccurred())
			}, 60).Should(Succeed())

			const newPassword = "fi6uCae7" //nolint:gosec
			AssertUpdateSecret("password", newPassword, superUserSecretName, namespace, clusterName, 30, env)
			AssertConnection(host, testsUtils.PostgresUser, testsUtils.PostgresDBName, newPassword, primaryPod, 60, env)
		})
	})
})

var _ = Describe("Enable superuser password", Label(tests.LabelServiceConnectivity), func() {
	const (
		namespacePrefix = "cluster-superuser-enable"
		sampleFile      = fixturesDir + "/secrets/cluster-secrets.yaml"
		level           = tests.Low
	)
	var namespace string

	It("enable and disable superuser access", func() {
		var err error
		// Create a cluster in a namespace we'll delete after the test
		namespace, err = env.CreateUniqueTestNamespace(namespacePrefix)
		Expect(err).ToNot(HaveOccurred())
		clusterName, err := env.GetResourceNameFromYAML(sampleFile)
		Expect(err).ToNot(HaveOccurred())
		AssertCreateCluster(namespace, clusterName, sampleFile, env)

		secretName := clusterName + apiv1.SuperUserSecretSuffix
		var secret corev1.Secret
		namespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      secretName,
		}

		primaryPod, err := env.GetClusterPrimary(namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())

		By("ensure superuser access is disabled by default", func() {
			Eventually(func(g Gomega) {
				err = env.Client.Get(env.Ctx, namespacedName, &secret)
				g.Expect(err).To(HaveOccurred())
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}, 200).Should(Succeed())

			timeout := time.Second * 10

			// We should have the `postgres` user with a null password
			Eventually(func() string {
				stdout, _, err := env.ExecCommand(env.Ctx, *primaryPod, specs.PostgresContainerName, &timeout,
					"psql", "-U", "postgres", "-tAc",
					"SELECT rolpassword IS NULL FROM pg_authid WHERE rolname='postgres'")
				if err != nil {
					return ""
				}
				return stdout
			}, 60).Should(Equal("t\n"))
		})

		By("enable superuser access", func() {
			// Setting EnableSuperuserAccess to true
			Eventually(func() error {
				cluster, err := env.GetCluster(namespace, clusterName)
				Expect(err).NotTo(HaveOccurred())
				cluster.Spec.EnableSuperuserAccess = ptr.To(true)
				return env.Client.Update(env.Ctx, cluster)
			}, 60, 5).Should(Not(HaveOccurred()))

			// We should now have a secret
			Eventually(func(g Gomega) {
				err = env.Client.Get(env.Ctx, namespacedName, &secret)
				g.Expect(err).ToNot(HaveOccurred())
			}, 90).WithPolling(time.Second).Should(Succeed())

			host, err := testsUtils.GetHostName(namespace, clusterName, env)
			Expect(err).ToNot(HaveOccurred())
			superUser, superUserPass, err := testsUtils.GetCredentials(clusterName, namespace,
				apiv1.SuperUserSecretSuffix, env)
			Expect(err).ToNot(HaveOccurred())
			AssertConnection(host, superUser, testsUtils.PostgresDBName, superUserPass, primaryPod, 60, env)
		})

		By("disable superuser access", func() {
			// Setting EnableSuperuserAccess to false
			Eventually(func() error {
				cluster, err := env.GetCluster(namespace, clusterName)
				Expect(err).NotTo(HaveOccurred())
				cluster.Spec.EnableSuperuserAccess = ptr.To(false)
				return env.Client.Update(env.Ctx, cluster)
			}, 60, 5).Should(Not(HaveOccurred()))

			// We expect the secret to eventually be deleted
			Eventually(func(g Gomega) {
				err = env.Client.Get(env.Ctx, namespacedName, &secret)
				g.Expect(err).To(HaveOccurred())
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}, 60).Should(Succeed())
		})
	})
})
