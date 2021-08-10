/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package e2e

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Update user password", func() {
	const namespace = "cluster-update-user-password"
	const sampleFile = fixturesDir + "/base/cluster-basic.yaml"
	const clusterName = "cluster-basic"

	JustAfterEach(func() {
		if CurrentGinkgoTestDescription().Failed {
			env.DumpClusterEnv(namespace, clusterName,
				"out/"+CurrentGinkgoTestDescription().TestText+".log")
		}
	})
	AfterEach(func() {
		err := env.DeleteNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
	})

	It("can update the user application password", func() {
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
