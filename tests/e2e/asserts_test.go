/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package e2e

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	apiv1alpha1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1alpha1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/specs"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"
	"github.com/EnterpriseDB/cloud-native-postgresql/tests"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

// AssertCreateCluster tests that the pods that should have been created by the sample
// exist and are in ready state
func AssertCreateCluster(namespace string, clusterName string, sample string, env *tests.TestingEnvironment) {
	By(fmt.Sprintf("having a %v namespace", namespace), func() {
		// Creating a namespace should be quick
		timeout := 20
		namespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      namespace,
		}

		Eventually(func() (string, error) {
			namespaceResource := &corev1.Namespace{}
			err := env.Client.Get(env.Ctx, namespacedName, namespaceResource)
			return namespaceResource.GetName(), err
		}, timeout).Should(BeEquivalentTo(namespace))
	})
	By(fmt.Sprintf("creating a Cluster in the %v namespace", namespace), func() {
		_, _, err := tests.Run("kubectl create -n " + namespace + " -f " + sample)
		Expect(err).ToNot(HaveOccurred())
	})
	// Setting up a cluster with three pods is slow, usually 200-600s
	AssertClusterIsReady(namespace, clusterName, 600, env)
}

func AssertClusterIsReady(namespace string, clusterName string, timeout int, env *tests.TestingEnvironment) {
	By("having a Cluster with each instance in status ready", func() {
		namespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      clusterName,
		}
		// Eventually the number of ready instances should be equal to the
		// amount of instances defined in the cluster
		cluster := &apiv1.Cluster{}
		err := env.Client.Get(env.Ctx, namespacedName, cluster)
		if err != nil {
			cluster := &apiv1alpha1.Cluster{}
			err = env.Client.Get(env.Ctx, namespacedName, cluster)
		}
		Expect(err).ToNot(HaveOccurred())
		Eventually(func() (int, error) {
			podList, err := env.GetClusterPodList(namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			readyInstances := utils.CountReadyPods(podList.Items)
			return readyInstances, err
		}, timeout).Should(BeEquivalentTo(cluster.Spec.Instances))
	})
}

// AssertConnection is used if a connection from a pod to a postgresql
// database works
func AssertConnection(host string, user string, dbname string,
	password string, queryingPod corev1.Pod, timeout int, env *tests.TestingEnvironment) {
	By(fmt.Sprintf("connecting to the %v service as %v", host, user), func() {
		Eventually(func() string {
			dsn := fmt.Sprintf("host=%v user=%v dbname=%v password=%v sslmode=require", host, user, dbname, password)
			timeout := time.Second * 2
			stdout, _, err := env.ExecCommand(env.Ctx, queryingPod, "postgres", &timeout,
				"psql", dsn, "-tAc", "SELECT 1")
			if err != nil {
				return ""
			}
			return stdout
		}, timeout).Should(Equal("1\n"))
	})
}

// AssertOperatorPodUnchanged verifies that the pod has an expected name and never restarted
func AssertOperatorPodUnchanged(expectedOperatorPodName string) {
	operatorPod, err := env.GetOperatorPod()
	Expect(err).NotTo(HaveOccurred())
	Expect(operatorPod.GetName()).Should(BeEquivalentTo(expectedOperatorPodName),
		"Operator pod was recreated before the end of the test")
	restartCount := -1
	for _, containerStatus := range operatorPod.Status.ContainerStatuses {
		if containerStatus.Name == "manager" {
			restartCount = int(containerStatus.RestartCount)
		}
	}
	Expect(restartCount).Should(BeEquivalentTo(0), fmt.Sprintf("Operator pod get restarted %v times ", restartCount))
}

// AssertOperatorIsReady verifies that the operator is ready
func AssertOperatorIsReady() {
	Eventually(env.IsOperatorReady, 120).Should(BeTrue(), "Operator pod is not ready")
}

// AssertCreateTestData create test data on primary pod
func AssertCreateTestData(namespace, clusterName, tableName string) {
	By("creating test data", func() {
		primaryPodInfo, err := env.GetClusterPrimary(namespace, clusterName)
		Expect(err).NotTo(HaveOccurred())
		commandTimeout := time.Second * 5
		query := fmt.Sprintf("CREATE TABLE %v AS VALUES (1), (2);", tableName)
		_, _, err = env.ExecCommand(env.Ctx, *primaryPodInfo, "postgres",
			&commandTimeout, "psql", "-U", "postgres", "app", "-tAc", query)
		Expect(err).ToNot(HaveOccurred())
	})
}

