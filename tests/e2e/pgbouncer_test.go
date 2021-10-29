/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package e2e

import (
	"fmt"

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
			env.DumpPoolerResourcesInfo(namespace, CurrentSpecReport().LeafNodeText)
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
			createAndAssertPgBouncerPoolerIsSetUp(namespace, poolerBasicAuthRWSampleFile, 1)
		})

		By("setting up read only type pgbouncer pooler", func() {
			createAndAssertPgBouncerPoolerIsSetUp(namespace, poolerBasicAuthROSampleFile, 1)
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
			createAndAssertPgBouncerPoolerIsSetUp(namespace, poolerCertificateRWSampleFile, 1)
		})

		By("setting up read only type pgbouncer pooler", func() {
			createAndAssertPgBouncerPoolerIsSetUp(namespace, poolerCertificateROSampleFile, 1)
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

var _ = Describe("PgBouncer Pooler Resources", func() {
	const (
		sampleFile                  = fixturesDir + "/pgbouncer/cluster-pgbouncer.yaml"
		poolerBasicAuthRWSampleFile = fixturesDir + "/pgbouncer/pgbouncer-pooler-basic-auth-rw.yaml"
		poolerBasicAuthROSampleFile = fixturesDir + "/pgbouncer/pgbouncer-pooler-basic-auth-ro.yaml"
		level                       = tests.Low
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
			env.DumpPoolerResourcesInfo(namespace, CurrentSpecReport().LeafNodeText)
		}
	})
	AfterEach(func() {
		err := env.DeleteNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
	})

	It("should recreate after deleting pgbouncer pod", func() {
		// Create a cluster in a namespace we'll delete after the test
		namespace = "pgbouncer-pod-delete"
		err := env.CreateNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
		clusterName, err = env.GetResourceNameFromYAML(sampleFile)
		Expect(err).ToNot(HaveOccurred())
		AssertCreateCluster(namespace, clusterName, sampleFile, env)

		By("setting up read write type pgbouncer pooler", func() {
			createAndAssertPgBouncerPoolerIsSetUp(namespace, poolerBasicAuthRWSampleFile, 1)
		})
		By("setting up read only type pgbouncer pooler", func() {
			createAndAssertPgBouncerPoolerIsSetUp(namespace, poolerBasicAuthROSampleFile, 1)
		})

		assertPodIsRecreated(namespace, poolerBasicAuthRWSampleFile)
		By("verifying pgbouncer read write service connections after deleting pod", func() {
			assertReadWriteConnectionUsingPgBouncerService(namespace, clusterName,
				poolerBasicAuthRWSampleFile, true)
		})

		assertPodIsRecreated(namespace, poolerBasicAuthROSampleFile)
		By("verifying pgbouncer read only service connections after pod deleting", func() {
			assertReadWriteConnectionUsingPgBouncerService(namespace, clusterName,
				poolerBasicAuthROSampleFile, false)
		})
	})
	It("should recreate after deleting pgbouncer deployment", func() {
		// Create a cluster in a namespace we'll delete after the test
		namespace = "pgbouncer-deployment-delete"
		err := env.CreateNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
		clusterName, err = env.GetResourceNameFromYAML(sampleFile)
		Expect(err).ToNot(HaveOccurred())
		AssertCreateCluster(namespace, clusterName, sampleFile, env)

		By("setting up read write type pgbouncer pooler", func() {
			createAndAssertPgBouncerPoolerIsSetUp(namespace, poolerBasicAuthRWSampleFile, 1)
		})
		By("setting up read only type pgbouncer pooler", func() {
			createAndAssertPgBouncerPoolerIsSetUp(namespace, poolerBasicAuthROSampleFile, 1)
		})

		assertDeploymentIsRecreated(namespace, poolerBasicAuthRWSampleFile)
		By("verifying pgbouncer read write service connections after deleting deployment", func() {
			// verify read and write connections after pgbouncer deployment deletion
			assertReadWriteConnectionUsingPgBouncerService(namespace, clusterName,
				poolerBasicAuthRWSampleFile, true)
		})

		assertDeploymentIsRecreated(namespace, poolerBasicAuthROSampleFile)
		By("verifying pgbouncer read only service connections after deleting deployment", func() {
			// verify read and write connections after pgbouncer deployment deletion
			assertReadWriteConnectionUsingPgBouncerService(namespace, clusterName,
				poolerBasicAuthROSampleFile, false)
		})
	})
})

func createAndAssertPgBouncerPoolerIsSetUp(namespace, poolerYamlFilePath string, expectedInstanceCount int) {
	_, _, err := tests.Run("kubectl create -n " + namespace + " -f " + poolerYamlFilePath)
	Expect(err).ToNot(HaveOccurred())

	Eventually(func() (int32, error) {
		deployment, err := getPGBouncerDeployment(namespace, poolerYamlFilePath)

		return deployment.Status.ReadyReplicas, err
	}, 300).Should(BeEquivalentTo(expectedInstanceCount))

	// check pooler pod is up and running
	assertPGBouncerPodsAreReady(namespace, poolerYamlFilePath, expectedInstanceCount)
}

// assertPGBouncerPodsAreReady verifies if PGBouncer pooler pods are ready
func assertPGBouncerPodsAreReady(namespace, poolerYamlFilePath string, expectedPodCount int) {
	Eventually(func() (bool, error) {
		podList, err := getPGBouncerPodList(namespace, poolerYamlFilePath)
		if err != nil {
			return false, err
		}

		podItemsCount := len(podList.Items)
		if podItemsCount != expectedPodCount {
			return false, fmt.Errorf("expected pgBouncer pods count match passed expected instance count. "+
				"Got: %v, Expected: %v", podItemsCount, expectedPodCount)
		}

		activeAndReadyPodCount := 0
		for _, item := range podList.Items {
			if utils.IsPodActive(item) && utils.IsPodReady(item) {
				activeAndReadyPodCount++
			}
			continue
		}

		if activeAndReadyPodCount != expectedPodCount {
			return false, fmt.Errorf("expected pgBouncer pods to be all active and ready. Got: %v, Expected: %v",
				activeAndReadyPodCount, expectedPodCount)
		}

		return true, nil
	}, 90).Should(BeTrue())
}

