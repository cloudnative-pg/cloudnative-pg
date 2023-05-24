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

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clusterv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Update user and superuser password", Label(tests.LabelServiceConnectivity), func() {
	const (
		namespacePrefix = "cluster-update-user-password"
		sampleFile      = fixturesDir + "/base/cluster-basic.yaml"
		clusterName     = "cluster-basic"
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
		namespace, err = env.CreateUniqueNamespace(namespacePrefix)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() error {
			if CurrentSpecReport().Failed() {
				env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
			}
			return env.DeleteNamespace(namespace)
		})
		AssertCreateCluster(namespace, clusterName, sampleFile, env)

		rwService := fmt.Sprintf("%v-rw.%v.svc", clusterName, namespace)

		// we use a pod in the cluster to have a psql client ready and
		// internal access to the k8s cluster
		podName := clusterName + "-1"
		pod := corev1.Pod{}
		namespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      podName,
		}
		err = env.Client.Get(env.Ctx, namespacedName, &pod)
		Expect(err).ToNot(HaveOccurred())

		By("update user application password", func() {
			const secretName = clusterName + "-app"
			const newPassword = "eeh2Zahohx" //nolint:gosec

			AssertUpdateSecret("password", newPassword, secretName, namespace, clusterName, 30, env)
			AssertConnection(rwService, "app", "app", newPassword, *psqlClientPod, 60, env)
		})

		By("fail updating user application password with wrong user in secret", func() {
			const secretName = clusterName + "-app"
			const newUser = "postgres"
			const newPassword = "newpassword"

			AssertUpdateSecret("password", newPassword, secretName, namespace, clusterName, 30, env)
			AssertUpdateSecret("username", newUser, secretName, namespace, clusterName, 30, env)

			dsn := fmt.Sprintf("host=%v user=%v dbname=%v password=%v sslmode=require",
				rwService, newUser, "app", newPassword)
			timeout := time.Second * 10
			_, _, err := utils.ExecCommand(env.Ctx, env.Interface, env.RestClientConfig,
				pod, specs.PostgresContainerName, &timeout,
				"psql", dsn, "-tAc", "SELECT 1")
			Expect(err).ToNot(BeNil())

			AssertUpdateSecret("username", "app", secretName, namespace, clusterName, 30, env)
		})

		By("update superuser password", func() {
			const secretName = clusterName + "-superuser"
			const newPassword = "fi6uCae7" //nolint:gosec
			AssertUpdateSecret("password", newPassword, secretName, namespace, clusterName, 30, env)
			AssertConnection(rwService, "postgres", "postgres", newPassword, *psqlClientPod, 60, env)
		})
	})
})

var _ = Describe("Disabling superuser password", Label(tests.LabelServiceConnectivity), func() {
	const (
		namespacePrefix = "cluster-superuser-enable"
		sampleFile      = fixturesDir + "/base/cluster-basic.yaml"
		clusterName     = "cluster-basic"
		level           = tests.Low
	)
	var namespace string
	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	It("enable disable superuser access", func() {
		var secret corev1.Secret
		var err error
		// Create a cluster in a namespace we'll delete after the test
		namespace, err = env.CreateUniqueNamespace(namespacePrefix)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() error {
			if CurrentSpecReport().Failed() {
				env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
			}
			return env.DeleteNamespace(namespace)
		})
		AssertCreateCluster(namespace, clusterName, sampleFile, env)
		// we use a pod in the cluster to have a psql client ready and
		// internal access to the k8s cluster
		namespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      clusterName,
		}

		By("disable superuser access", func() {
			const secretName = clusterName + "-superuser"
			cluster := &clusterv1.Cluster{}
			err := env.Client.Get(env.Ctx, namespacedName, cluster)
			Expect(err).ToNot(HaveOccurred())

			Eventually(func() error {
				err = env.Client.Get(env.Ctx,
					client.ObjectKey{Namespace: namespace, Name: secretName},
					&secret)
				return err
			}, 60).Should(Not(HaveOccurred()))

			// Setting to false, now we should not have a secret or password
			Eventually(func() error {
				err := env.Client.Get(env.Ctx, namespacedName, cluster)
				if err != nil {
					return err
				}
				falseValue := false
				cluster.Spec.EnableSuperuserAccess = &falseValue
				return env.Client.Update(env.Ctx, cluster)
			}, 60, 5).Should(BeNil())

			Eventually(func() bool {
				err = env.Client.Get(env.Ctx,
					client.ObjectKey{Namespace: namespace, Name: secretName},
					&secret)
				if err == nil {
					GinkgoWriter.Printf("Secret %v in namespace %v still exists\n", secretName, namespace)
					return false
				}
				secretNotFound := apierrors.IsNotFound(err)
				if !secretNotFound {
					GinkgoWriter.Printf("Error reported is %s\n", err.Error())
				}
				return secretNotFound
			}, 90).WithPolling(time.Second).Should(BeEquivalentTo(true))

			// We test that the password was set to null in pod 1
			pod, err := env.GetClusterPrimary(namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			timeout := time.Second * 10
			// We should have the `postgres` user with a null password
			Eventually(func() string {
				stdout, _, err := env.ExecCommand(env.Ctx, *pod, specs.PostgresContainerName, &timeout,
					"psql", "-U", "postgres", "-tAc",
					"SELECT rolpassword IS NULL FROM pg_authid WHERE rolname='postgres'")
				if err != nil {
					return ""
				}
				return stdout
			}, 60).Should(Equal("t\n"))

			// Setting to true, so we have a secret and a new password
			Eventually(func() error {
				err := env.Client.Get(env.Ctx, namespacedName, cluster)
				if err != nil {
					return err
				}
				trueValue := true
				cluster.Spec.EnableSuperuserAccess = &trueValue
				return env.Client.Update(env.Ctx, cluster)
			}, 60, 5).Should(BeNil())

			Eventually(func() error {
				err = env.Client.Get(env.Ctx,
					client.ObjectKey{Namespace: namespace, Name: secretName},
					&secret)
				return err
			}, 60).Should(Not(HaveOccurred()))

			rwService := fmt.Sprintf("%v-rw.%v.svc", clusterName, namespace)
			password := string(secret.Data["password"])
			AssertConnection(rwService, "postgres", "postgres", password, *psqlClientPod, 60, env)
		})
	})
})