// insertRecordIntoTable insert an entry entry into a table
func insertRecordIntoTable(namespace, clusterName, tableName string, value int) {
	primaryPodInfo, err := env.GetClusterPrimary(namespace, clusterName)
	Expect(err).NotTo(HaveOccurred())

	query := fmt.Sprintf("INSERT INTO %v VALUES (%v);", tableName, value)
	_, _, err = env.ExecCommand(env.Ctx, *primaryPodInfo, "postgres",
		&commandTimeout, "psql", "-U", "postgres", "app", "-tAc", query)
	Expect(err).ToNot(HaveOccurred())
}

// AssertDataExpectedCount verifies that an expected amount of rows exist on the table
func AssertDataExpectedCount(namespace, podName, tableName string, expectedValue int) {
	By(fmt.Sprintf("verifying test data on pod %v", podName), func() {
		newPodNamespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      podName,
		}
		Pod := &corev1.Pod{}
		err := env.Client.Get(env.Ctx, newPodNamespacedName, Pod)
		Expect(err).ToNot(HaveOccurred())
		query := fmt.Sprintf("select count(*) from %v", tableName)
		commandTimeout := time.Second * 5
		// The data previously created should be there

		Eventually(func() (int, error) {
			stdout, _, err := env.ExecCommand(env.Ctx, *Pod, "postgres",
				&commandTimeout, "psql", "-U", "postgres", "app", "-tAc", query)
			Expect(err).ToNot(HaveOccurred())
			value, err := strconv.Atoi(strings.Trim(stdout, "\n"))
			return value, err
		}, 300).Should(BeEquivalentTo(expectedValue))
	})
}

// assertClusterStandbysAreStreaming verifies that all the standbys of a
// cluster have a wal receiver running.
func assertClusterStandbysAreStreaming(namespace string, clusterName string) {
	Eventually(func() error {
		podList, err := env.GetClusterPodList(namespace, clusterName)
		if err != nil {
			return err
		}

		primary, err := env.GetClusterPrimary(namespace, clusterName)
		if err != nil {
			return err
		}

		for _, pod := range podList.Items {
			// Primary should be ignored
			if pod.GetName() == primary.GetName() {
				continue
			}

			timeout := time.Second
			out, _, err := env.ExecCommand(env.Ctx, pod, "postgres", &timeout,
				"psql", "-U", "postgres", "-tAc", "SELECT count(*) FROM pg_stat_wal_receiver")
			if err != nil {
				return err
			}

			value, atoiErr := strconv.Atoi(strings.Trim(out, "\n"))
			if atoiErr != nil {
				return atoiErr
			}
			if value != 1 {
				return fmt.Errorf("pod %v not streaming", pod.Name)
			}
		}

		return nil
	}, 60).ShouldNot(HaveOccurred())
}

func AssertStandbysFollowPromotion(namespace string, clusterName string, timeout int32) {
	// Track the start of the assert. We expect to complete before
	// timeout.
	start := time.Now()

	By(fmt.Sprintf("having all the instances on timeline 2 in less than %v sec", timeout), func() {
		// One of the standbys will be promoted and the rw service
		// should point to it, so the application can keep writing.
		// Records inserted after the promotion will be marked
		// with timeline '00000002'. If all the instances are back
		// and are following the promotion, we should find those
		// records on each of them.

		commandTimeout := time.Second * 2
		for i := 1; i < 4; i++ {
			podName := fmt.Sprintf("%v-%v", clusterName, i)
			podNamespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      podName,
			}
			Eventually(func() (string, error) {
				pod := &corev1.Pod{}
				if err := env.Client.Get(env.Ctx, podNamespacedName, pod); err != nil {
					return "", err
				}
				out, _, err := env.ExecCommand(env.Ctx, *pod, "postgres",
					&commandTimeout, "psql", "-U", "postgres", "app", "-tAc",
					"SELECT count(*) > 0 FROM tps.tl "+
						"WHERE timeline = '00000002'")
				return strings.TrimSpace(out), err
			}, timeout).Should(BeEquivalentTo("t"),
				"Pod %v should have moved to timeline 2", podName)
		}
	})

	By("having all the instances ready", func() {
		AssertClusterIsReady(namespace, clusterName, 600, env)
	})

	By(fmt.Sprintf("restoring full cluster functionality within %v seconds", timeout), func() {
		elapsed := time.Since(start)
		fmt.Printf("Cluster has been in a degraded state for %v seconds\n", elapsed)
		Expect(elapsed.Seconds()).To(BeNumerically("<", timeout))
	})
}