func assertReadWriteConnectionUsingPgBouncerService(
	namespace,
	clusterName,
	poolerYamlFilePath string,
	isPoolerRW bool,
) {
	poolerServiceName, err := env.GetResourceNameFromYAML(poolerYamlFilePath)
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
	AssertConnection(poolerServiceName, "app", "app", generatedAppUserPassword, *pod, 180, env)

	// verify that, if pooler type setup read write then it will allow both read and
	// write operations or if pooler type setup read only then it will allow only read operations
	if isPoolerRW {
		AssertWritesToPrimarySucceeds(pod, poolerServiceName, "app", "app",
			generatedAppUserPassword)
	} else {
		AssertWritesToReplicaFails(pod, poolerServiceName, "app", "app",
			generatedAppUserPassword)
	}
}

// getPGBouncerPodList gather the pgbouncer pod list
func getPGBouncerPodList(namespace, poolerYamlFilePath string) (*corev1.PodList, error) {
	poolerName, err := env.GetResourceNameFromYAML(poolerYamlFilePath)
	Expect(err).ToNot(HaveOccurred())

	podList := &corev1.PodList{}
	err = env.Client.List(env.Ctx, podList, client.InNamespace(namespace),
		client.MatchingLabels{pgbouncer.PgbouncerNameLabel: poolerName})
	if err != nil {
		return &corev1.PodList{}, err
	}
	return podList, err
}

// getPGBouncerDeployment gather the pgbouncer deployment info
func getPGBouncerDeployment(namespace, poolerYamlFilePath string) (*appsv1.Deployment, error) {
	poolerName, err := env.GetResourceNameFromYAML(poolerYamlFilePath)
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

func assertPodIsRecreated(namespace, poolerSampleFile string) {
	var podNameBeforeDelete string
	poolerName, err := env.GetResourceNameFromYAML(poolerSampleFile)
	Expect(err).ToNot(HaveOccurred())

	By(fmt.Sprintf("deleting pooler '%s' pod", poolerName), func() {
		// gather pgbouncer pod name before deleting
		podList, err := getPGBouncerPodList(namespace, poolerSampleFile)
		Expect(err).ToNot(HaveOccurred())
		Expect(len(podList.Items)).Should(BeEquivalentTo(1))
		podNameBeforeDelete = podList.Items[0].GetName()

		// deleting pgbouncer pod
		cmd := fmt.Sprintf("kubectl delete pod %s -n %s", podNameBeforeDelete, namespace)
		_, _, err = tests.Run(cmd)
		Expect(err).ToNot(HaveOccurred())
	})
	By(fmt.Sprintf("verifying pooler '%s' pod has recreated", poolerName), func() {
		// New pod should be created
		Eventually(func() (bool, error) {
			podList, err := getPGBouncerPodList(namespace, poolerSampleFile)
			if err != nil {
				return false, err
			}
			if len(podList.Items) == 1 {
				if utils.IsPodActive(podList.Items[0]) && utils.IsPodReady(podList.Items[0]) {
					if !(podNameBeforeDelete == podList.Items[0].GetName()) {
						return true, err
					}
				}
			}
			return false, err
		}, 120).Should(BeTrue())
	})
}

func assertDeploymentIsRecreated(namespace, poolerSampleFile string) {
	var deploymentUID types.UID
	deploymentInfo, err := getPGBouncerDeployment(namespace, poolerSampleFile)
	Expect(err).ToNot(HaveOccurred())
	deploymentName := deploymentInfo.GetName()

	By(fmt.Sprintf("deleting pgbouncer '%s' deployment", deploymentName), func() {
		// gather pgbouncer deployment info before delete
		deploymentUID = deploymentInfo.UID
		// deleting pgbouncer deployment
		cmd := fmt.Sprintf("kubectl delete deployment %s -n %s", deploymentName, namespace)
		_, _, err = tests.Run(cmd)
		Expect(err).ToNot(HaveOccurred())
	})
	By(fmt.Sprintf("verifying new deployment '%s' has recreated", deploymentName), func() {
		// new deployment will be created and ready replicas should be one
		Eventually(func() (int32, error) {
			deployment, err := getPGBouncerDeployment(namespace, poolerSampleFile)
			return deployment.Status.ReadyReplicas, err
		}, 300).Should(BeEquivalentTo(1))

		// new deployment UID will be different from old one
		deploymentInfo, err = getPGBouncerDeployment(namespace, poolerSampleFile)
		Expect(err).ToNot(HaveOccurred())
		newDeploymentUID := deploymentInfo.UID
		Expect(newDeploymentUID).ToNot(BeEquivalentTo(deploymentUID))
	})
	By(fmt.Sprintf("newly created pod has up and running after deleting '%s' deployment", deploymentName), func() {
		// check pgbouncer pod will be up and running
		Eventually(func() (bool, error) {
			podList, err := getPGBouncerPodList(namespace, poolerSampleFile)
			if err != nil {
				return false, err
			}
			if len(podList.Items) == 1 {
				return utils.IsPodActive(podList.Items[0]) && utils.IsPodReady(podList.Items[0]), err
			}
			return false, nil
		}, 120).Should(BeTrue())
	})
}
