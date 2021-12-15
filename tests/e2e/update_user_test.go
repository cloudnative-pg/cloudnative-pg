/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package e2e

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clusterv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/tests"
)

var _ = Describe("Update user password", func() {
	const (
		namespace   = "cluster-update-user-password"
		sampleFile  = fixturesDir + "/base/cluster-basic.yaml"
		clusterName = "cluster-basic"
		level       = tests.Low
	)

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	JustAfterEach(func() {
		if CurrentSpecReport().Failed() {
			env.DumpClusterEnv(namespace, clusterName,
				"out/"+CurrentSpecReport().LeafNodeText+".log")
		}
	})
	AfterEach(func() {
		err := env.DeleteNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
	})

	It("can update the user application password", func() {
		const namespace = "cluster-update-user-password"
		var secret corev1.Secret
		// Create a cluster in a namespace we'll delete after the test
		err := env.CreateNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
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
			err = env.Client.Get(env.Ctx,
				client.ObjectKey{Namespace: namespace, Name: secretName},
				&secret)
			Expect(err).ToNot(HaveOccurred())

			// newSecret := secret
			secret.Data["password"] = []byte(newPassword)

			err = env.Client.Update(env.Ctx, &secret)
			Expect(err).ToNot(HaveOccurred())

			AssertConnection(rwService, "app", "app", newPassword, pod, 60, env)
		})

		By("update superuser password", func() {
			const secretName = clusterName + "-superuser"
			const newPassword = "fi6uCae7" //nolint:gosec
			err = env.Client.Get(env.Ctx,
				client.ObjectKey{Namespace: namespace, Name: secretName},
				&secret)
			Expect(err).ToNot(HaveOccurred())

			secret.Data["password"] = []byte(newPassword)

			err = env.Client.Update(env.Ctx, &secret)
			Expect(err).ToNot(HaveOccurred())

			AssertConnection(rwService, "postgres", "postgres", newPassword, pod, 60, env)
		})
	})
})

var _ = Describe("Disabling superuser password", func() {
	const namespace = "cluster-superuser-enable"
	const sampleFile = fixturesDir + "/base/cluster-basic.yaml"
	const clusterName = "cluster-basic"

	JustAfterEach(func() {
		if CurrentSpecReport().Failed() {
			env.DumpClusterEnv(namespace, clusterName,
				"out/"+CurrentSpecReport().LeafNodeText+".log")
		}
	})
	AfterEach(func() {
		err := env.DeleteNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
	})

	It("enable disable superuser access", func() {
		var secret corev1.Secret
		// Create a cluster in a namespace we'll delete after the test
		err := env.CreateNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
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
			err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
				err := env.Client.Get(env.Ctx, namespacedName, cluster)
				Expect(err).ToNot(HaveOccurred())
				falseValue := false
				cluster.Spec.EnableSuperuserAccess = &falseValue
				return env.Client.Update(env.Ctx, cluster)
			})
			Expect(err).ToNot(HaveOccurred())

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
				stdout, _, err := env.ExecCommand(env.Ctx, *pod, "postgres", &timeout,
					"psql", "-U", "postgres", "-tAc",
					"SELECT rolpassword IS NULL FROM pg_authid WHERE rolname='postgres'")
				if err != nil {
					return ""
				}
				return stdout
			}, 60).Should(Equal("t\n"))

			// Setting to true, so we have a secret and a new password
			err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
				err := env.Client.Get(env.Ctx, namespacedName, cluster)
				Expect(err).ToNot(HaveOccurred())
				trueValue := true
				cluster.Spec.EnableSuperuserAccess = &trueValue
				return env.Client.Update(env.Ctx, cluster)
			})
			Expect(err).ToNot(HaveOccurred())

			Eventually(func() error {
				err = env.Client.Get(env.Ctx,
					client.ObjectKey{Namespace: namespace, Name: secretName},
					&secret)
				return err
			}, 60).Should(Not(HaveOccurred()))

			rwService := fmt.Sprintf("%v-rw.%v.svc", clusterName, namespace)
			password := string(secret.Data["password"])

			AssertConnection(rwService, "postgres", "postgres", password, *pod, 60, env)
		})
	})
})

var _ = Describe("Creating a cluster without superuser password", func() {
	const namespace = "no-postgres-pwd"
	const sampleFile = fixturesDir + "/secrets/cluster-no-postgres-pwd.yaml"
	const clusterName = "cluster-no-postgres-pwd"

	JustAfterEach(func() {
		if CurrentSpecReport().Failed() {
			env.DumpClusterEnv(namespace, clusterName,
				"out/"+CurrentSpecReport().LeafNodeText+".log")
		}
	})
	AfterEach(func() {
		err := env.DeleteNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
	})

	It("create a cluster without postgres password", func() {
		var secret corev1.Secret
		var cluster clusterv1.Cluster
		const secretName = clusterName + "-superuser"

		// Create a cluster in a namespace we'll delete after the test
		err := env.CreateNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
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
			err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
				err := env.Client.Get(env.Ctx, namespacedName, &cluster)
				Expect(err).ToNot(HaveOccurred())
				trueValue := true
				cluster.Spec.EnableSuperuserAccess = &trueValue
				return env.Client.Update(env.Ctx, &cluster)
			})
			Expect(err).ToNot(HaveOccurred())
		})

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
				stdout, _, err := env.ExecCommand(env.Ctx, *pod, "postgres", &timeout,
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