func AssertWritesResumedBeforeTimeout(namespace string, clusterName string, timeout int32) {
	By(fmt.Sprintf("resuming writing in less than %v sec", timeout), func() {
		// We measure the difference between the last entry with
		// timeline 1 and the first one with timeline 2.
		// It should be less than maxFailoverTime seconds.
		// Any pod is good to measure the difference, we choose -2
		query := "WITH a AS ( " +
			"  SELECT * " +
			"  , t-lag(t) OVER (order by t) AS timediff " +
			"  FROM tps.tl " +
			") " +
			"SELECT EXTRACT ('EPOCH' FROM timediff) " +
			"FROM a " +
			"WHERE timeline = ( " +
			"  SELECT timeline " +
			"  FROM tps.tl " +
			"  ORDER BY t DESC " +
			"  LIMIT 1 " +
			") " +
			"ORDER BY t ASC " +
			"LIMIT 1;"
		podName := clusterName + "-2"
		namespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      podName,
		}
		var switchTime float64
		commandTimeout := time.Second * 5
		pod := &corev1.Pod{}
		err := env.Client.Get(env.Ctx, namespacedName, pod)
		Expect(err).ToNot(HaveOccurred())
		out, _, _ := env.ExecCommand(env.Ctx, *pod, "postgres",
			&commandTimeout, "psql", "-U", "postgres", "app", "-tAc", query)
		switchTime, err = strconv.ParseFloat(strings.TrimSpace(out), 64)
		fmt.Printf("Write activity resumed in %v seconds\n", switchTime)
		Expect(switchTime, err).Should(BeNumerically("<", timeout))
	})
}

// AssertNewPrimary checks that, during a failover, a new primary
// is being elected and promoted and that write operation succeed
// on this new pod.
func AssertNewPrimary(namespace string, clusterName string, oldprimary string) {
	By("verifying the new primary pod", func() {
		// Gather the primary
		timeout := 120
		namespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      clusterName,
		}
		// Wait for the operator to set a new TargetPrimary
		Eventually(func() (string, error) {
			cluster := &apiv1.Cluster{}
			err := env.Client.Get(env.Ctx, namespacedName, cluster)
			return cluster.Status.TargetPrimary, err
		}, timeout).ShouldNot(BeEquivalentTo(oldprimary))
		cluster := &apiv1.Cluster{}
		err := env.Client.Get(env.Ctx, namespacedName, cluster)
		newPrimary := cluster.Status.TargetPrimary
		Expect(err).ToNot(HaveOccurred())

		// Expect the chosen pod to eventually become a primary
		namespacedName = types.NamespacedName{
			Namespace: namespace,
			Name:      newPrimary,
		}
		Eventually(func() (bool, error) {
			pod := corev1.Pod{}
			err := env.Client.Get(env.Ctx, namespacedName, &pod)
			return specs.IsPodPrimary(pod), err
		}, timeout).Should(BeTrue())
	})
	By("verifying write operation on the new primary pod", func() {
		commandTimeout := time.Second * 5
		pod, err := env.GetClusterPrimary(namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		// Expect write operation to succeed
		query := "create table assert_new_primary(var1 text)"
		_, _, err = env.ExecCommand(env.Ctx, *pod, "postgres",
			&commandTimeout, "psql", "-U", "postgres", "app", "-tAc", query)
		Expect(err).ToNot(HaveOccurred())
	})
}

func AssertStorageCredentialsAreCreated(namespace string, name string, id string, key string) {
	_, _, err := tests.Run(fmt.Sprintf("kubectl create secret generic %v -n %v "+
		"--from-literal='ID=%v' "+
		"--from-literal='KEY=%v'",
		name, namespace, id, key))
	Expect(err).ToNot(HaveOccurred())
}

