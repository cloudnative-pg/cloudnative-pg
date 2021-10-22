/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package e2e

import (
	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/specs/pgbouncer"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"
	"github.com/EnterpriseDB/cloud-native-postgresql/tests"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("PGBouncer Connections", func() {
	const (
		sampleFile                    = fixturesDir + "/pgbouncer/cluster-pgbouncer.yaml"
		poolerBasicAuthRWSampleFile   = fixturesDir + "/pgbouncer/pgbouncer-pooler-basic-auth-rw.yaml"
		poolerCertificateRWSampleFile = fixturesDir + "/pgbouncer/pgbouncer-pooler-tls-rw.yaml"
		poolerBasicAuthROSampleFile   = fixturesDir + "/pgbouncer/pgbouncer-pooler-basic-auth-ro.yaml"
		poolerCertificateROSampleFile = fixturesDir + "/pgbouncer/pgbouncer-pooler-tls-ro.yaml"
		level                         = tests.Low
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

	It("can connect to Postgres via pgbouncer service using basic auth", func() {
		// Create a cluster in a namespace we'll delete after the test
		namespace = "pgbouncer-basic-auth"
		err := env.CreateNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
		clusterName, err = env.GetResourceNameFromYAML(sampleFile)
		Expect(err).ToNot(HaveOccurred())
		AssertCreateCluster(namespace, clusterName, sampleFile, env)

		By("setting up read write type pgbouncer pooler", func() {
			assertPgBouncerPoolerIsSetUp(namespace, poolerBasicAuthRWSampleFile)
		})

		By("setting up read only type pgbouncer pooler", func() {
			assertPgBouncerPoolerIsSetUp(namespace, poolerBasicAuthROSampleFile)
		})

		By("verifying read and write connections using pgbouncer service", func() {
			assertReadWriteConnectionUsingPgBouncerService(namespace, clusterName,
				poolerBasicAuthRWSampleFile, true)
		})

		By("verifying read connections using pgbouncer service", func() {
			assertReadWriteConnectionUsingPgBouncerService(namespace, clusterName,
				poolerBasicAuthROSampleFile, false)
		})
	})

	It("can connect to Postgres via pgbouncer service using tls certificates", func() {
		// Create a cluster in a namespace we'll delete after the test
		namespace = "pgbouncer-certificate"
		err := env.CreateNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
		clusterName, err = env.GetResourceNameFromYAML(sampleFile)
		Expect(err).ToNot(HaveOccurred())
		AssertCreateCluster(namespace, clusterName, sampleFile, env)

		By("setting up read write type pgbouncer pooler", func() {
			assertPgBouncerPoolerIsSetUp(namespace, poolerCertificateRWSampleFile)
		})

		By("setting up read only type pgbouncer pooler", func() {
			assertPgBouncerPoolerIsSetUp(namespace, poolerCertificateROSampleFile)
		})

		By("verifying read and write connections using pgbouncer service", func() {
			assertReadWriteConnectionUsingPgBouncerService(namespace, clusterName,
				poolerCertificateRWSampleFile, true)
		})

		By("verifying read connections using pgbouncer service", func() {
			assertReadWriteConnectionUsingPgBouncerService(namespace, clusterName,
				poolerCertificateROSampleFile, false)
		})
	})
})

func assertPgBouncerPoolerIsSetUp(namespace, poolerSampleFile string) {
	_, _, err := tests.Run("kubectl create -n " + namespace + " -f " + poolerSampleFile)
	Expect(err).ToNot(HaveOccurred())

	Eventually(func() (int32, error) {
		deployment, err := getPgBouncerDeployment(namespace, poolerSampleFile)
		return deployment.Status.ReadyReplicas, err
	}, 300).Should(BeEquivalentTo(1))

	// check pooler pod is up and running
	Eventually(func() (bool, error) {
		podList, err := getPGBouncerPodList(namespace, poolerSampleFile)
		if err != nil {
			return false, err
		}
		if len(podList.Items) == 1 {
			return utils.IsPodActive(podList.Items[0]) && utils.IsPodReady(podList.Items[0]), err
		}
		return false, err
	}, 90).Should(BeTrue())
}

func assertReadWriteConnectionUsingPgBouncerService(namespace, clusterName, poolerSampleFile string, poolerRW bool) {
	poolerServiceName, err := env.GetResourceNameFromYAML(poolerSampleFile)
	Expect(err).ToNot(HaveOccurred())
	podName := clusterName + "-1"
	pod := &corev1.Pod{}
	namespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      podName,
	}
	err = env.Client.Get(env.Ctx, namespacedName, pod)
	Expect(err).ToNot(HaveOccurred())

	// Get the app user password from the -app secret
	appSecretName := clusterName + "-app"
	appSecret := &corev1.Secret{}
	appSecretNamespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      appSecretName,
	}
	err = env.Client.Get(env.Ctx, appSecretNamespacedName, appSecret)
	Expect(err).ToNot(HaveOccurred())
	generatedAppUserPassword := string(appSecret.Data["password"])
	AssertConnection(poolerServiceName, "app", "app", generatedAppUserPassword, *pod, 120, env)

	// verify that, if pooler type setup read write then it will allow both read and
	// write operations or if pooler type setup read only then it will allow only read operations
	if poolerRW {
		AssertWritesToPrimarySucceeds(pod, poolerServiceName, "app", "app",
			generatedAppUserPassword)
	} else {
		AssertWritesToReplicaFails(pod, poolerServiceName, "app", "app",
			generatedAppUserPassword)
	}
}

// getPGBouncerPodList gather the pgbouncer pod list
func getPGBouncerPodList(namespace, poolerSampleFile string) (*corev1.PodList, error) {
	poolerName, err := env.GetResourceNameFromYAML(poolerSampleFile)
	Expect(err).ToNot(HaveOccurred())

	podList := &corev1.PodList{}
	err = env.Client.List(env.Ctx, podList, client.InNamespace(namespace),
		client.MatchingLabels{pgbouncer.PgbouncerNameLabel: poolerName})
	if err != nil {
		return &corev1.PodList{}, err
	}
	return podList, err
}

// getPgBouncerDeployment gather the pgbouncer deployment info
func getPgBouncerDeployment(namespace, poolerSampleFile string) (*appsv1.Deployment, error) {
	poolerName, err := env.GetResourceNameFromYAML(poolerSampleFile)
	Expect(err).ToNot(HaveOccurred())
	// Wait for the deployment to be ready
	deploymentNamespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      poolerName,
	}
	deployment := &appsv1.Deployment{}
	err = env.Client.Get(env.Ctx, deploymentNamespacedName, deployment)

	if err != nil {
		return &appsv1.Deployment{}, err
	}

	return deployment, nil
}