var _ = Describe("Creating a cluster without superuser password", Label(tests.LabelServiceConnectivity), func() {
	const (
		namespacePrefix = "no-postgres-pwd"
		sampleFile      = fixturesDir + "/secrets/cluster-no-postgres-pwd.yaml.template"
		clusterName     = "cluster-no-postgres-pwd"
		level           = tests.Low
	)
	var namespace string
	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	It("create a cluster without postgres password", func() {
		var secret corev1.Secret
		var cluster clusterv1.Cluster
		var err error
		const secretName = clusterName + "-superuser"

		// Create a cluster in a namespace we'll delete after the test
		namespace, err = env.CreateUniqueNamespace(namespacePrefix)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() error {
			if CurrentSpecReport().Failed() {
				env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
			}
			return env.DeleteNamespace(namespace)
		})
		AssertCreateCluster(namespace, clusterName, sampleFile, env)
		// we use a pod in the cluster to have a psql client ready and
		// internal access to the k8s cluster
		namespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      clusterName,
		}

		By("ensuring no superuser secret is generated", func() {
			err := env.Client.Get(env.Ctx, namespacedName, &cluster)
			Expect(err).ToNot(HaveOccurred())

			Eventually(func() bool {
				err = env.Client.Get(env.Ctx,
					client.ObjectKey{Namespace: namespace, Name: secretName},
					&secret)
				return apierrors.IsNotFound(err)
			}, 60).Should(BeTrue())
		})

		By("enabling superuser access", func() {
			// Setting to true, now we should have a secret with the password
			Eventually(func() error {
				err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
					err := env.Client.Get(env.Ctx, namespacedName, &cluster)
					Expect(err).ToNot(HaveOccurred())
					trueValue := true
					cluster.Spec.EnableSuperuserAccess = &trueValue
					err = env.Client.Update(env.Ctx, &cluster)
					if err != nil {
						return err
					}
					return nil
				})
				return err
			}, 60, 5).Should(BeNil())

			By("waiting for the superuser secret to be created", func() {
				Eventually(func() error {
					err = env.Client.Get(env.Ctx,
						client.ObjectKey{Namespace: namespace, Name: secretName},
						&secret)
					return err
				}, 60).Should(Not(HaveOccurred()))
			})

			By("verifying that the password is really set", func() {
				// We test that the password is set in pod 1
				pod, err := env.GetClusterPrimary(namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				timeout := time.Second * 10
				// We should have the `postgres` user with a null password
				Eventually(func() string {
					stdout, _, err := env.ExecCommand(env.Ctx, *pod, specs.PostgresContainerName, &timeout,
						"psql", "-U", "postgres", "-tAc",
						"SELECT rolpassword IS NULL FROM pg_authid WHERE rolname='postgres'")
					if err != nil {
						return ""
					}
					return stdout
				}, 60).Should(Equal("f\n"))
			})
		})
	})
})