// InstallMinio installs minio to verify the backup and archive walls
func InstallMinio(namespace string) {
	// Create a PVC-based deployment for the minio version
	// minio/minio:RELEASE.2020-04-23T00-58-49Z
	minioPVCFile := fixturesDir + "/backup/minio/minio-pvc.yaml"
	minioDeploymentFile := fixturesDir +
		"/backup/minio/minio-deployment.yaml"

	_, _, err := tests.Run(fmt.Sprintf("kubectl apply -n %v -f %v",
		namespace, minioPVCFile))
	Expect(err).ToNot(HaveOccurred())
	_, _, err = tests.Run(fmt.Sprintf("kubectl apply -n %v -f %v",
		namespace, minioDeploymentFile))
	Expect(err).ToNot(HaveOccurred())

	// Wait for the minio pod to be ready
	deploymentNamespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      "minio",
	}
	Eventually(func() (int32, error) {
		deployment := &appsv1.Deployment{}
		err = env.Client.Get(env.Ctx, deploymentNamespacedName, deployment)
		return deployment.Status.ReadyReplicas, err
	}, 300).Should(BeEquivalentTo(1))

	// Create a minio service
	_, _, err = tests.Run(fmt.Sprintf("kubectl apply -n %v -f %v",
		namespace, fixturesDir+"/backup/minio/minio-service.yaml"))
	Expect(err).ToNot(HaveOccurred())
}

// InstallMinioClient installs minio client to verify the backup and archive walls
func InstallMinioClient(namespace string) {
	clientFile := fixturesDir + "/backup/minio/minio-client.yaml"
	_, _, err := tests.Run(fmt.Sprintf(
		"kubectl apply -n %v -f %v",
		namespace, clientFile))
	Expect(err).ToNot(HaveOccurred())

	mcNamespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      minioClientName,
	}
	Eventually(func() (bool, error) {
		mc := &corev1.Pod{}
		err = env.Client.Get(env.Ctx, mcNamespacedName, mc)
		return utils.IsPodReady(*mc), err
	}, 180).Should(BeTrue())
}

// AssertArchiveWalOnMinio to archive walls and verify that exists
func AssertArchiveWalOnMinio(namespace, clusterName string) {
	// Create a WAL on the primary and check if it arrives at minio, within a short time
	By("archiving WALs and verifying they exist", func() {
		primary := clusterName + "-1"
		out, _, err := tests.Run(fmt.Sprintf(
			"kubectl exec -n %v %v -- %v",
			namespace,
			primary,
			switchWalCmd))
		Expect(err).ToNot(HaveOccurred())

		latestWAL := strings.TrimSpace(out)
		Eventually(func() (int, error) {
			// WALs are compressed with gzip in the fixture
			return CountFilesOnMinio(namespace, latestWAL+".gz")
		}, 30).Should(BeEquivalentTo(1))
	})
}

// CountFilesOnMinio uses the minioClient in the given `namespace` to count  the
// amount of files matching the given `path`
func CountFilesOnMinio(namespace string, path string) (value int, err error) {
	var stdout string
	stdout, _, err = tests.RunUnchecked(fmt.Sprintf(
		"kubectl exec -n %v %v -- %v",
		namespace,
		minioClientName,
		composeFindMinioCmd(path, "minio")))
	if err != nil {
		return -1, err
	}
	value, err = strconv.Atoi(strings.Trim(stdout, "\n"))
	return value, err
}

func AssertReplicaModeCluster(
	namespace,
	srcClusterName,
	srcClusterSample,
	replicaClusterName,
	replicaClusterSample,
	checkQuery string) {
	var primarySrcCluster, primaryReplicaCluster *corev1.Pod
	var err error
	commandTimeout = time.Second * 5

	By("creating source cluster", func() {
		// Create replica source cluster
		AssertCreateCluster(namespace, srcClusterName, srcClusterSample, env)
		// Get primary from source cluster
		Eventually(func() error {
			primarySrcCluster, err = env.GetClusterPrimary(namespace, srcClusterName)
			return err
		}, 5).Should(BeNil())
	})

	By("creating test data in source cluster", func() {
		cmd := "CREATE TABLE test_replica AS VALUES (1), (2);"
		_, _, err = env.ExecCommand(env.Ctx, *primarySrcCluster, "postgres",
			&commandTimeout, "psql", "-U", "postgres", "app", "-tAc", cmd)
		Expect(err).ToNot(HaveOccurred())
	})

	By("creating replica cluster", func() {
		AssertCreateCluster(namespace, replicaClusterName, replicaClusterSample, env)
		// Get primary from replica cluster
		Eventually(func() error {
			primaryReplicaCluster, err = env.GetClusterPrimary(namespace, replicaClusterName)
			return err
		}, 5).Should(BeNil())
	})

	By("verifying that replica cluster primary is in recovery mode", func() {
		query := "select pg_is_in_recovery();"
		Eventually(func() (string, error) {
			stdOut, _, err := env.ExecCommand(env.Ctx, *primaryReplicaCluster, "postgres",
				&commandTimeout, "psql", "-U", "postgres", "app", "-tAc", query)
			return strings.Trim(stdOut, "\n"), err
		}, 300, 15).Should(BeEquivalentTo("t"))
	})

	By("checking data have been copied correctly in replica cluster", func() {
		Eventually(func() (string, error) {
			stdOut, _, err := env.ExecCommand(env.Ctx, *primaryReplicaCluster, "postgres",
				&commandTimeout, "psql", "-U", "postgres", "app", "-tAc", checkQuery)
			return strings.Trim(stdOut, "\n"), err
		}, 180, 10).Should(BeEquivalentTo("2"))
	})

	By("writing some new data to the source cluster", func() {
		insertRecordIntoTable(namespace, srcClusterName, "test_replica", 3)
	})

	By("checking new data have been copied correctly in replica cluster", func() {
		Eventually(func() (string, error) {
			stdOut, _, err := env.ExecCommand(env.Ctx, *primaryReplicaCluster, "postgres",
				&commandTimeout, "psql", "-U", "postgres", "app", "-tAc", checkQuery)
			return strings.Trim(stdOut, "\n"), err
		}, 180, 15).Should(BeEquivalentTo("3"))
	})
}

func AssertWritesToReplicaFails(
	connectingPod *corev1.Pod,
	service string,
	appDBName string,
	appDBUser string,
	appDBPass string) {
	By(fmt.Sprintf("Verifying %v service doesn't allow writes", service),
		func() {
			timeout := time.Second * 2

			dsn := fmt.Sprintf("host=%v user=%v dbname=%v password=%v sslmode=require",
				service, appDBUser, appDBName, appDBPass)

			// Expect to be connected to a replica
			stdout, _, err := env.ExecCommand(env.Ctx, *connectingPod, "postgres", &timeout,
				"psql", dsn, "-tAc", "select pg_is_in_recovery()")
			value := strings.Trim(stdout, "\n")
			Expect(value, err).To(Equal("t"))

			// Expect to be in a read-only transaction
			_, _, err = env.ExecCommand(env.Ctx, *connectingPod, "postgres", &timeout,
				"psql", dsn, "-tAc", "CREATE TABLE table1(var1 text);")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).Should(
				ContainSubstring("cannot execute CREATE TABLE in a read-only transaction"))
		})
}

func AssertWritesToPrimarySucceeds(
	connectingPod *corev1.Pod,
	service string,
	appDBName string,
	appDBUser string,
	appDBPass string) {
	By(fmt.Sprintf("Verifying %v service correctly manages writes", service),
		func() {
			timeout := time.Second * 2

			dsn := fmt.Sprintf("host=%v user=%v dbname=%v password=%v sslmode=require",
				service, appDBUser, appDBName, appDBPass)

			// Expect to be connected to a primary
			stdout, _, err := env.ExecCommand(env.Ctx, *connectingPod, "postgres", &timeout,
				"psql", dsn, "-tAc", "select pg_is_in_recovery()")
			value := strings.Trim(stdout, "\n")
			Expect(value, err).To(Equal("f"))

			// Expect to be able to write
			_, _, err = env.ExecCommand(env.Ctx, *connectingPod, "postgres", &timeout,
				"psql", dsn, "-tAc", "CREATE TABLE table1(var1 text);")
			Expect(err).ToNot(HaveOccurred())
		})
}
