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
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/thoas/go-funk"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/strings/slices"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/certs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	testsUtils "github.com/cloudnative-pg/cloudnative-pg/tests/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func AssertSwitchover(namespace string, clusterName string, env *testsUtils.TestingEnvironment) {
	AssertSwitchoverWithHistory(namespace, clusterName, false, env)
}

func AssertSwitchoverOnReplica(namespace string, clusterName string, env *testsUtils.TestingEnvironment) {
	AssertSwitchoverWithHistory(namespace, clusterName, true, env)
}

// AssertSwitchoverWithHistory does a switchover and waits until the old primary
// is streaming from the new primary.
// In a primary cluster it checks a new timeline was created by watching for history files.
// In a replica cluster, a switchover per se does not switch the timeline
func AssertSwitchoverWithHistory(
	namespace string,
	clusterName string,
	isReplica bool,
	env *testsUtils.TestingEnvironment,
) {
	var pods []string
	var oldPrimary, targetPrimary string
	var oldPodListLength int

	// First we check that the starting situation is the expected one
	By("checking that CurrentPrimary and TargetPrimary are the same", func() {
		var cluster *apiv1.Cluster

		Eventually(func(g Gomega) {
			var err error
			cluster, err = env.GetCluster(namespace, clusterName)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(cluster.Status.CurrentPrimary, err).To(
				BeEquivalentTo(cluster.Status.TargetPrimary),
			)
		}).Should(Succeed())

		oldPrimary = cluster.Status.CurrentPrimary

		// Gather pod names
		podList, err := env.GetClusterPodList(namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		oldPodListLength = len(podList.Items)
		for _, p := range podList.Items {
			pods = append(pods, p.Name)
		}
		sort.Strings(pods)
		// TODO: this algorithm is very naïve, only works if we're lucky and the `-1` instance
		// is the primary and the -2 is the most advanced replica
		Expect(pods[0]).To(BeEquivalentTo(oldPrimary))
		targetPrimary = pods[1]
	})

	By(fmt.Sprintf("setting the TargetPrimary node to trigger a switchover to %s", targetPrimary), func() {
		err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			cluster, err := env.GetCluster(namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			cluster.Status.TargetPrimary = targetPrimary
			return env.Client.Status().Update(env.Ctx, cluster)
		})
		Expect(err).ToNot(HaveOccurred())
	})

	By("waiting that the TargetPrimary become also CurrentPrimary", func() {
		Eventually(func() (string, error) {
			cluster, err := env.GetCluster(namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			return cluster.Status.CurrentPrimary, err
		}, testTimeouts[testsUtils.NewPrimaryAfterSwitchover]).Should(BeEquivalentTo(targetPrimary))
	})

	By("waiting that the old primary become ready", func() {
		namespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      oldPrimary,
		}
		timeout := 120
		Eventually(func() (bool, error) {
			pod := corev1.Pod{}
			err := env.Client.Get(env.Ctx, namespacedName, &pod)
			return utils.IsPodActive(pod) && utils.IsPodReady(pod), err
		}, timeout).Should(BeTrue())
	})

	By("waiting that the old primary become a standby", func() {
		namespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      oldPrimary,
		}
		timeout := 120
		Eventually(func() (bool, error) {
			pod := corev1.Pod{}
			err := env.Client.Get(env.Ctx, namespacedName, &pod)
			return specs.IsPodStandby(pod), err
		}, timeout).Should(BeTrue())
	})

	// After we finish the switchover, we should wait for the cluster to be ready
	// otherwise, anyone executing this may not wait and also, the following part of the function
	// may fail because the switchover hasn't properly finish yet.
	AssertClusterIsReady(namespace, clusterName, testTimeouts[testsUtils.ClusterIsReady], env)

	if !isReplica {
		By("confirming that the all postgres containers have *.history file after switchover", func() {
			pods = []string{}
			timeout := 120

			// Gather pod names
			podList, err := env.GetClusterPodList(namespace, clusterName)
			Expect(len(podList.Items), err).To(BeEquivalentTo(oldPodListLength))
			for _, p := range podList.Items {
				pods = append(pods, p.Name)
			}

			Eventually(func() error {
				count := 0
				for _, pod := range pods {
					out, _, err := env.ExecCommandInInstancePod(
						testsUtils.PodLocator{
							Namespace: namespace,
							PodName:   pod,
						}, nil, "sh", "-c", "ls $PGDATA/pg_wal/*.history")
					if err != nil {
						return err
					}

					numHistory := len(strings.Split(strings.TrimSpace(out), "\n"))
					GinkgoWriter.Printf("count %d: pod: %s, the number of history file in pg_wal: %d\n", count, pod, numHistory)
					count++
					if numHistory > 0 {
						continue
					}

					return errors.New("at least 1 .history file expected but none found")
				}
				return nil
			}, timeout).ShouldNot(HaveOccurred())
		})
	}
}

// AssertCreateCluster creates the cluster and verifies that the ready pods
// correspond to the number of Instances in the cluster spec.
// Important: this is not equivalent to "kubectl apply", and is not able
// to apply a patch to an existing object.
func AssertCreateCluster(
	namespace string,
	clusterName string,
	sampleFile string,
	env *testsUtils.TestingEnvironment,
) {
	By(fmt.Sprintf("having a %v namespace", namespace), func() {
		// Creating a namespace should be quick
		namespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      namespace,
		}
		Eventually(func() (string, error) {
			namespaceResource := &corev1.Namespace{}
			err := env.Client.Get(env.Ctx, namespacedName, namespaceResource)
			return namespaceResource.GetName(), err
		}, testTimeouts[testsUtils.NamespaceCreation]).Should(BeEquivalentTo(namespace))
	})

	By(fmt.Sprintf("creating a Cluster in the %v namespace", namespace), func() {
		CreateResourceFromFile(namespace, sampleFile)
	})
	// Setting up a cluster with three pods is slow, usually 200-600s
	AssertClusterIsReady(namespace, clusterName, testTimeouts[testsUtils.ClusterIsReady], env)
}

// AssertClusterIsReady checks the cluster has as many pods as in spec, that
// none of them are going to be deleted, and that the status is Healthy
func AssertClusterIsReady(namespace string, clusterName string, timeout int, env *testsUtils.TestingEnvironment) {
	By(fmt.Sprintf("having a Cluster %s with each instance in status ready", clusterName), func() {
		// Eventually the number of ready instances should be equal to the
		// amount of instances defined in the cluster and
		// the cluster status should be in healthy state
		var cluster *apiv1.Cluster

		Eventually(func(g Gomega) {
			var err error
			cluster, err = env.GetCluster(namespace, clusterName)
			g.Expect(err).ToNot(HaveOccurred())
		}).Should(Succeed())

		start := time.Now()
		Eventually(func() (string, error) {
			podList, err := env.GetClusterPodList(namespace, clusterName)
			if err != nil {
				return "", err
			}
			if cluster.Spec.Instances == utils.CountReadyPods(podList.Items) {
				for _, pod := range podList.Items {
					if pod.DeletionTimestamp != nil {
						return fmt.Sprintf("Pod '%s' is waiting for deletion", pod.Name), nil
					}
				}
				cluster, err = env.GetCluster(namespace, clusterName)
				return cluster.Status.Phase, err
			}
			return fmt.Sprintf("Ready pod is not as expected. Spec Instances: %d, ready pods: %d \n",
				cluster.Spec.Instances,
				utils.CountReadyPods(podList.Items)), nil
		}, timeout, 2).Should(BeEquivalentTo(apiv1.PhaseHealthy),
			func() string {
				cluster := testsUtils.PrintClusterResources(namespace, clusterName, env)
				nodes, _ := env.DescribeKubernetesNodes()
				return fmt.Sprintf("CLUSTER STATE\n%s\n\nK8S NODES\n%s",
					cluster, nodes)
			},
		)

		if cluster.Spec.Instances != 1 {
			Eventually(func(g Gomega) {
				podList, err := env.GetClusterPodList(namespace, clusterName)
				g.Expect(err).ToNot(HaveOccurred(), "cannot get cluster pod list")

				primaryPod, err := env.GetClusterPrimary(namespace, clusterName)
				g.Expect(err).ToNot(HaveOccurred(), "cannot find cluster primary pod")

				replicaNamesList := make([]string, 0, len(podList.Items)-1)
				for _, pod := range podList.Items {
					if pod.Name != primaryPod.Name {
						replicaNamesList = append(replicaNamesList, pq.QuoteLiteral(pod.Name))
					}
				}
				replicaNamesString := strings.Join(replicaNamesList, ",")
				out, _, err := env.ExecQueryInInstancePod(
					testsUtils.PodLocator{
						Namespace: namespace,
						PodName:   primaryPod.Name,
					},
					"postgres",
					fmt.Sprintf("SELECT COUNT(*) FROM pg_stat_replication WHERE application_name IN (%s)",
						replicaNamesString),
				)
				g.Expect(err).ToNot(HaveOccurred(), "cannot extract the list of streaming replicas")
				g.Expect(strings.TrimSpace(out)).To(BeEquivalentTo(fmt.Sprintf("%d", len(replicaNamesList))))
			}, timeout, 2).Should(Succeed(), "Replicas are attached via streaming connection")
		}
		GinkgoWriter.Println("Cluster ready, took", time.Since(start))
	})
}

func AssertClusterDefault(namespace string, clusterName string,
	isExpectedToDefault bool, env *testsUtils.TestingEnvironment,
) {
	By("having a Cluster object populated with default values", func() {
		// Eventually the number of ready instances should be equal to the
		// amount of instances defined in the cluster and
		// the cluster status should be in healthy state
		var cluster *apiv1.Cluster
		Eventually(func(g Gomega) {
			var err error
			cluster, err = env.GetCluster(namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			g.Expect(err).ToNot(HaveOccurred())
		}).Should(Succeed())

		validationErr := cluster.Validate()
		if isExpectedToDefault {
			Expect(validationErr).Should(BeEmpty(), validationErr)
		} else {
			Expect(validationErr).ShouldNot(BeEmpty(), validationErr)
		}
	})
}

func AssertWebhookEnabled(env *testsUtils.TestingEnvironment, mutating, validating string) {
	By("re-setting namespace selector for all admission controllers", func() {
		// Setting the namespace selector in MutatingWebhook and ValidatingWebhook
		// to nil will go back to the default behaviour
		mWhc, position, err := testsUtils.GetCNPGsMutatingWebhookByName(env, mutating)
		Expect(err).ToNot(HaveOccurred())
		mWhc.Webhooks[position].NamespaceSelector = nil
		err = testsUtils.UpdateCNPGsMutatingWebhookConf(env, mWhc)
		Expect(err).ToNot(HaveOccurred())

		vWhc, position, err := testsUtils.GetCNPGsValidatingWebhookByName(env, validating)
		Expect(err).ToNot(HaveOccurred())
		vWhc.Webhooks[position].NamespaceSelector = nil
		err = testsUtils.UpdateCNPGsValidatingWebhookConf(env, vWhc)
		Expect(err).ToNot(HaveOccurred())
	})
}

// Update the secrets and verify cluster reference the updated resource version of secrets
func AssertUpdateSecret(field string, value string, secretName string, namespace string,
	clusterName string, timeout int, env *testsUtils.TestingEnvironment,
) {
	var secret corev1.Secret
	Eventually(func(g Gomega) {
		err := env.Client.Get(env.Ctx,
			ctrlclient.ObjectKey{Namespace: namespace, Name: secretName},
			&secret)
		g.Expect(err).ToNot(HaveOccurred())
	}).Should(Succeed())

	secret.Data[field] = []byte(value)
	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		return env.Client.Update(env.Ctx, &secret)
	})
	Expect(err).ToNot(HaveOccurred())

	// Wait for the cluster pickup the updated secrets version first
	Eventually(func() string {
		cluster, err := env.GetCluster(namespace, clusterName)
		if err != nil {
			GinkgoWriter.Printf("Error reports while retrieving cluster %v\n", err.Error())
			return ""
		}
		switch {
		case strings.HasSuffix(secretName, apiv1.ApplicationUserSecretSuffix):
			GinkgoWriter.Printf("Resource version of Application secret referenced in the cluster is %v\n",
				cluster.Status.SecretsResourceVersion.ApplicationSecretVersion)
			return cluster.Status.SecretsResourceVersion.ApplicationSecretVersion
		case strings.HasSuffix(secretName, apiv1.SuperUserSecretSuffix):
			GinkgoWriter.Printf("Resource version of Superuser secret referenced in the cluster is %v\n",
				cluster.Status.SecretsResourceVersion.SuperuserSecretVersion)
			return cluster.Status.SecretsResourceVersion.SuperuserSecretVersion
		default:
			GinkgoWriter.Printf("Unsupported secrets name found %v\n", secretName)
			return ""
		}
	}, timeout).Should(BeEquivalentTo(secret.ResourceVersion))
}

// AssertConnection is used if a connection from a pod to a postgresql
// database works
func AssertConnection(host string, user string, dbname string,
	password string, queryingPod *corev1.Pod, timeout int, env *testsUtils.TestingEnvironment,
) {
	By(fmt.Sprintf("connecting to the %v service as %v", host, user), func() {
		Eventually(func() string {
			dsn := fmt.Sprintf("host=%v user=%v dbname=%v password=%v sslmode=require", host, user, dbname, password)
			commandTimeout := time.Second * 10
			stdout, _, err := env.ExecCommand(env.Ctx, *queryingPod, specs.PostgresContainerName, &commandTimeout,
				"psql", dsn, "-tAc", "SELECT 1")
			if err != nil {
				return ""
			}
			return stdout
		}, timeout).Should(Equal("1\n"))
	})
}

// AssertOperatorIsReady verifies that the operator is ready
func AssertOperatorIsReady() {
	Eventually(func() (bool, error) {
		ready, err := env.IsOperatorReady()
		if ready && err == nil {
			return true, nil
		}
		// Waiting a bit to avoid overloading the API server
		time.Sleep(1 * time.Second)
		return ready, err
	}, testTimeouts[testsUtils.OperatorIsReady]).Should(BeTrue(), "Operator pod is not ready")
}

// AssertDatabaseIsReady checks the database on the primary is ready to run queries
//
// NOTE: even if we checked AssertClusterIsReady, a temporary DB connectivity issue would take
// failureThreshold x periodSeconds to be detected
func AssertDatabaseIsReady(namespace, clusterName, dbName string) {
	By(fmt.Sprintf("checking the database on %s is ready", clusterName), func() {
		Eventually(func(g Gomega) {
			primary, err := env.GetClusterPrimary(namespace, clusterName)
			g.Expect(err).ToNot(HaveOccurred())

			stdout, stderr, err := env.ExecCommandInInstancePod(testsUtils.PodLocator{
				Namespace: namespace,
				PodName:   primary.GetName(),
			}, nil, "pg_isready")
			g.Expect(err).ShouldNot(HaveOccurred())
			g.Expect(stderr).To(BeEmpty(), "while checking pg_isready")
			g.Expect(stdout).To(ContainSubstring("accepting"), "while checking pg_isready: Not accepting connections")
			_, _, err = env.ExecQueryInInstancePod(testsUtils.PodLocator{
				Namespace: namespace,
				PodName:   primary.GetName(),
			}, testsUtils.DatabaseName(dbName), "select 1")
			g.Expect(err).ShouldNot(HaveOccurred())
		}, RetryTimeout, PollingTime).Should(Succeed())
	})
}

// AssertCreateTestData create test on the "app" database
func AssertCreateTestData(env *testsUtils.TestingEnvironment, namespace, clusterName, tableName string) {
	AssertDatabaseIsReady(namespace, clusterName, testsUtils.AppDBName)
	By(fmt.Sprintf("creating test data in cluster %v", clusterName), func() {
		forward, conn, err := testsUtils.ForwardPSQLConnection(
			env,
			namespace,
			clusterName,
			testsUtils.AppDBName,
			apiv1.ApplicationUserSecretSuffix,
		)
		Expect(err).ToNot(HaveOccurred())

		query := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %v AS VALUES (1),(2);", tableName)
		_, err = conn.Exec(query)
		Expect(err).ToNot(HaveOccurred())
		forward.Stop()
	})
}

// AssertCreateTestDataWithDatabaseName create test data in a given database.
func AssertCreateTestDataWithDatabaseName(
	env *testsUtils.TestingEnvironment,
	namespace,
	clusterName,
	databaseName,
	tableName string,
) {
	By(fmt.Sprintf("creating test data in cluster %v", clusterName), func() {
		forward, conn, err := testsUtils.ForwardPSQLConnection(
			env,
			namespace,
			clusterName,
			databaseName,
			apiv1.ApplicationUserSecretSuffix,
		)
		Expect(err).ToNot(HaveOccurred())
		query := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %v AS VALUES (1),(2);", tableName)
		_, err = conn.Exec(query)
		Expect(err).ToNot(HaveOccurred())
		forward.Stop()
	})
}

type TableLocator struct {
	Namespace   string
	ClusterName string
	TableName   string
	Tablespace  string
}

// AssertCreateTestDataInTablespace create test data.
func AssertCreateTestDataInTablespace(env *testsUtils.TestingEnvironment, tl TableLocator) {
	AssertDatabaseIsReady(tl.Namespace, tl.ClusterName, testsUtils.AppDBName)
	By(fmt.Sprintf("creating test data in tablespace %q", tl.Tablespace), func() {
		forward, conn, err := testsUtils.ForwardPSQLConnection(
			env,
			tl.Namespace,
			tl.ClusterName,
			testsUtils.AppDBName,
			apiv1.ApplicationUserSecretSuffix,
		)
		defer func() {
			forward.Stop()
		}()
		Expect(err).ToNot(HaveOccurred())

		query := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %v TABLESPACE %v AS VALUES (1),(2);",
			tl.TableName, tl.Tablespace)

		_, err = conn.Exec(query)
		Expect(err).ToNot(HaveOccurred())
	})
}

// AssertCreateTestDataLargeObject create large objects with oid and data
func AssertCreateTestDataLargeObject(namespace, clusterName string, oid int, data string, pod *corev1.Pod) {
	By("creating large object", func() {
		query := fmt.Sprintf("CREATE TABLE IF NOT EXISTS image (name text,raster oid); "+
			"INSERT INTO image (name, raster) VALUES ('beautiful image', lo_from_bytea(%d, '%s'));", oid, data)
		appUser, appUserPass, err := testsUtils.GetCredentials(clusterName, namespace, apiv1.ApplicationUserSecretSuffix, env)
		Expect(err).ToNot(HaveOccurred())
		host, err := testsUtils.GetHostName(namespace, clusterName, env)
		Expect(err).ToNot(HaveOccurred())
		_, _, err = testsUtils.RunQueryFromPod(
			pod,
			host,
			testsUtils.AppDBName,
			appUser,
			appUserPass,
			query,
			env)
		Expect(err).ToNot(HaveOccurred())
	})
}

// insertRecordIntoTableWithDatabaseName insert an entry into a table
func insertRecordIntoTableWithDatabaseName(
	env *testsUtils.TestingEnvironment,
	namespace,
	clusterName,
	databaseName,
	tableName string,
	value int,
) {
	forward, conn, err := testsUtils.ForwardPSQLConnection(
		env,
		namespace,
		clusterName,
		databaseName,
		apiv1.ApplicationUserSecretSuffix,
	)
	defer func() {
		forward.Stop()
	}()
	Expect(err).ToNot(HaveOccurred())

	_, err = conn.Exec(fmt.Sprintf("INSERT INTO %s VALUES (%d);", tableName, value))
	Expect(err).ToNot(HaveOccurred())
}

// insertRecordIntoTable insert an entry into a table
func insertRecordIntoTable(tableName string, value int, conn *sql.DB) {
	_, err := conn.Exec(fmt.Sprintf("INSERT INTO %s VALUES (%d)", tableName, value))
	Expect(err).ToNot(HaveOccurred())
}

// AssertDatabaseExists assert if database exists
func AssertDatabaseExists(namespace, podName, databaseName string, expectedValue bool) {
	By(fmt.Sprintf("verifying if database %v exists", databaseName), func() {
		pod := &corev1.Pod{}
		commandTimeout := time.Second * 10
		query := fmt.Sprintf("SELECT EXISTS(SELECT 1 FROM pg_database WHERE lower(datname) = lower('%v'));", databaseName)
		err := env.Client.Get(env.Ctx, ctrlclient.ObjectKey{Namespace: namespace, Name: podName}, pod)
		Expect(err).ToNot(HaveOccurred())
		stdout, _, err := env.ExecCommand(env.Ctx, *pod, specs.PostgresContainerName,
			&commandTimeout, "psql", "-U", "postgres", "postgres", "-tAc", query)
		Expect(err).ToNot(HaveOccurred())
		if expectedValue {
			Expect(strings.Trim(stdout, "\n")).To(BeEquivalentTo("t"))
		} else {
			Expect(strings.Trim(stdout, "\n")).To(BeEquivalentTo("f"))
		}
	})
}

// AssertUserExists assert if user exists
func AssertUserExists(namespace, podName, userName string, expectedValue bool) {
	By(fmt.Sprintf("verifying if user %v exists", userName), func() {
		pod := &corev1.Pod{}
		commandTimeout := time.Second * 10
		query := fmt.Sprintf("SELECT EXISTS(SELECT 1 FROM pg_user WHERE lower(usename) = lower('%v'));", userName)
		err := env.Client.Get(env.Ctx, ctrlclient.ObjectKey{Namespace: namespace, Name: podName}, pod)
		Expect(err).ToNot(HaveOccurred())
		stdout, stderr, err := env.ExecCommand(env.Ctx, *pod, specs.PostgresContainerName,
			&commandTimeout, "psql", "-U", "postgres", "postgres", "-tAc", query)
		if err != nil {
			GinkgoWriter.Printf("stdout: %v\nstderr: %v", stdout, stderr)
		}
		Expect(err).ToNot(HaveOccurred())
		if expectedValue {
			Expect(strings.Trim(stdout, "\n")).To(BeEquivalentTo("t"))
		} else {
			Expect(strings.Trim(stdout, "\n")).To(BeEquivalentTo("f"))
		}
	})
}

// AssertDataExpectedCountWithDatabaseName verifies that an expected amount of rows exists on the table
func AssertDataExpectedCountWithDatabaseName(namespace, podName, databaseName string,
	tableName string, expectedValue int,
) {
	By(fmt.Sprintf("verifying test data on pod %v", podName), func() {
		query := fmt.Sprintf("select count(*) from %v", tableName)
		commandTimeout := time.Second * 10

		Eventually(func() (int, error) {
			// We keep getting the pod, since there could be a new pod with the same name
			pod := &corev1.Pod{}
			err := env.Client.Get(env.Ctx, ctrlclient.ObjectKey{Namespace: namespace, Name: podName}, pod)
			if err != nil {
				return 0, err
			}
			stdout, _, err := env.ExecCommand(env.Ctx, *pod, specs.PostgresContainerName,
				&commandTimeout, "psql", "-U", "postgres", databaseName, "-tAc", query)
			if err != nil {
				return 0, err
			}
			nRows, err := strconv.Atoi(strings.Trim(stdout, "\n"))
			return nRows, err
		}, 300).Should(BeEquivalentTo(expectedValue))
	})
}

// AssertDataExpectedCount verifies that an expected amount of rows exists on the table
func AssertDataExpectedCount(
	env *testsUtils.TestingEnvironment,
	namespace,
	clusterName,
	tableName string,
	expectedValue int,
) {
	By(fmt.Sprintf("verifying test data in table %v", tableName), func() {
		row, err := testsUtils.RunQueryRowOverForward(
			env,
			namespace,
			clusterName,
			testsUtils.AppDBName,
			apiv1.ApplicationUserSecretSuffix,
			fmt.Sprintf("SELECT COUNT(*) FROM %s", tableName),
		)
		Expect(err).ToNot(HaveOccurred())

		var nRows int
		err = row.Scan(&nRows)
		Expect(err).ToNot(HaveOccurred())
		Expect(nRows).Should(BeEquivalentTo(expectedValue))
	})
}

// AssertLargeObjectValue verifies the presence of a Large Object given by its OID and data
func AssertLargeObjectValue(namespace, clusterName string, oid int, data string, pod *corev1.Pod) {
	By("verifying large object", func() {
		query := fmt.Sprintf("SELECT encode(lo_get(%v), 'escape');", oid)
		Eventually(func() (string, error) {
			// We keep getting the pod, since there could be a new pod with the same name
			appUser, appUserPass, err := testsUtils.GetCredentials(
				clusterName, namespace, apiv1.ApplicationUserSecretSuffix, env)
			Expect(err).ToNot(HaveOccurred())
			host, err := testsUtils.GetHostName(namespace, clusterName, env)
			Expect(err).ToNot(HaveOccurred())
			stdout, _, err := testsUtils.RunQueryFromPod(
				pod,
				host,
				testsUtils.AppDBName,
				appUser,
				appUserPass,
				query,
				env)
			if err != nil {
				return "", err
			}
			return strings.Trim(stdout, "\n"), nil
		}, testTimeouts[testsUtils.LargeObject]).Should(BeEquivalentTo(data))
	})
}

// AssertClusterStandbysAreStreaming verifies that all the standbys of a cluster have a wal-receiver running.
func AssertClusterStandbysAreStreaming(namespace string, clusterName string, timeout int32) {
	Eventually(func() error {
		standbyPods, err := env.GetClusterReplicas(namespace, clusterName)
		if err != nil {
			return err
		}

		for _, pod := range standbyPods.Items {
			timeout := time.Second * 10
			out, _, err := env.EventuallyExecCommand(env.Ctx, pod, specs.PostgresContainerName, &timeout,
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
	}, timeout).ShouldNot(HaveOccurred())
}

func AssertStandbysFollowPromotion(namespace string, clusterName string, timeout int32) {
	// Track the start of the assertion. We expect to complete before
	// timeout.
	start := time.Now()

	By(fmt.Sprintf("having all the instances on timeline 2 in less than %v sec", timeout), func() {
		// One of the standbys will be promoted and the rw service
		// should point to it, so the application can keep writing.
		// Records inserted after the promotion will be marked
		// with timeline '00000002'. If all the instances are back
		// and are following the promotion, we should find those
		// records on each of them.

		commandTimeout := time.Second * 10
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
				out, _, err := env.ExecCommand(env.Ctx, *pod, specs.PostgresContainerName,
					&commandTimeout, "psql", "-U", "postgres", "app", "-tAc",
					"SELECT count(*) > 0 FROM tps.tl "+
						"WHERE timeline = '00000002'")
				return strings.TrimSpace(out), err
			}, timeout).Should(BeEquivalentTo("t"),
				"Pod %v should have moved to timeline 2", podName)
		}
	})

	By("having all the instances ready", func() {
		AssertClusterIsReady(namespace, clusterName, testTimeouts[testsUtils.ClusterIsReady], env)
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
		commandTimeout := time.Second * 10
		pod := &corev1.Pod{}
		err := env.Client.Get(env.Ctx, namespacedName, pod)
		Expect(err).ToNot(HaveOccurred())
		out, _, err := env.EventuallyExecCommand(env.Ctx, *pod, specs.PostgresContainerName,
			&commandTimeout, "psql", "-U", "postgres", "app", "-tAc", query)
		Expect(err).ToNot(HaveOccurred())
		switchTime, err = strconv.ParseFloat(strings.TrimSpace(out), 64)
		if err != nil {
			fmt.Printf("Write activity resumed in %v seconds\n", switchTime)
		}
		Expect(switchTime, err).Should(BeNumerically("<", timeout))
	})
}

// AssertNewPrimary checks that, during a failover, a new primary
// is being elected and promoted and that write operation succeed
// on this new pod.
func AssertNewPrimary(namespace string, clusterName string, oldPrimary string) {
	var newPrimaryPod string
	By(fmt.Sprintf("verifying the new primary pod, oldPrimary is %s", oldPrimary), func() {
		// Gather the primary
		timeout := 120
		// Wait for the operator to set a new TargetPrimary
		var cluster *apiv1.Cluster
		Eventually(func() (string, error) {
			var err error
			cluster, err = env.GetCluster(namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			return cluster.Status.TargetPrimary, err
		}, timeout).ShouldNot(Or(BeEquivalentTo(oldPrimary), BeEquivalentTo(apiv1.PendingFailoverMarker)))
		newPrimary := cluster.Status.TargetPrimary

		// Expect the chosen pod to eventually become a primary
		namespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      newPrimary,
		}
		Eventually(func() (bool, error) {
			pod := corev1.Pod{}
			err := env.Client.Get(env.Ctx, namespacedName, &pod)
			return specs.IsPodPrimary(pod), err
		}, timeout).Should(BeTrue())
		newPrimaryPod = newPrimary
	})
	By(fmt.Sprintf("verifying write operation on the new primary pod: %s", newPrimaryPod), func() {
		commandTimeout := time.Second * 10
		namespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      newPrimaryPod,
		}
		pod := corev1.Pod{}
		err := env.Client.Get(env.Ctx, namespacedName, &pod)
		Expect(err).ToNot(HaveOccurred())
		// Expect write operation to succeed
		query := "CREATE TABLE IF NOT EXISTS assert_new_primary(var1 text);"
		_, _, err = env.EventuallyExecCommand(env.Ctx, pod, specs.PostgresContainerName,
			&commandTimeout, "psql", "-U", "postgres", "app", "-tAc", query)
		Expect(err).ToNot(HaveOccurred())
	})
}

func AssertStorageCredentialsAreCreated(namespace string, name string, id string, key string) {
	Eventually(func() error {
		_, _, err := testsUtils.Run(fmt.Sprintf("kubectl create secret generic %v -n %v "+
			"--from-literal='ID=%v' "+
			"--from-literal='KEY=%v'",
			name, namespace, id, key))
		return err
	}, 60, 5).Should(BeNil())
}

// minioPath gets the MinIO file string for WAL/backup objects in a configured bucket
func minioPath(serverName, fileName string) string {
	// the * regexes enable matching these typical paths:
	// 	minio/backups/serverName/base/20220618T140300/data.tar
	// 	minio/backups/serverName/wals/0000000100000000/000000010000000000000002.gz
	//  minio/backups/serverName/wals/00000002.history.gz
	return filepath.Join("*", serverName, "*", fileName)
}

// CheckPointAndSwitchWalOnPrimary trigger a checkpoint and switch wal on primary pod and returns the latest WAL file
func CheckPointAndSwitchWalOnPrimary(namespace, clusterName string) string {
	var latestWAL string
	By("trigger checkpoint and switch wal on primary", func() {
		pod, err := env.GetClusterPrimary(namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		primary := pod.GetName()
		latestWAL = switchWalAndGetLatestArchive(namespace, primary)
	})
	return latestWAL
}

// AssertArchiveWalOnMinio archives WALs and verifies that they are in the storage
func AssertArchiveWalOnMinio(namespace, clusterName string, serverName string) {
	var latestWALPath string
	// Create a WAL on the primary and check if it arrives at minio, within a short time
	By("archiving WALs and verifying they exist", func() {
		pod, err := env.GetClusterPrimary(namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		primary := pod.GetName()
		latestWAL := switchWalAndGetLatestArchive(namespace, primary)
		latestWALPath = minioPath(serverName, latestWAL+".gz")
	})

	By(fmt.Sprintf("verify the existence of WAL %v in minio", latestWALPath), func() {
		Eventually(func() (int, error) {
			// WALs are compressed with gzip in the fixture
			return testsUtils.CountFilesOnMinio(minioEnv, latestWALPath)
		}, testTimeouts[testsUtils.WalsInMinio]).Should(BeEquivalentTo(1))
	})
}

func AssertScheduledBackupsAreScheduled(namespace string, backupYAMLPath string, timeout int) {
	CreateResourceFromFile(namespace, backupYAMLPath)
	scheduledBackupName, err := env.GetResourceNameFromYAML(backupYAMLPath)
	Expect(err).NotTo(HaveOccurred())

	// We expect the scheduled backup to be scheduled before a
	// timeout
	scheduledBackupNamespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      scheduledBackupName,
	}

	Eventually(func() (*v1.Time, error) {
		scheduledBackup := &apiv1.ScheduledBackup{}
		err := env.Client.Get(env.Ctx,
			scheduledBackupNamespacedName, scheduledBackup)
		return scheduledBackup.Status.LastScheduleTime, err
	}, timeout).ShouldNot(BeNil())

	// Within a few minutes we should have at least two backups
	Eventually(func() (int, error) {
		return getScheduledBackupCompleteBackupsCount(namespace, scheduledBackupName)
	}, timeout).Should(BeNumerically(">=", 2))
}

func getScheduledBackupBackups(namespace string, scheduledBackupName string) ([]apiv1.Backup, error) {
	scheduledBackupNamespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      scheduledBackupName,
	}
	// Get all the backups that are children of the ScheduledBackup
	scheduledBackup := &apiv1.ScheduledBackup{}
	err := env.Client.Get(env.Ctx, scheduledBackupNamespacedName,
		scheduledBackup)
	backups := &apiv1.BackupList{}
	if err != nil {
		return nil, err
	}
	err = env.Client.List(env.Ctx, backups,
		ctrlclient.InNamespace(namespace))
	if err != nil {
		return nil, err
	}
	var ret []apiv1.Backup

	for _, backup := range backups.Items {
		if strings.HasPrefix(backup.Name, scheduledBackup.Name+"-") {
			ret = append(ret, backup)
		}
	}
	return ret, nil
}

func getScheduledBackupCompleteBackupsCount(namespace string, scheduledBackupName string) (int, error) {
	backups, err := getScheduledBackupBackups(namespace, scheduledBackupName)
	if err != nil {
		return -1, err
	}
	completed := 0
	for _, backup := range backups {
		if strings.HasPrefix(backup.Name, scheduledBackupName+"-") &&
			backup.Status.Phase == apiv1.BackupPhaseCompleted {
			completed++
		}
	}
	return completed, nil
}

// AssertPgRecoveryMode verifies if the target pod recovery mode is enabled or disabled
func AssertPgRecoveryMode(pod *corev1.Pod, expectedValue bool) {
	By(fmt.Sprintf("verifying that postgres recovery mode is %v", expectedValue), func() {
		stringExpectedValue := "f"
		if expectedValue {
			stringExpectedValue = "t"
		}

		Eventually(func() (string, error) {
			commandTimeout := time.Second * 10
			stdOut, stdErr, err := env.ExecCommand(env.Ctx, *pod, specs.PostgresContainerName, &commandTimeout,
				"psql", "-U", "postgres", "postgres", "-tAc", "select pg_is_in_recovery();")
			if err != nil {
				GinkgoWriter.Printf("stdout: %v\ntderr: %v\n", stdOut, stdErr)
			}
			return strings.Trim(stdOut, "\n"), err
		}, 300, 10).Should(BeEquivalentTo(stringExpectedValue))
	})
}

// AssertReplicaModeCluster checks that, after inserting some data in a source cluster,
// a replica cluster can be bootstrapped using pg_basebackup and is properly replicating
// from the source cluster
func AssertReplicaModeCluster(
	namespace,
	srcClusterName,
	srcClusterDBName,
	replicaClusterSample,
	testTableName string,
) {
	var primaryReplicaCluster *corev1.Pod
	commandTimeout := time.Second * 10
	checkQuery := fmt.Sprintf("SELECT count(*) FROM %v", testTableName)

	AssertDatabaseIsReady(namespace, srcClusterName, srcClusterDBName)

	AssertCreateTestDataWithDatabaseName(env, namespace, srcClusterName, srcClusterDBName, testTableName)

	By("creating replica cluster", func() {
		replicaClusterName, err := env.GetResourceNameFromYAML(replicaClusterSample)
		Expect(err).ToNot(HaveOccurred())
		AssertCreateCluster(namespace, replicaClusterName, replicaClusterSample, env)
		// Get primary from replica cluster
		Eventually(func() error {
			primaryReplicaCluster, err = env.GetClusterPrimary(namespace, replicaClusterName)
			return err
		}, 30, 3).Should(BeNil())
		AssertPgRecoveryMode(primaryReplicaCluster, true)
	})

	By("checking data have been copied correctly in replica cluster", func() {
		Eventually(func() (string, error) {
			stdOut, _, err := env.ExecCommand(env.Ctx, *primaryReplicaCluster, specs.PostgresContainerName,
				&commandTimeout, "psql", "-U", "postgres", srcClusterDBName, "-tAc", checkQuery)
			return strings.Trim(stdOut, "\n"), err
		}, 180, 10).Should(BeEquivalentTo("2"))
	})

	By("writing some new data to the source cluster", func() {
		insertRecordIntoTableWithDatabaseName(env, namespace, srcClusterName, srcClusterDBName, testTableName, 3)
	})

	By("checking new data have been copied correctly in replica cluster", func() {
		Eventually(func() (string, error) {
			stdOut, _, err := env.ExecCommand(env.Ctx, *primaryReplicaCluster, specs.PostgresContainerName,
				&commandTimeout, "psql", "-U", "postgres", srcClusterDBName, "-tAc", checkQuery)
			return strings.Trim(stdOut, "\n"), err
		}, 180, 15).Should(BeEquivalentTo("3"))
	})

	if srcClusterDBName != "app" {
		// verify the replica database created followed the source database, rather than
		// default to the "app" db and user
		By("checking that in replica cluster there is no database app and user app", func() {
			AssertDatabaseExists(namespace, primaryReplicaCluster.Name, "app", false)
			AssertUserExists(namespace, primaryReplicaCluster.Name, "app", false)
		})
	}
}

// AssertDetachReplicaModeCluster verifies that a replica cluster can be detached from the
// source cluster, and its target primary can be promoted. As such, new write operation
// on the source cluster shouldn't be received anymore by the detached replica cluster.
// Also, make sure the boostrap fields database and owner of the replica cluster are
// properly ignored
func AssertDetachReplicaModeCluster(
	namespace,
	srcClusterName,
	srcDatabaseName,
	replicaClusterName,
	replicaDatabaseName,
	replicaUserName,
	testTableName string,
) {
	var primaryReplicaCluster *corev1.Pod
	replicaCommandTimeout := time.Second * 10

	var referenceTime time.Time
	By("taking the reference time before the detaching", func() {
		Eventually(func(g Gomega) {
			referenceCondition, err := testsUtils.GetConditionsInClusterStatus(namespace, replicaClusterName, env,
				apiv1.ConditionClusterReady)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(referenceCondition.Status).To(BeEquivalentTo(corev1.ConditionTrue))
			g.Expect(referenceCondition).ToNot(BeNil())
			referenceTime = referenceCondition.LastTransitionTime.Time
		}, 60, 5).Should(Succeed())
	})

	By("disabling the replica mode", func() {
		Eventually(func(g Gomega) {
			_, _, err := testsUtils.RunUnchecked(fmt.Sprintf(
				"kubectl patch cluster %v -n %v  -p '{\"spec\":{\"replica\":{\"enabled\":false}}}'"+
					" --type='merge'",
				replicaClusterName, namespace))
			g.Expect(err).ToNot(HaveOccurred())
		}, 60, 5).Should(Succeed())
	})

	By("ensuring the replica cluster got promoted and restarted", func() {
		Eventually(func(g Gomega) {
			cluster, err := env.GetCluster(namespace, replicaClusterName)
			g.Expect(err).ToNot(HaveOccurred())
			condition, err := testsUtils.GetConditionsInClusterStatus(namespace, cluster.Name, env,
				apiv1.ConditionClusterReady)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(condition).ToNot(BeNil())
			g.Expect(condition.Status).To(BeEquivalentTo(corev1.ConditionTrue))
			g.Expect(condition.LastTransitionTime.Time).To(BeTemporally(">", referenceTime))
		}).WithTimeout(60 * time.Second).Should(Succeed())
		AssertClusterIsReady(namespace, replicaClusterName, testTimeouts[testsUtils.ClusterIsReady], env)
	})

	By("verifying write operation on the replica cluster primary pod", func() {
		query := "CREATE TABLE IF NOT EXISTS replica_cluster_primary AS VALUES (1),(2);"
		// Expect write operation to succeed
		Eventually(func(g Gomega) {
			var err error

			// Get primary from replica cluster
			primaryReplicaCluster, err = env.GetClusterPrimary(namespace, replicaClusterName)
			g.Expect(err).ToNot(HaveOccurred())
			_, _, err = env.EventuallyExecCommand(env.Ctx, *primaryReplicaCluster, specs.PostgresContainerName,
				&replicaCommandTimeout, "psql", "-U", "postgres", srcDatabaseName, "-tAc", query)
			g.Expect(err).ToNot(HaveOccurred())
		}, 300, 15).Should(Succeed())
	})

	By("verifying the replica database doesn't exist in the replica cluster", func() {
		// Application database configuration is skipped for replica clusters,
		// so we expect these to not be present
		AssertDatabaseExists(namespace, primaryReplicaCluster.Name, replicaDatabaseName, false)
		AssertUserExists(namespace, primaryReplicaCluster.Name, replicaUserName, false)
	})

	By("writing some new data to the source cluster", func() {
		AssertCreateTestDataWithDatabaseName(env, namespace, srcClusterName, srcDatabaseName, testTableName)
	})

	By("verifying that replica cluster was not modified", func() {
		outTables, stdErr, err := env.EventuallyExecCommand(env.Ctx, *primaryReplicaCluster, specs.PostgresContainerName,
			&replicaCommandTimeout, "psql", "-U", "postgres", srcDatabaseName, "-tAc", "\\dt")
		if err != nil {
			GinkgoWriter.Printf("stdout: %v\nstderr: %v\n", outTables, stdErr)
		}
		Expect(err).ToNot(HaveOccurred())
		Expect(strings.Contains(outTables, testTableName), err).Should(BeFalse())
	})
}

func AssertWritesToReplicaFails(
	connectingPod *corev1.Pod,
	service string,
	appDBName string,
	appDBUser string,
	appDBPass string,
) {
	By(fmt.Sprintf("Verifying %v service doesn't allow writes", service),
		func() {
			timeout := time.Second * 10
			dsn := testsUtils.CreateDSN(service, appDBUser, appDBName, appDBPass, testsUtils.Require, 5432)

			// Expect to be connected to a replica
			stdout, _, err := env.EventuallyExecCommand(env.Ctx, *connectingPod, specs.PostgresContainerName, &timeout,
				"psql", dsn, "-tAc", "select pg_is_in_recovery()")
			value := strings.Trim(stdout, "\n")
			Expect(value, err).To(Equal("t"))

			// Expect to be in a read-only transaction
			_, _, err = utils.ExecCommand(env.Ctx, env.Interface, env.RestClientConfig, *connectingPod,
				specs.PostgresContainerName, &timeout,
				"psql", dsn, "-tAc", "CREATE TABLE IF NOT EXISTS table1(var1 text);")
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
	appDBPass string,
) {
	By(fmt.Sprintf("Verifying %v service correctly manages writes", service),
		func() {
			timeout := time.Second * 10
			dsn := testsUtils.CreateDSN(service, appDBUser, appDBName, appDBPass, testsUtils.Require, 5432)

			// Expect to be connected to a primary
			stdout, _, err := env.EventuallyExecCommand(env.Ctx, *connectingPod, specs.PostgresContainerName, &timeout,
				"psql", dsn, "-tAc", "select pg_is_in_recovery()")
			value := strings.Trim(stdout, "\n")
			Expect(value, err).To(Equal("f"))

			// Expect to be able to write
			_, _, err = env.EventuallyExecCommand(env.Ctx, *connectingPod, specs.PostgresContainerName, &timeout,
				"psql", dsn, "-tAc", "CREATE TABLE IF NOT EXISTS table1(var1 text);")
			Expect(err).ToNot(HaveOccurred())
		})
}

func AssertFastFailOver(
	namespace,
	sampleFile,
	clusterName,
	webTestFile,
	webTestJob string,
	maxReattachTime,
	maxFailoverTime int32,
) {
	var err error
	By(fmt.Sprintf("having a %v namespace", namespace), func() {
		// Creating a namespace should be quick
		timeout := 20
		namespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      namespace,
		}

		Eventually(func() (string, error) {
			namespaceResource := &corev1.Namespace{}
			err = env.Client.Get(env.Ctx, namespacedName, namespaceResource)
			return namespaceResource.GetName(), err
		}, timeout).Should(BeEquivalentTo(namespace))
	})

	By(fmt.Sprintf("creating a Cluster in the %v namespace", namespace), func() {
		CreateResourceFromFile(namespace, sampleFile)
	})

	By("having a Cluster with three instances ready", func() {
		AssertClusterIsReady(namespace, clusterName, testTimeouts[testsUtils.ClusterIsReady], env)
	})

	// Node 1 should be the primary, so the -rw service should
	// point there. We verify this.
	By("having the current primary on node1", func() {
		endpointName := clusterName + "-rw"
		endpoint := &corev1.Endpoints{}
		endpointNamespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      endpointName,
		}
		podName := clusterName + "-1"
		pod := &corev1.Pod{}
		podNamespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      podName,
		}
		err = env.Client.Get(env.Ctx, endpointNamespacedName,
			endpoint)
		Expect(err).ToNot(HaveOccurred())
		err = env.Client.Get(env.Ctx, podNamespacedName, pod)
		Expect(testsUtils.FirstEndpointIP(endpoint), err).To(
			BeEquivalentTo(pod.Status.PodIP))
	})

	By("preparing the db for the test scenario", func() {
		// Create the table used by the scenario
		query := "CREATE SCHEMA IF NOT EXISTS tps; " +
			"CREATE TABLE IF NOT EXISTS tps.tl ( " +
			"id BIGSERIAL" +
			", timeline TEXT DEFAULT (substring(pg_walfile_name(" +
			"    pg_current_wal_lsn()), 1, 8))" +
			", t timestamp DEFAULT (clock_timestamp() AT TIME ZONE 'UTC')" +
			", source text NOT NULL" +
			", PRIMARY KEY (id)" +
			")"

		primaryPod, err := env.GetClusterPrimary(namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		_, _, err = env.ExecCommandWithPsqlClient(
			namespace,
			clusterName,
			primaryPod,
			apiv1.ApplicationUserSecretSuffix,
			testsUtils.AppDBName,
			query,
		)
		Expect(err).ToNot(HaveOccurred())
	})

	By("starting load", func() {
		// We set up Apache Benchmark and webtest. Apache Benchmark, a load generator,
		// continuously calls the webtest api to execute inserts
		// on the postgres primary. We make sure that the first
		// records appear on the database before moving to the next
		// step.
		_, _, err = testsUtils.Run("kubectl create -n " + namespace +
			" -f " + webTestFile)
		Expect(err).ToNot(HaveOccurred())

		_, _, err = testsUtils.Run("kubectl create -n " + namespace +
			" -f " + webTestJob)
		Expect(err).ToNot(HaveOccurred())

		commandTimeout := time.Second * 10
		timeout := 60
		primaryPodName := clusterName + "-1"
		primaryPodNamespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      primaryPodName,
		}

		Eventually(func() (string, error) {
			primaryPod := &corev1.Pod{}
			err = env.Client.Get(env.Ctx, primaryPodNamespacedName, primaryPod)
			if err != nil {
				return "", err
			}
			out, _, err := env.ExecCommand(env.Ctx, *primaryPod, specs.PostgresContainerName,
				&commandTimeout, "psql", "-U", "postgres", "app", "-tAc",
				"SELECT count(*) > 0 FROM tps.tl")
			return strings.TrimSpace(out), err
		}, timeout).Should(BeEquivalentTo("t"))
	})

	By("deleting the primary", func() {
		// The primary is force-deleted.
		quickDelete := &ctrlclient.DeleteOptions{
			GracePeriodSeconds: &quickDeletionPeriod,
		}
		lm := clusterName + "-1"
		err = env.DeletePod(namespace, lm, quickDelete)
		Expect(err).ToNot(HaveOccurred())
	})

	AssertStandbysFollowPromotion(namespace, clusterName, maxReattachTime)

	AssertWritesResumedBeforeTimeout(namespace, clusterName, maxFailoverTime)
}

func AssertCustomMetricsResourcesExist(namespace, sampleFile string, configMapsCount, secretsCount int) {
	By("verifying the custom metrics ConfigMaps and Secrets exist", func() {
		// Create the ConfigMaps and a Secret
		_, _, err := testsUtils.Run("kubectl apply -n " + namespace + " -f " + sampleFile)
		Expect(err).ToNot(HaveOccurred())

		// Check configmaps exist
		timeout := 20
		Eventually(func() ([]corev1.ConfigMap, error) {
			cmList := &corev1.ConfigMapList{}
			err = env.Client.List(
				env.Ctx, cmList, ctrlclient.InNamespace(namespace),
				ctrlclient.MatchingLabels{"e2e": "metrics"},
			)
			return cmList.Items, err
		}, timeout).Should(HaveLen(configMapsCount))

		// Check secret exists
		Eventually(func() ([]corev1.Secret, error) {
			secretList := &corev1.SecretList{}
			err = env.Client.List(
				env.Ctx, secretList, ctrlclient.InNamespace(namespace),
				ctrlclient.MatchingLabels{"e2e": "metrics"},
			)
			return secretList.Items, err
		}, timeout).Should(HaveLen(secretsCount))
	})
}

func AssertCreationOfTestDataForTargetDB(
	env *testsUtils.TestingEnvironment,
	namespace,
	clusterName,
	targetDBName,
	tableName string,
) {
	By(fmt.Sprintf("creating target database '%v' and table '%v'", targetDBName, tableName), func() {
		// We need to gather the cluster primary to create the database via superuser
		currentPrimary, err := env.GetClusterPrimary(namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())

		appUser, _, err := testsUtils.GetCredentials(clusterName, namespace, apiv1.ApplicationUserSecretSuffix, env)
		Expect(err).ToNot(HaveOccurred())

		// Create database
		commandTimeout := time.Second * 10
		createDBQuery := fmt.Sprintf("CREATE DATABASE %v OWNER %v", targetDBName, appUser)
		_, _, err = env.ExecCommand(
			env.Ctx,
			*currentPrimary,
			specs.PostgresContainerName,
			&commandTimeout,
			"psql", "-U", "postgres", "-tAc", createDBQuery,
		)
		Expect(err).ToNot(HaveOccurred())

		forward, conn, err := testsUtils.ForwardPSQLConnection(
			env,
			namespace,
			clusterName,
			targetDBName,
			apiv1.ApplicationUserSecretSuffix,
		)
		Expect(err).ToNot(HaveOccurred())

		// Create table on target database
		createTableQuery := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %v (id int);", tableName)
		_, err = conn.Exec(createTableQuery)
		Expect(err).ToNot(HaveOccurred())

		// Grant a permission
		grantRoleQuery := "GRANT SELECT ON all tables in schema public to pg_monitor;"
		_, err = conn.Exec(grantRoleQuery)
		Expect(err).ToNot(HaveOccurred())

		// Close the connection and forward
		forward.Stop()
	})
}

// AssertApplicationDatabaseConnection check the connectivity of application database
func AssertApplicationDatabaseConnection(
	namespace,
	clusterName,
	appUser,
	appDB,
	appPassword,
	appSecretName string,
	pod *corev1.Pod,
) {
	By("checking cluster can connect with application database user and password", func() {
		// Get the app user password from the auto generated -app secret if appPassword is not provided
		if appPassword == "" {
			if appSecretName == "" {
				appSecretName = clusterName + "-app"
			}
			appSecret := &corev1.Secret{}
			appSecretNamespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      appSecretName,
			}
			err := env.Client.Get(env.Ctx, appSecretNamespacedName, appSecret)
			Expect(err).ToNot(HaveOccurred())
			appPassword = string(appSecret.Data["password"])
		}
		// rwService := fmt.Sprintf("%v-rw.%v.svc", clusterName, namespace)
		rwService := testsUtils.CreateServiceFQDN(namespace, testsUtils.GetReadWriteServiceName(clusterName))

		AssertConnection(rwService, appUser, appDB, appPassword, pod, 60, env)
	})
}

func AssertMetricsData(namespace, targetOne, targetTwo, targetSecret string, cluster *apiv1.Cluster) {
	By("collect and verify metric being exposed with target databases", func() {
		podList, err := env.GetClusterPodList(namespace, cluster.Name)
		Expect(err).ToNot(HaveOccurred())
		for _, pod := range podList.Items {
			podName := pod.GetName()
			out, err := testsUtils.RetrieveMetricsFromInstance(env, pod, cluster.IsMetricsTLSEnabled())
			Expect(err).ToNot(HaveOccurred())
			Expect(strings.Contains(out, fmt.Sprintf(`cnpg_some_query_rows{datname="%v"} 0`, targetOne))).Should(BeTrue(),
				"Metric collection issues on %v.\nCollected metrics:\n%v", podName, out)
			Expect(strings.Contains(out, fmt.Sprintf(`cnpg_some_query_rows{datname="%v"} 0`, targetTwo))).Should(BeTrue(),
				"Metric collection issues on %v.\nCollected metrics:\n%v", podName, out)
			Expect(strings.Contains(out, fmt.Sprintf(`cnpg_some_query_test_rows{datname="%v"} 1`,
				targetSecret))).Should(BeTrue(),
				"Metric collection issues on %v.\nCollected metrics:\n%v", podName, out)

			if pod.Name != cluster.Status.CurrentPrimary {
				continue
			}

			Expect(out).Should(ContainSubstring("last_available_backup_timestamp"))
			Expect(out).Should(ContainSubstring("last_failed_backup_timestamp"))
		}
	})
}

func CreateAndAssertServerCertificatesSecrets(
	namespace, clusterName, caSecName, tlsSecName string, includeCAPrivateKey bool,
) {
	cluster, caPair, err := testsUtils.CreateSecretCA(namespace, clusterName, caSecName, includeCAPrivateKey, env)
	Expect(err).ToNot(HaveOccurred())

	serverPair, err := caPair.CreateAndSignPair(cluster.GetServiceReadWriteName(), certs.CertTypeServer,
		cluster.GetClusterAltDNSNames(),
	)
	Expect(err).ToNot(HaveOccurred())
	serverSecret := serverPair.GenerateCertificateSecret(namespace, tlsSecName)
	err = env.Client.Create(env.Ctx, serverSecret)
	Expect(err).ToNot(HaveOccurred())
}

func CreateAndAssertClientCertificatesSecrets(
	namespace, clusterName, caSecName, tlsSecName, userSecName string, includeCAPrivateKey bool,
) {
	_, caPair, err := testsUtils.CreateSecretCA(namespace, clusterName, caSecName, includeCAPrivateKey, env)
	Expect(err).ToNot(HaveOccurred())

	// Sign tls certificates for streaming_replica user
	serverPair, err := caPair.CreateAndSignPair("streaming_replica", certs.CertTypeClient, nil)
	Expect(err).ToNot(HaveOccurred())

	serverSecret := serverPair.GenerateCertificateSecret(namespace, tlsSecName)
	err = env.Client.Create(env.Ctx, serverSecret)
	Expect(err).ToNot(HaveOccurred())

	// Creating 'app' user tls certificates to validate connection from psql client
	serverPair, err = caPair.CreateAndSignPair("app", certs.CertTypeClient, nil)
	Expect(err).ToNot(HaveOccurred())

	serverSecret = serverPair.GenerateCertificateSecret(namespace, userSecName)
	err = env.Client.Create(env.Ctx, serverSecret)
	Expect(err).ToNot(HaveOccurred())
}

func AssertSSLVerifyFullDBConnectionFromAppPod(namespace string, clusterName string, appPod corev1.Pod) {
	By("creating an app Pod and connecting to DB, using Certificate authentication", func() {
		// Connecting to DB, using Certificate authentication
		Eventually(func() (string, string, error) {
			dsn := fmt.Sprintf("host=%v-rw.%v.svc port=5432 "+
				"sslkey=/etc/secrets/tls/tls.key "+
				"sslcert=/etc/secrets/tls/tls.crt "+
				"sslrootcert=/etc/secrets/ca/ca.crt "+
				"dbname=app user=app sslmode=verify-full", clusterName, namespace)
			timeout := time.Second * 10
			stdout, stderr, err := env.ExecCommand(env.Ctx, appPod, appPod.Spec.Containers[0].Name, &timeout,
				"psql", dsn, "-tAc", "SELECT 1")
			return stdout, stderr, err
		}, 360).Should(BeEquivalentTo("1\n"))
	})
}

func AssertCreateSASTokenCredentials(namespace string, id string, key string) {
	// Adding 24 hours to the current time
	date := time.Now().UTC().Add(time.Hour * 24)
	// Creating date time format for az command
	expiringDate := fmt.Sprintf("%v"+"-"+"%d"+"-"+"%v"+"T"+"%v"+":"+"%v"+"Z",
		date.Year(),
		date.Month(),
		date.Day(),
		date.Hour(),
		date.Minute())

	out, _, err := testsUtils.Run(fmt.Sprintf(
		// SAS Token at Blob Container level does not currently work in Barman Cloud
		// https://github.com/EnterpriseDB/barman/issues/388
		// we will use SAS Token at Storage Account level
		// ( "az storage container generate-sas --account-name %v "+
		// "--name %v "+
		// "--https-only --permissions racwdl --auth-mode key --only-show-errors "+
		// "--expiry \"$(date -u -d \"+4 hours\" '+%%Y-%%m-%%dT%%H:%%MZ')\"",
		// id, blobContainerName )
		"az storage account generate-sas --account-name %v "+
			"--https-only --permissions cdlruwap --account-key %v "+
			"--resource-types co --services b --expiry %v -o tsv",
		id, key, expiringDate))
	Expect(err).ToNot(HaveOccurred())
	SASTokenRW := strings.TrimRight(out, "\n")

	out, _, err = testsUtils.Run(fmt.Sprintf(
		"az storage account generate-sas --account-name %v "+
			"--https-only --permissions lr --account-key %v "+
			"--resource-types co --services b --expiry %v -o tsv",
		id, key, expiringDate))
	Expect(err).ToNot(HaveOccurred())
	SASTokenRO := strings.TrimRight(out, "\n")

	AssertROSASTokenUnableToWrite("restore-cluster-sas", id, SASTokenRO)

	AssertStorageCredentialsAreCreated(namespace, "backup-storage-creds-sas", id, SASTokenRW)
	AssertStorageCredentialsAreCreated(namespace, "restore-storage-creds-sas", id, SASTokenRO)
}

func AssertROSASTokenUnableToWrite(containerName string, id string, key string) {
	_, _, err := testsUtils.RunUnchecked(fmt.Sprintf("az storage container create "+
		"--name %v --account-name %v "+
		"--sas-token %v", containerName, id, key))
	Expect(err).To(HaveOccurred())
}

func AssertClusterAsyncReplica(namespace, sourceClusterFile, restoreClusterFile, tableName string) {
	By("Async Replication into external cluster", func() {
		restoredClusterName, err := env.GetResourceNameFromYAML(restoreClusterFile)
		Expect(err).ToNot(HaveOccurred())
		// Add additional data to the source cluster
		sourceClusterName, err := env.GetResourceNameFromYAML(sourceClusterFile)
		Expect(err).ToNot(HaveOccurred())
		CreateResourceFromFile(namespace, restoreClusterFile)
		// We give more time than the usual 600s, since the recovery is slower
		AssertClusterIsReady(namespace, restoredClusterName, testTimeouts[testsUtils.ClusterIsReadySlow], env)

		// Test data should be present on restored primary
		restoredPrimary, err := env.GetClusterPrimary(namespace, restoredClusterName)
		Expect(err).ToNot(HaveOccurred())

		// We need the credentials from the source cluster because the replica cluster
		// doesn't create the credentials on it's own namespace
		appUser, appUserPass, err := testsUtils.GetCredentials(
			sourceClusterName,
			namespace,
			apiv1.ApplicationUserSecretSuffix,
			env,
		)
		Expect(err).ToNot(HaveOccurred())

		forwardRestored, connRestored, err := testsUtils.ForwardPSQLConnectionWithCreds(
			env,
			namespace,
			restoredClusterName,
			testsUtils.AppDBName,
			appUser,
			appUserPass,
		)
		defer func() {
			_ = connRestored.Close()
			forwardRestored.Stop()
		}()
		Expect(err).ToNot(HaveOccurred())

		row := connRestored.QueryRow(fmt.Sprintf("SELECT count(*) FROM %s", tableName))
		var countString string
		err = row.Scan(&countString)
		Expect(err).ToNot(HaveOccurred())
		Expect(countString).To(BeEquivalentTo("2"))

		forwardSource, connSource, err := testsUtils.ForwardPSQLConnection(
			env,
			namespace,
			sourceClusterName,
			testsUtils.AppDBName,
			apiv1.ApplicationUserSecretSuffix,
		)
		defer func() {
			_ = connSource.Close()
			forwardSource.Stop()
		}()
		Expect(err).ToNot(HaveOccurred())

		// Insert new data in the source cluster
		insertRecordIntoTable(tableName, 3, connSource)
		AssertArchiveWalOnMinio(namespace, sourceClusterName, sourceClusterName)
		AssertDataExpectedCount(env, namespace, sourceClusterName, tableName, 3)

		cluster, err := env.GetCluster(namespace, restoredClusterName)
		Expect(err).ToNot(HaveOccurred())
		expectedReplicas := cluster.Spec.Instances - 1
		// Cascading replicas should be attached to primary replica
		connectedReplicas, err := testsUtils.CountReplicas(env, restoredPrimary)
		Expect(connectedReplicas, err).To(BeEquivalentTo(expectedReplicas))
	})
}

func AssertClusterRestoreWithApplicationDB(namespace, restoreClusterFile, tableName string) {
	restoredClusterName, err := env.GetResourceNameFromYAML(restoreClusterFile)
	Expect(err).ToNot(HaveOccurred())

	By("Restoring a backup in a new cluster", func() {
		CreateResourceFromFile(namespace, restoreClusterFile)

		// We give more time than the usual 600s, since the recovery is slower
		AssertClusterIsReady(namespace, restoredClusterName, testTimeouts[testsUtils.ClusterIsReadySlow], env)

		// Test data should be present on restored primary
		AssertDataExpectedCount(env, namespace, restoredClusterName, tableName, 2)
	})

	By("Ensuring the restored cluster is on timeline 2", func() {
		row, err := testsUtils.RunQueryRowOverForward(
			env,
			namespace,
			restoredClusterName,
			testsUtils.AppDBName,
			apiv1.ApplicationUserSecretSuffix,
			"SELECT substring(pg_walfile_name(pg_current_wal_lsn()), 1, 8)",
		)
		Expect(err).ToNot(HaveOccurred())

		var timeline string
		err = row.Scan(&timeline)
		Expect(err).ToNot(HaveOccurred())
		Expect(timeline).To(BeEquivalentTo("00000002"))
	})

	// Restored standby should be attached to restored primary
	AssertClusterStandbysAreStreaming(namespace, restoredClusterName, 120)

	// Gather Credentials
	appUser, appUserPass, err := testsUtils.GetCredentials(restoredClusterName, namespace,
		apiv1.ApplicationUserSecretSuffix, env)
	Expect(err).ToNot(HaveOccurred())
	secretName := restoredClusterName + apiv1.ApplicationUserSecretSuffix

	By("checking the restored cluster with pre-defined app password connectable", func() {
		primaryPod, err := env.GetClusterPrimary(namespace, restoredClusterName)
		Expect(err).ToNot(HaveOccurred())
		AssertApplicationDatabaseConnection(
			namespace,
			restoredClusterName,
			appUser,
			testsUtils.AppDBName,
			appUserPass,
			secretName,
			primaryPod)
	})

	By("update user application password for restored cluster and verify connectivity", func() {
		const newPassword = "eeh2Zahohx" //nolint:gosec
		AssertUpdateSecret("password", newPassword, secretName, namespace, restoredClusterName, 30, env)

		primaryPod, err := env.GetClusterPrimary(namespace, restoredClusterName)
		Expect(err).ToNot(HaveOccurred())

		AssertApplicationDatabaseConnection(
			namespace,
			restoredClusterName,
			appUser,
			testsUtils.AppDBName,
			newPassword,
			secretName,
			primaryPod)
	})
}

func AssertClusterRestore(namespace, restoreClusterFile, tableName string) {
	restoredClusterName, err := env.GetResourceNameFromYAML(restoreClusterFile)
	Expect(err).ToNot(HaveOccurred())

	By("Restoring a backup in a new cluster", func() {
		CreateResourceFromFile(namespace, restoreClusterFile)

		// We give more time than the usual 600s, since the recovery is slower
		AssertClusterIsReady(namespace, restoredClusterName, testTimeouts[testsUtils.ClusterIsReadySlow], env)

		// Test data should be present on restored primary
		primary := restoredClusterName + "-1"
		AssertDataExpectedCount(env, namespace, restoredClusterName, tableName, 2)

		// Restored primary should be on timeline 2
		out, _, err := env.ExecQueryInInstancePod(
			testsUtils.PodLocator{
				Namespace: namespace,
				PodName:   primary,
			},
			testsUtils.DatabaseName("app"),
			"select substring(pg_walfile_name(pg_current_wal_lsn()), 1, 8)")
		Expect(strings.Trim(out, "\n"), err).To(Equal("00000002"))

		// Restored standby should be attached to restored primary
		AssertClusterStandbysAreStreaming(namespace, restoredClusterName, 120)
	})
}

// AssertClusterImport imports a database into a new cluster, and verifies that
// the new cluster is functioning properly
func AssertClusterImport(namespace, clusterWithExternalClusterName, clusterName, databaseName string) *apiv1.Cluster {
	var cluster *apiv1.Cluster
	By("Importing Database in a new cluster", func() {
		var err error
		cluster, err = testsUtils.ImportDatabaseMicroservice(namespace, clusterName,
			clusterWithExternalClusterName, "", databaseName, env)
		Expect(err).ToNot(HaveOccurred())
		// We give more time than the usual 600s, since the recovery is slower
		AssertClusterIsReady(namespace, clusterWithExternalClusterName,
			testTimeouts[testsUtils.ClusterIsReadySlow], env)
		// Restored standby should be attached to restored primary
		AssertClusterStandbysAreStreaming(namespace, clusterWithExternalClusterName, 120)
	})
	return cluster
}

func AssertScheduledBackupsImmediate(namespace, backupYAMLPath, scheduledBackupName string) {
	By("scheduling immediate backups", func() {
		var err error
		// Create the ScheduledBackup
		CreateResourceFromFile(namespace, backupYAMLPath)

		// We expect the scheduled backup to be scheduled after creation
		scheduledBackupNamespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      scheduledBackupName,
		}
		Eventually(func() (*v1.Time, error) {
			scheduledBackup := &apiv1.ScheduledBackup{}
			err = env.Client.Get(env.Ctx,
				scheduledBackupNamespacedName, scheduledBackup)
			return scheduledBackup.Status.LastScheduleTime, err
		}, 30).ShouldNot(BeNil())

		// The immediate backup fixtures has crontabs that hardly ever run
		// The only backup that we get should be the immediate one
		Eventually(func() (int, error) {
			currentBackupCount, err := getScheduledBackupCompleteBackupsCount(namespace, scheduledBackupName)
			if err != nil {
				return 0, err
			}
			return currentBackupCount, err
		}, 120).Should(BeNumerically("==", 1))
	})
}

func AssertSuspendScheduleBackups(namespace, scheduledBackupName string) {
	var completedBackupsCount int
	var err error
	By("suspending the scheduled backup", func() {
		// update suspend status to true
		Eventually(func() error {
			cmd := fmt.Sprintf("kubectl patch ScheduledBackup %v -n %v -p '{\"spec\":{\"suspend\":true}}' "+
				"--type='merge'", scheduledBackupName, namespace)
			_, _, err = testsUtils.RunUnchecked(cmd)
			if err != nil {
				return err
			}
			return nil
		}, 60, 5).Should(BeNil())
		scheduledBackupNamespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      scheduledBackupName,
		}
		Eventually(func() bool {
			scheduledBackup := &apiv1.ScheduledBackup{}
			err = env.Client.Get(env.Ctx, scheduledBackupNamespacedName, scheduledBackup)
			return *scheduledBackup.Spec.Suspend
		}, 30).Should(BeTrue())
	})
	By("waiting for ongoing backups to complete", func() {
		// After suspending, new backups shouldn't start.
		// If there are running backups they had already started,
		// and we give them some time to finish.
		Eventually(func() (bool, error) {
			completedBackupsCount, err = getScheduledBackupCompleteBackupsCount(namespace, scheduledBackupName)
			if err != nil {
				return false, err
			}
			backups, err := getScheduledBackupBackups(namespace, scheduledBackupName)
			if err != nil {
				return false, err
			}
			return len(backups) == completedBackupsCount, nil
		}, 80).Should(BeTrue())
	})
	By("verifying backup has suspended", func() {
		Consistently(func() (int, error) {
			backups, err := getScheduledBackupBackups(namespace, scheduledBackupName)
			if err != nil {
				return 0, err
			}
			return len(backups), err
		}, 80).Should(BeEquivalentTo(completedBackupsCount))
	})
	By("resuming suspended backup", func() {
		// take current backup count before suspend the schedule backup
		completedBackupsCount, err = getScheduledBackupCompleteBackupsCount(namespace, scheduledBackupName)
		Expect(err).ToNot(HaveOccurred())
		Eventually(func() error {
			cmd := fmt.Sprintf("kubectl patch ScheduledBackup %v -n %v -p '{\"spec\":{\"suspend\":false}}' "+
				"--type='merge'", scheduledBackupName, namespace)
			_, _, err = testsUtils.RunUnchecked(cmd)
			if err != nil {
				return err
			}
			return nil
		}, 60, 5).Should(BeNil())
		scheduledBackupNamespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      scheduledBackupName,
		}
		Eventually(func() bool {
			scheduledBackup := &apiv1.ScheduledBackup{}
			err = env.Client.Get(env.Ctx, scheduledBackupNamespacedName, scheduledBackup)
			return *scheduledBackup.Spec.Suspend
		}, 30).Should(BeFalse())
	})
	By("verifying backup has resumed", func() {
		Eventually(func() (int, error) {
			currentBackupCount, err := getScheduledBackupCompleteBackupsCount(namespace, scheduledBackupName)
			if err != nil {
				return 0, err
			}
			return currentBackupCount, err
		}, 180).Should(BeNumerically(">", completedBackupsCount))
	})
}

func AssertClusterWasRestoredWithPITRAndApplicationDB(namespace, clusterName, tableName, lsn string) {
	// We give more time than the usual 600s, since the recovery is slower
	AssertClusterIsReady(namespace, clusterName, testTimeouts[testsUtils.ClusterIsReadySlow], env)

	// Gather the recovered cluster primary
	primaryInfo, err := env.GetClusterPrimary(namespace, clusterName)
	Expect(err).ToNot(HaveOccurred())
	secretName := clusterName + apiv1.ApplicationUserSecretSuffix

	By("Ensuring the restored cluster is on timeline 3", func() {
		// Restored primary should be on timeline 3
		row, err := testsUtils.RunQueryRowOverForward(
			env,
			namespace,
			clusterName,
			testsUtils.AppDBName,
			apiv1.ApplicationUserSecretSuffix,
			"select substring(pg_walfile_name(pg_current_wal_lsn()), 1, 8)",
		)
		Expect(err).ToNot(HaveOccurred())

		var currentWalLsn string
		err = row.Scan(&currentWalLsn)
		Expect(err).ToNot(HaveOccurred())
		Expect(currentWalLsn).To(Equal(lsn))

		// Restored standby should be attached to restored primary
		Expect(testsUtils.CountReplicas(env, primaryInfo)).To(BeEquivalentTo(2))
	})

	By(fmt.Sprintf("after restored, 3rd entry should not be exists in table '%v'", tableName), func() {
		// Only 2 entries should be present
		AssertDataExpectedCount(env, namespace, clusterName, tableName, 2)
	})

	// Gather credentials
	appUser, appUserPass, err := testsUtils.GetCredentials(clusterName, namespace, apiv1.ApplicationUserSecretSuffix, env)
	Expect(err).ToNot(HaveOccurred())

	primaryPod, err := env.GetClusterPrimary(namespace, clusterName)
	Expect(err).ToNot(HaveOccurred())

	By("checking the restored cluster with auto generated app password connectable", func() {
		AssertApplicationDatabaseConnection(
			namespace,
			clusterName,
			appUser,
			testsUtils.AppDBName,
			appUserPass,
			secretName,
			primaryPod)
	})

	By("update user application password for restored cluster and verify connectivity", func() {
		const newPassword = "eeh2Zahohx" //nolint:gosec
		AssertUpdateSecret("password", newPassword, secretName, namespace, clusterName, 30, env)
		AssertApplicationDatabaseConnection(
			namespace,
			clusterName,
			appUser,
			testsUtils.AppDBName,
			newPassword,
			secretName,
			primaryPod)
	})
}

func AssertClusterWasRestoredWithPITR(namespace, clusterName, tableName, lsn string) {
	By("restoring a backup cluster with PITR in a new cluster", func() {
		// We give more time than the usual 600s, since the recovery is slower
		AssertClusterIsReady(namespace, clusterName, testTimeouts[testsUtils.ClusterIsReadySlow], env)
		primaryInfo, err := env.GetClusterPrimary(namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())

		// Restored primary should be on timeline 3
		row, err := testsUtils.RunQueryRowOverForward(
			env,
			namespace,
			clusterName,
			testsUtils.AppDBName,
			apiv1.ApplicationUserSecretSuffix,
			"select substring(pg_walfile_name(pg_current_wal_lsn()), 1, 8)",
		)
		Expect(err).ToNot(HaveOccurred())

		var currentWalLsn string
		err = row.Scan(&currentWalLsn)
		Expect(err).ToNot(HaveOccurred())
		Expect(currentWalLsn).To(Equal(lsn))

		// Restored standby should be attached to restored primary
		Expect(testsUtils.CountReplicas(env, primaryInfo)).To(BeEquivalentTo(2))
	})

	By(fmt.Sprintf("after restored, 3rd entry should not be exists in table '%v'", tableName), func() {
		// Only 2 entries should be present
		AssertDataExpectedCount(env, namespace, clusterName, tableName, 2)
	})
}

func AssertArchiveConditionMet(namespace, clusterName, timeout string) {
	By("Waiting for the condition", func() {
		out, _, err := testsUtils.Run(fmt.Sprintf(
			"kubectl -n %s wait --for=condition=ContinuousArchiving=true cluster/%s --timeout=%s",
			namespace, clusterName, timeout))
		Expect(err).ToNot(HaveOccurred())
		outPut := strings.TrimSpace(out)
		Expect(outPut).Should(ContainSubstring("condition met"))
	})
}

func AssertArchiveWalOnAzurite(namespace, clusterName string) {
	// Create a WAL on the primary and check if it arrives at the Azure Blob Storage within a short time
	By("archiving WALs and verifying they exist", func() {
		primary := clusterName + "-1"
		latestWAL := switchWalAndGetLatestArchive(namespace, primary)
		// verifying on blob storage using az
		// Define what file we are looking for in Azurite.
		// Escapes are required since az expects forward slashes to be escaped
		path := fmt.Sprintf("%v\\/wals\\/0000000100000000\\/%v.gz", clusterName, latestWAL)
		// verifying on blob storage using az
		Eventually(func() (int, error) {
			return testsUtils.CountFilesOnAzuriteBlobStorage(namespace, clusterName, path)
		}, 60).Should(BeEquivalentTo(1))
	})
}

func AssertArchiveWalOnAzureBlob(namespace, clusterName string, configuration testsUtils.AzureConfiguration) {
	// Create a WAL on the primary and check if it arrives at the Azure Blob Storage, within a short time
	By("archiving WALs and verifying they exist", func() {
		primary, err := env.GetClusterPrimary(namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		latestWAL := switchWalAndGetLatestArchive(primary.Namespace, primary.Name)
		// Define what file we are looking for in Azure.
		// Escapes are required since az expects forward slashes to be escaped
		path := fmt.Sprintf("wals\\/0000000100000000\\/%v.gz", latestWAL)
		// Verifying on blob storage using az
		Eventually(func() (int, error) {
			return testsUtils.CountFilesOnAzureBlobStorage(configuration, clusterName, path)
		}, 60).Should(BeEquivalentTo(1))
	})
}

// switchWalAndGetLatestArchive trigger a new wal and get the name of latest wal file
func switchWalAndGetLatestArchive(namespace, podName string) string {
	_, _, err := env.ExecQueryInInstancePod(
		testsUtils.PodLocator{
			Namespace: namespace,
			PodName:   podName,
		},
		testsUtils.DatabaseName("postgres"),
		"CHECKPOINT;")
	Expect(err).ToNot(HaveOccurred())

	out, _, err := env.ExecQueryInInstancePod(
		testsUtils.PodLocator{
			Namespace: namespace,
			PodName:   podName,
		},
		testsUtils.DatabaseName("postgres"),
		"SELECT pg_walfile_name(pg_switch_wal());")
	Expect(err).ToNot(HaveOccurred())

	return strings.TrimSpace(out)
}

func prepareClusterForPITROnMinio(
	namespace,
	clusterName,
	backupSampleFile string,
	expectedVal int,
	currentTimestamp *string,
) {
	const tableNamePitr = "for_restore"

	By("backing up a cluster and verifying it exists on minio", func() {
		testsUtils.ExecuteBackup(namespace, backupSampleFile, false, testTimeouts[testsUtils.BackupIsReady], env)
		latestTar := minioPath(clusterName, "data.tar")
		Eventually(func() (int, error) {
			return testsUtils.CountFilesOnMinio(minioEnv, latestTar)
		}, 60).Should(BeNumerically(">=", expectedVal),
			fmt.Sprintf("verify the number of backups %v is greater than or equal to %v", latestTar,
				expectedVal))
		Eventually(func() (string, error) {
			cluster, err := env.GetCluster(namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			return cluster.Status.FirstRecoverabilityPoint, err
		}, 30).ShouldNot(BeEmpty())
	})

	// Write a table and insert 2 entries on the "app" database
	AssertCreateTestData(env, namespace, clusterName, tableNamePitr)

	By("getting currentTimestamp", func() {
		ts, err := testsUtils.GetCurrentTimestamp(namespace, clusterName, env)
		*currentTimestamp = ts
		Expect(err).ToNot(HaveOccurred())
	})

	By(fmt.Sprintf("writing 3rd entry into test table '%v'", tableNamePitr), func() {
		forward, conn, err := testsUtils.ForwardPSQLConnection(
			env,
			namespace,
			clusterName,
			testsUtils.AppDBName,
			apiv1.ApplicationUserSecretSuffix,
		)
		defer func() {
			forward.Stop()
		}()
		Expect(err).ToNot(HaveOccurred())

		insertRecordIntoTable(tableNamePitr, 3, conn)
	})
	AssertArchiveWalOnMinio(namespace, clusterName, clusterName)
	AssertArchiveConditionMet(namespace, clusterName, "5m")
	AssertBackupConditionInClusterStatus(namespace, clusterName)
}

func prepareClusterForPITROnAzureBlob(
	namespace string,
	clusterName string,
	backupSampleFile string,
	azureConfig testsUtils.AzureConfiguration,
	expectedVal int,
	currentTimestamp *string,
) {
	const tableNamePitr = "for_restore"
	By("backing up a cluster and verifying it exists on Azure Blob", func() {
		testsUtils.ExecuteBackup(namespace, backupSampleFile, false, testTimeouts[testsUtils.BackupIsReady], env)

		Eventually(func() (int, error) {
			return testsUtils.CountFilesOnAzureBlobStorage(azureConfig, clusterName, "data.tar")
		}, 30).Should(BeEquivalentTo(expectedVal))
		Eventually(func() (string, error) {
			cluster, err := env.GetCluster(namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			return cluster.Status.FirstRecoverabilityPoint, err
		}, 30).ShouldNot(BeEmpty())
	})

	// Write a table and insert 2 entries on the "app" database
	AssertCreateTestData(env, namespace, clusterName, tableNamePitr)

	By("getting currentTimestamp", func() {
		ts, err := testsUtils.GetCurrentTimestamp(namespace, clusterName, env)
		*currentTimestamp = ts
		Expect(err).ToNot(HaveOccurred())
	})

	By(fmt.Sprintf("writing 3rd entry into test table '%v'", tableNamePitr), func() {
		forward, conn, err := testsUtils.ForwardPSQLConnection(
			env,
			namespace,
			clusterName,
			testsUtils.AppDBName,
			apiv1.ApplicationUserSecretSuffix,
		)
		defer func() {
			forward.Stop()
		}()
		Expect(err).ToNot(HaveOccurred())
		insertRecordIntoTable(tableNamePitr, 3, conn)
	})
	AssertArchiveWalOnAzureBlob(namespace, clusterName, env.AzureConfiguration)
	AssertArchiveConditionMet(namespace, clusterName, "5m")
	AssertBackupConditionInClusterStatus(namespace, clusterName)
}

func prepareClusterOnAzurite(namespace, clusterName, clusterSampleFile string) {
	By("creating the Azurite storage credentials", func() {
		err := testsUtils.CreateStorageCredentialsOnAzurite(namespace, env)
		Expect(err).ToNot(HaveOccurred())
	})

	By("setting up Azurite to hold the backups", func() {
		// Deploying azurite for blob storage
		err := testsUtils.InstallAzurite(namespace, env)
		Expect(err).ToNot(HaveOccurred())
	})

	By("setting up az-cli", func() {
		// This is required as we have a service of Azurite running locally.
		// In order to connect, we need az cli inside the namespace
		err := testsUtils.InstallAzCli(namespace, env)
		Expect(err).ToNot(HaveOccurred())
	})

	// Creating cluster
	AssertCreateCluster(namespace, clusterName, clusterSampleFile, env)

	AssertArchiveConditionMet(namespace, clusterName, "5m")
}

func prepareClusterBackupOnAzurite(
	namespace,
	clusterName,
	clusterSampleFile,
	backupFile,
	tableName string,
) {
	// Setting up Azurite and az cli along with Postgresql cluster
	prepareClusterOnAzurite(namespace, clusterName, clusterSampleFile)
	// Write a table and some data on the "app" database
	AssertCreateTestData(env, namespace, clusterName, tableName)
	AssertArchiveWalOnAzurite(namespace, clusterName)

	By("backing up a cluster and verifying it exists on azurite", func() {
		// We create a Backup
		testsUtils.ExecuteBackup(namespace, backupFile, false, testTimeouts[testsUtils.BackupIsReady], env)
		// Verifying file called data.tar should be available on Azurite blob storage
		Eventually(func() (int, error) {
			return testsUtils.CountFilesOnAzuriteBlobStorage(namespace, clusterName, "data.tar")
		}, 30).Should(BeNumerically(">=", 1))
		Eventually(func() (string, error) {
			cluster, err := env.GetCluster(namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			return cluster.Status.FirstRecoverabilityPoint, err
		}, 30).ShouldNot(BeEmpty())
	})
	AssertBackupConditionInClusterStatus(namespace, clusterName)
}

func prepareClusterForPITROnAzurite(
	namespace,
	clusterName,
	backupSampleFile string,
	currentTimestamp *string,
) {
	By("backing up a cluster and verifying it exists on azurite", func() {
		// We create a Backup
		testsUtils.ExecuteBackup(namespace, backupSampleFile, false, testTimeouts[testsUtils.BackupIsReady], env)
		// Verifying file called data.tar should be available on Azurite blob storage
		Eventually(func() (int, error) {
			return testsUtils.CountFilesOnAzuriteBlobStorage(namespace, clusterName, "data.tar")
		}, 30).Should(BeNumerically(">=", 1))
		Eventually(func() (string, error) {
			cluster, err := env.GetCluster(namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			return cluster.Status.FirstRecoverabilityPoint, err
		}, 30).ShouldNot(BeEmpty())
	})

	// Write a table and insert 2 entries on the "app" database
	AssertCreateTestData(env, namespace, clusterName, "for_restore")

	By("getting currentTimestamp", func() {
		ts, err := testsUtils.GetCurrentTimestamp(namespace, clusterName, env)
		*currentTimestamp = ts
		Expect(err).ToNot(HaveOccurred())
	})

	By(fmt.Sprintf("writing 3rd entry into test table '%v'", "for_restore"), func() {
		forward, conn, err := testsUtils.ForwardPSQLConnection(
			env,
			namespace,
			clusterName,
			testsUtils.AppDBName,
			apiv1.ApplicationUserSecretSuffix,
		)
		defer func() {
			forward.Stop()
		}()
		Expect(err).ToNot(HaveOccurred())
		insertRecordIntoTable("for_restore", 3, conn)
	})
	AssertArchiveWalOnAzurite(namespace, clusterName)
}

func createAndAssertPgBouncerPoolerIsSetUp(namespace, poolerYamlFilePath string, expectedInstanceCount int) {
	CreateResourceFromFile(namespace, poolerYamlFilePath)
	Eventually(func() (int32, error) {
		poolerName, err := env.GetResourceNameFromYAML(poolerYamlFilePath)
		Expect(err).ToNot(HaveOccurred())
		// Wait for the deployment to be ready
		deployment := &appsv1.Deployment{}
		err = env.Client.Get(env.Ctx, types.NamespacedName{Namespace: namespace, Name: poolerName}, deployment)

		return deployment.Status.ReadyReplicas, err
	}, 300).Should(BeEquivalentTo(expectedInstanceCount))

	// check pooler pod is up and running
	assertPGBouncerPodsAreReady(namespace, poolerYamlFilePath, expectedInstanceCount)
}

func assertPgBouncerPoolerDeploymentStrategy(
	namespace, poolerYamlFilePath string,
	expectedMaxSurge, expectedMaxUnavailable string,
) {
	By("verify pooler deployment has expected rolling update configuration", func() {
		Eventually(func() bool {
			poolerName, err := env.GetResourceNameFromYAML(poolerYamlFilePath)
			Expect(err).ToNot(HaveOccurred())
			// Wait for the deployment to be ready
			deployment := &appsv1.Deployment{}
			err = env.Client.Get(env.Ctx, types.NamespacedName{Namespace: namespace, Name: poolerName}, deployment)
			if err != nil {
				return false
			}
			if expectedMaxSurge == deployment.Spec.Strategy.RollingUpdate.MaxSurge.String() &&
				expectedMaxUnavailable == deployment.Spec.Strategy.RollingUpdate.MaxUnavailable.String() {
				return true
			}
			return false
		}, 300).Should(BeTrue())
	})
}

// assertPGBouncerPodsAreReady verifies if PGBouncer pooler pods are ready
func assertPGBouncerPodsAreReady(namespace, poolerYamlFilePath string, expectedPodCount int) {
	Eventually(func() (bool, error) {
		poolerName, err := env.GetResourceNameFromYAML(poolerYamlFilePath)
		Expect(err).ToNot(HaveOccurred())
		podList := &corev1.PodList{}
		err = env.Client.List(env.Ctx, podList, ctrlclient.InNamespace(namespace),
			ctrlclient.MatchingLabels{utils.PgbouncerNameLabel: poolerName})
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
	poolerName, err := env.GetResourceNameFromYAML(poolerYamlFilePath)
	Expect(err).ToNot(HaveOccurred())
	poolerService := testsUtils.CreateServiceFQDN(namespace, poolerName)
	appUser, generatedAppUserPassword, err := testsUtils.GetCredentials(
		clusterName, namespace, apiv1.ApplicationUserSecretSuffix, env)
	Expect(err).ToNot(HaveOccurred())
	primaryPod, err := env.GetClusterPrimary(namespace, clusterName)
	Expect(err).ToNot(HaveOccurred())
	AssertConnection(poolerService, appUser, "app", generatedAppUserPassword, primaryPod, 180, env)

	// verify that, if pooler type setup read write then it will allow both read and
	// write operations or if pooler type setup read only then it will allow only read operations
	if isPoolerRW {
		AssertWritesToPrimarySucceeds(primaryPod, poolerService, "app", appUser,
			generatedAppUserPassword)
	} else {
		AssertWritesToReplicaFails(primaryPod, poolerService, "app", appUser,
			generatedAppUserPassword)
	}
}

func assertPodIsRecreated(namespace, poolerSampleFile string) {
	var podNameBeforeDelete string
	poolerName, err := env.GetResourceNameFromYAML(poolerSampleFile)
	Expect(err).ToNot(HaveOccurred())

	By(fmt.Sprintf("deleting pooler '%s' pod", poolerName), func() {
		// gather pgbouncer pod name before deleting
		podList := &corev1.PodList{}
		err = env.Client.List(env.Ctx, podList, ctrlclient.InNamespace(namespace),
			ctrlclient.MatchingLabels{utils.PgbouncerNameLabel: poolerName})
		Expect(err).ToNot(HaveOccurred())
		Expect(len(podList.Items)).Should(BeEquivalentTo(1))
		podNameBeforeDelete = podList.Items[0].GetName()

		// deleting pgbouncer pod
		cmd := fmt.Sprintf("kubectl delete pod %s -n %s", podNameBeforeDelete, namespace)
		_, _, err = testsUtils.Run(cmd)
		Expect(err).ToNot(HaveOccurred())
	})
	By(fmt.Sprintf("verifying pooler '%s' pod has been recreated", poolerName), func() {
		// New pod should be created
		Eventually(func() (bool, error) {
			podList := &corev1.PodList{}
			err = env.Client.List(env.Ctx, podList, ctrlclient.InNamespace(namespace),
				ctrlclient.MatchingLabels{utils.PgbouncerNameLabel: poolerName})
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

	poolerName, err := env.GetResourceNameFromYAML(poolerSampleFile)
	Expect(err).ToNot(HaveOccurred())

	deploymentNamespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      poolerName,
	}
	deployment := &appsv1.Deployment{}
	Eventually(func(g Gomega) {
		err := env.Client.Get(env.Ctx, deploymentNamespacedName, deployment)
		g.Expect(err).ToNot(HaveOccurred())
	}).Should(Succeed())
	err = testsUtils.DeploymentWaitForReady(env, deployment, 60)
	Expect(err).ToNot(HaveOccurred())
	deploymentName := deployment.GetName()

	// Get the pods UIDs. We'll confirm they've changed
	podList := &corev1.PodList{}
	err = env.Client.List(env.Ctx, podList, ctrlclient.InNamespace(namespace),
		ctrlclient.MatchingLabels{utils.PgbouncerNameLabel: poolerName})
	Expect(err).ToNot(HaveOccurred())
	uids := make([]types.UID, len(podList.Items))
	for i, p := range podList.Items {
		uids[i] = p.UID
	}

	By(fmt.Sprintf("deleting pgbouncer '%s' deployment", deploymentName), func() {
		// gather pgbouncer deployment info before delete
		deploymentUID = deployment.UID
		// deleting pgbouncer deployment
		err := env.Client.Delete(env.Ctx, deployment)
		Expect(err).ToNot(HaveOccurred())
	})
	By(fmt.Sprintf("verifying new deployment '%s' has been recreated", deploymentName), func() {
		// new deployment will be created and ready replicas should be one
		Eventually(func() (types.UID, error) {
			err = env.Client.Get(env.Ctx, deploymentNamespacedName, deployment)
			return deployment.UID, err
		}, 300).ShouldNot(BeEquivalentTo(deploymentUID))
	})
	By(fmt.Sprintf("new '%s' deployment has new pods ready", deploymentName), func() {
		err := testsUtils.DeploymentWaitForReady(env, deployment, 120)
		Expect(err).ToNot(HaveOccurred())
	})
	By("verifying UIDs of pods have changed", func() {
		// We wait for the pods of the previous deployment to be deleted
		Eventually(func() (int, error) {
			err := env.Client.List(env.Ctx, podList, ctrlclient.InNamespace(namespace),
				ctrlclient.MatchingLabels{utils.PgbouncerNameLabel: poolerName})
			return len(podList.Items), err
		}, 60).Should(BeNumerically("==", *deployment.Spec.Replicas))
		newuids := make([]types.UID, len(podList.Items))
		for i, p := range podList.Items {
			newuids[i] = p.UID
		}
		Expect(len(funk.Join(uids, newuids, funk.InnerJoin).([]types.UID))).To(BeEquivalentTo(0))
	})
}

// assertPGBouncerEndpointsContainsPodsIP makes sure that the Endpoints resource directs the traffic
// to the correct pods.
func assertPGBouncerEndpointsContainsPodsIP(
	namespace,
	poolerYamlFilePath string,
	expectedPodCount int,
) {
	var pgBouncerPods []*corev1.Pod
	endpoint := &corev1.Endpoints{}
	endpointName, err := env.GetResourceNameFromYAML(poolerYamlFilePath)
	Expect(err).ToNot(HaveOccurred())

	Eventually(func(g Gomega) {
		err := env.Client.Get(env.Ctx, types.NamespacedName{Namespace: namespace, Name: endpointName}, endpoint)
		g.Expect(err).ToNot(HaveOccurred())
	}).Should(Succeed())

	poolerName, err := env.GetResourceNameFromYAML(poolerYamlFilePath)
	Expect(err).ToNot(HaveOccurred())
	podList := &corev1.PodList{}
	err = env.Client.List(env.Ctx, podList, ctrlclient.InNamespace(namespace),
		ctrlclient.MatchingLabels{utils.PgbouncerNameLabel: poolerName})
	Expect(err).ToNot(HaveOccurred())
	Expect(endpoint.Subsets).ToNot(BeEmpty())

	for _, ip := range endpoint.Subsets[0].Addresses {
		for podIndex, pod := range podList.Items {
			if pod.Status.PodIP == ip.IP {
				pgBouncerPods = append(pgBouncerPods, &podList.Items[podIndex])
				continue
			}
		}
	}

	Expect(pgBouncerPods).Should(HaveLen(expectedPodCount), "Pod length or IP mismatch in ep")
}

// assertPGBouncerHasServiceNameInsideHostParameter makes sure that the service name is contained inside the host file
func assertPGBouncerHasServiceNameInsideHostParameter(namespace, serviceName string, podList *corev1.PodList) {
	for _, pod := range podList.Items {
		command := fmt.Sprintf("kubectl exec -n %s %s -- /bin/bash -c 'grep "+
			" \"host=%s\" controller/configs/pgbouncer.ini'", namespace, pod.Name, serviceName)
		out, _, err := testsUtils.Run(command)
		Expect(err).ToNot(HaveOccurred())
		expectedContainedHost := fmt.Sprintf("host=%s", serviceName)
		Expect(out).To(ContainSubstring(expectedContainedHost))
	}
}

// OnlineResizePVC is for verifying if storage can be automatically expanded, or not
func OnlineResizePVC(namespace, clusterName string) {
	walStorageEnabled, err := testsUtils.IsWalStorageEnabled(namespace, clusterName, env)
	Expect(err).ToNot(HaveOccurred())

	pvc := &corev1.PersistentVolumeClaimList{}
	By("verify PVC before expansion", func() {
		// Verifying the first stage of deployment to compare it further with expanded value
		err := env.Client.List(env.Ctx, pvc, ctrlclient.InNamespace(namespace))
		Expect(err).ToNot(HaveOccurred())
		// Iterating through PVC list to assure its default size
		for _, pvClaim := range pvc.Items {
			Expect(pvClaim.Status.Capacity.Storage().String()).To(BeEquivalentTo("1Gi"))
		}
	})
	By("expanding Cluster storage", func() {
		// Patching cluster to expand storage size from 1Gi to 2Gi
		storageType := []string{"storage"}
		if walStorageEnabled {
			storageType = append(storageType, "walStorage")
		}
		for _, s := range storageType {
			cmd := fmt.Sprintf(
				"kubectl patch cluster %v -n %v -p '{\"spec\":{\"%v\":{\"size\":\"2Gi\"}}}' --type=merge",
				clusterName,
				namespace,
				s)
			Eventually(func() error {
				_, _, err := testsUtils.RunUnchecked(cmd)
				return err
			}, 60, 5).Should(BeNil())
		}
	})
	By("verifying Cluster storage is expanded", func() {
		// Gathering and verifying the new size of PVC after update on cluster
		expectedCount := 3
		if walStorageEnabled {
			expectedCount = 6
		}
		Eventually(func() int {
			// Variable counter to store the updated total of expanded PVCs. It should be equal to three
			updateCount := 0
			// Gathering PVC list
			err := env.Client.List(env.Ctx, pvc, ctrlclient.InNamespace(namespace))
			Expect(err).ToNot(HaveOccurred())
			// Iterating through PVC list to compare with expanded size
			for _, pvClaim := range pvc.Items {
				// Size comparison
				if pvClaim.Status.Capacity.Storage().String() == "2Gi" {
					updateCount++
				}
			}
			return updateCount
		}, 300).Should(BeEquivalentTo(expectedCount))
	})
}

func OfflineResizePVC(namespace, clusterName string, timeout int) {
	walStorageEnabled, err := testsUtils.IsWalStorageEnabled(namespace, clusterName, env)
	Expect(err).ToNot(HaveOccurred())

	By("verify PVC size before expansion", func() {
		// Gathering PVC list for future use of comparison and deletion after storage expansion
		pvc := &corev1.PersistentVolumeClaimList{}
		err := env.Client.List(env.Ctx, pvc, ctrlclient.InNamespace(namespace))
		Expect(err).ToNot(HaveOccurred())
		// Iterating through PVC list to verify the default size for future comparison
		for _, pvClaim := range pvc.Items {
			Expect(pvClaim.Status.Capacity.Storage().String()).To(BeEquivalentTo("1Gi"))
		}
	})
	By("expanding Cluster storage", func() {
		// Expanding cluster storage
		storageType := []string{"storage"}
		if walStorageEnabled {
			storageType = append(storageType, "walStorage")
		}
		for _, s := range storageType {
			cmd := fmt.Sprintf(
				"kubectl patch cluster %v -n %v -p '{\"spec\":{\"%v\":{\"size\":\"2Gi\"}}}' --type=merge",
				clusterName,
				namespace,
				s)
			Eventually(func() error {
				_, _, err := testsUtils.RunUnchecked(cmd)
				return err
			}, 60, 5).Should(BeNil())
		}
	})
	By("deleting Pod and PVCs, first replicas then the primary", func() {
		// Gathering cluster primary
		currentPrimary, err := env.GetClusterPrimary(namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		currentPrimaryWalStorageName := currentPrimary.Name + "-wal"
		quickDelete := &ctrlclient.DeleteOptions{
			GracePeriodSeconds: &quickDeletionPeriod,
		}

		podList, err := env.GetClusterPodList(namespace, clusterName)
		Expect(len(podList.Items), err).To(BeEquivalentTo(3))

		// Iterating through PVC list for deleting pod and PVC for storage expansion
		for _, pod := range podList.Items {
			// Comparing cluster pods to not be primary to ensure cluster is healthy.
			// Primary will be eventually deleted
			if !specs.IsPodPrimary(pod) {
				// Deleting PVC
				_, _, err = testsUtils.Run(
					"kubectl delete pvc " + pod.Name + " -n " + namespace + " --wait=false")
				Expect(err).ToNot(HaveOccurred())
				// Deleting WalStorage PVC if needed
				if walStorageEnabled {
					_, _, err = testsUtils.Run(
						"kubectl delete pvc " + pod.Name + "-wal" + " -n " + namespace + " --wait=false")
					Expect(err).ToNot(HaveOccurred())
				}
				// Deleting standby and replica pods
				err = env.DeletePod(namespace, pod.Name, quickDelete)
				Expect(err).ToNot(HaveOccurred())
			}
		}
		AssertClusterIsReady(namespace, clusterName, timeout, env)

		// Deleting primary pvc
		_, _, err = testsUtils.Run(
			"kubectl delete pvc " + currentPrimary.Name + " -n " + namespace + " --wait=false")
		Expect(err).ToNot(HaveOccurred())
		// Deleting Primary WalStorage PVC if needed
		if walStorageEnabled {
			_, _, err = testsUtils.Run(
				"kubectl delete pvc " + currentPrimaryWalStorageName + " -n " + namespace + " --wait=false")
			Expect(err).ToNot(HaveOccurred())
		}
		// Deleting primary pod
		err = env.DeletePod(namespace, currentPrimary.Name, quickDelete)
		Expect(err).ToNot(HaveOccurred())
	})

	AssertClusterIsReady(namespace, clusterName, timeout, env)
	By("verifying Cluster storage is expanded", func() {
		// Gathering PVC list for comparison
		pvcList, err := env.GetPVCList(namespace)
		Expect(err).ToNot(HaveOccurred())
		// Gathering PVC size and comparing with expanded value
		expectedCount := 3
		if walStorageEnabled {
			expectedCount = 6
		}
		Eventually(func() int {
			// Bool value to ensure every pod in cluster expanded, will be eventually compared as true
			count := 0
			// Iterating through PVC list for comparison
			for _, pvClaim := range pvcList.Items {
				// Comparing to expanded value.
				// Once all pods will be expanded, count will be equal to three
				if pvClaim.Status.Capacity.Storage().String() == "2Gi" {
					count++
				}
			}
			return count
		}, 30).Should(BeEquivalentTo(expectedCount))
	})
}

func DeleteTableUsingPgBouncerService(
	namespace,
	clusterName,
	poolerYamlFilePath string,
	env *testsUtils.TestingEnvironment,
	pod *corev1.Pod,
) {
	poolerName, err := env.GetResourceNameFromYAML(poolerYamlFilePath)
	Expect(err).ToNot(HaveOccurred())
	poolerService := testsUtils.CreateServiceFQDN(namespace, poolerName)
	appUser, generatedAppUserPassword, err := testsUtils.GetCredentials(
		clusterName, namespace, apiv1.ApplicationUserSecretSuffix, env)
	Expect(err).ToNot(HaveOccurred())
	AssertConnection(poolerService, appUser, "app", generatedAppUserPassword, pod, 180, env)

	_, _, err = testsUtils.RunQueryFromPod(
		pod, poolerService, "app", appUser, generatedAppUserPassword,
		"DROP TABLE table1",
		env)
	Expect(err).ToNot(HaveOccurred())
}

func collectAndAssertDefaultMetricsPresentOnEachPod(
	namespace, clusterName string,
	tlsEnabled bool,
	expectPresent bool,
) {
	By("collecting and verifying a set of default metrics on each pod", func() {
		defaultMetrics := []string{
			"cnpg_pg_settings_setting",
			"cnpg_backends_waiting_total",
			"cnpg_pg_postmaster_start_time",
			"cnpg_pg_replication",
			"cnpg_pg_stat_archiver",
			"cnpg_pg_stat_bgwriter",
			"cnpg_pg_stat_database",
		}

		if env.PostgresVersion > 16 {
			defaultMetrics = append(defaultMetrics,
				"cnpg_pg_stat_checkpointer",
			)
		}

		podList, err := env.GetClusterPodList(namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		for _, pod := range podList.Items {
			podName := pod.GetName()
			out, err := testsUtils.RetrieveMetricsFromInstance(env, pod, tlsEnabled)
			Expect(err).ToNot(HaveOccurred())

			// error should be zero on each pod metrics
			Expect(strings.Contains(out, "cnpg_collector_last_collection_error 0")).Should(BeTrue(),
				"Metric collection issues on %v.\nCollected metrics:\n%v", podName, out)
			// verify that, default set of monitoring queries should not be existed on each pod
			for _, data := range defaultMetrics {
				if expectPresent {
					Expect(strings.Contains(out, data)).Should(BeTrue(),
						"Metric collection issues on pod %v."+
							"\nFor expected keyword '%v'.\nCollected metrics:\n%v", podName, data, out)
				} else {
					Expect(strings.Contains(out, data)).Should(BeFalse(),
						"Metric collection issues on pod %v."+
							"\nFor expected keyword '%v'.\nCollected metrics:\n%v", podName, data, out)
				}
			}
		}
	})
}

// collectAndAssertMetricsPresentOnEachPod verify a set of metrics is existed in each pod
func collectAndAssertCollectorMetricsPresentOnEachPod(cluster *apiv1.Cluster) {
	cnpgCollectorMetrics := []string{
		"cnpg_collector_collection_duration_seconds",
		"cnpg_collector_fencing_on",
		"cnpg_collector_nodes_used",
		"cnpg_collector_pg_wal",
		"cnpg_collector_pg_wal_archive_status",
		"cnpg_collector_postgres_version",
		"cnpg_collector_collections_total",
		"cnpg_collector_last_collection_error",
		"cnpg_collector_collection_duration_seconds",
		"cnpg_collector_manual_switchover_required",
		"cnpg_collector_sync_replicas",
		"cnpg_collector_replica_mode",
	}

	if env.PostgresVersion > 14 {
		cnpgCollectorMetrics = append(cnpgCollectorMetrics,
			"cnpg_collector_wal_records",
			"cnpg_collector_wal_fpi",
			"cnpg_collector_wal_bytes",
			"cnpg_collector_wal_buffers_full",
			"cnpg_collector_wal_write",
			"cnpg_collector_wal_sync",
			"cnpg_collector_wal_write_time",
			"cnpg_collector_wal_sync_time",
		)
	}
	By("collecting and verify set of collector metrics on each pod", func() {
		podList, err := env.GetClusterPodList(cluster.Namespace, cluster.Name)
		Expect(err).ToNot(HaveOccurred())
		for _, pod := range podList.Items {
			podName := pod.GetName()
			out, err := testsUtils.RetrieveMetricsFromInstance(env, pod, cluster.IsMetricsTLSEnabled())
			Expect(err).ToNot(HaveOccurred())

			// error should be zero on each pod metrics
			Expect(strings.Contains(out, "cnpg_collector_last_collection_error 0")).Should(BeTrue(),
				"Metric collection issues on %v.\nCollected metrics:\n%v", podName, out)
			// verify that, default set of monitoring queries should not be existed on each pod
			for _, data := range cnpgCollectorMetrics {
				Expect(strings.Contains(out, data)).Should(BeTrue(),
					"Metric collection issues on pod %v."+
						"\nFor expected keyword '%v'.\nCollected metrics:\n%v", podName, data, out)
			}
		}
	})
}

// CreateResourcesFromFileWithError creates the Kubernetes objects defined in the
// YAML sample file and returns any errors
func CreateResourcesFromFileWithError(namespace, sampleFilePath string) error {
	wrapErr := func(err error) error { return fmt.Errorf("on CreateResourcesFromFileWithError: %w", err) }
	yaml, err := GetYAMLContent(sampleFilePath)
	if err != nil {
		return wrapErr(err)
	}

	objects, err := testsUtils.ParseObjectsFromYAML(yaml, namespace)
	if err != nil {
		return wrapErr(err)
	}
	for _, obj := range objects {
		_, err := testsUtils.CreateObject(env, obj)
		if err != nil {
			return wrapErr(err)
		}
	}
	return nil
}

// CreateResourceFromFile creates the Kubernetes objects defined in a YAML sample file
func CreateResourceFromFile(namespace, sampleFilePath string) {
	Eventually(func() error {
		return CreateResourcesFromFileWithError(namespace, sampleFilePath)
	}, RetryTimeout, PollingTime).Should(BeNil())
}

// GetYAMLContent opens a .yaml of .template file and returns its content
//
// In the case of a .template file, it performs the substitution of the embedded
// SHELL-FORMAT variables
func GetYAMLContent(sampleFilePath string) ([]byte, error) {
	wrapErr := func(err error) error { return fmt.Errorf("in GetYAMLContent: %w", err) }
	cleanPath := filepath.Clean(sampleFilePath)
	data, err := os.ReadFile(cleanPath)
	if err != nil {
		return nil, wrapErr(err)
	}
	yaml := data

	if filepath.Ext(cleanPath) == ".template" {
		preRollingUpdateImg := os.Getenv("E2E_PRE_ROLLING_UPDATE_IMG")
		if preRollingUpdateImg == "" {
			preRollingUpdateImg = os.Getenv("POSTGRES_IMG")
		}
		csiStorageClass := os.Getenv("E2E_CSI_STORAGE_CLASS")
		if csiStorageClass == "" {
			csiStorageClass = os.Getenv("E2E_DEFAULT_STORAGE_CLASS")
		}
		envVars := buildTemplateEnvs(map[string]string{
			"E2E_PRE_ROLLING_UPDATE_IMG": preRollingUpdateImg,
			"E2E_CSI_STORAGE_CLASS":      csiStorageClass,
		})

		if serverName := os.Getenv("SERVER_NAME"); serverName != "" {
			envVars["SERVER_NAME"] = serverName
		}

		yaml, err = testsUtils.Envsubst(envVars, data)
		if err != nil {
			return nil, wrapErr(err)
		}
	}
	return yaml, nil
}

func buildTemplateEnvs(additionalEnvs map[string]string) map[string]string {
	envs := make(map[string]string)
	rawEnvs := os.Environ()
	for _, s := range rawEnvs {
		keyValue := strings.Split(s, "=")
		if len(keyValue) < 2 {
			continue
		}
		envs[keyValue[0]] = keyValue[1]
	}

	for key, value := range additionalEnvs {
		envs[key] = value
	}

	return envs
}

// DeleteResourcesFromFile deletes the Kubernetes objects described in the file
func DeleteResourcesFromFile(namespace, sampleFilePath string) error {
	wrapErr := func(err error) error { return fmt.Errorf("in DeleteResourcesFromFile: %w", err) }
	yaml, err := GetYAMLContent(sampleFilePath)
	if err != nil {
		return wrapErr(err)
	}

	objects, err := testsUtils.ParseObjectsFromYAML(yaml, namespace)
	if err != nil {
		return wrapErr(err)
	}
	for _, obj := range objects {
		err := testsUtils.DeleteObject(env, obj)
		if err != nil {
			return wrapErr(err)
		}
	}
	return nil
}

// Assert in the giving cluster, all the postgres db has no pending restart
func AssertPostgresNoPendingRestart(namespace, clusterName string, cmdTimeout time.Duration, timeout int) {
	By("waiting for all pods have no pending restart", func() {
		podList, err := env.GetClusterPodList(namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		// Check that the new parameter has been modified in every pod
		Eventually(func() (bool, error) {
			noPendingRestart := true
			for _, pod := range podList.Items {
				stdout, _, err := env.ExecCommand(env.Ctx, pod, specs.PostgresContainerName, &cmdTimeout,
					"psql", "-U", "postgres", "-tAc", "SELECT EXISTS(SELECT 1 FROM pg_settings WHERE pending_restart)")
				if err != nil {
					return false, nil
				}
				if strings.Trim(stdout, "\n") == "f" {
					continue
				}

				noPendingRestart = false
				break
			}
			return noPendingRestart, nil
		}, timeout, 2).Should(BeEquivalentTo(true),
			"all pods in cluster has no pending restart")
	})
}

func AssertBackupConditionTimestampChangedInClusterStatus(
	namespace,
	clusterName string,
	clusterConditionType apiv1.ClusterConditionType,
	lastTransactionTimeStamp *v1.Time,
) {
	By(fmt.Sprintf("waiting for backup condition status in cluster '%v'", clusterName), func() {
		Eventually(func() (bool, error) {
			getBackupCondition, err := testsUtils.GetConditionsInClusterStatus(
				namespace, clusterName, env, clusterConditionType)
			if err != nil {
				return false, err
			}
			return getBackupCondition.LastTransitionTime.After(lastTransactionTimeStamp.Time), nil
		}, 300, 5).Should(BeTrue())
	})
}

func AssertBackupConditionInClusterStatus(namespace, clusterName string) {
	By(fmt.Sprintf("waiting for backup condition status in cluster '%v'", clusterName), func() {
		Eventually(func() (string, error) {
			getBackupCondition, err := testsUtils.GetConditionsInClusterStatus(
				namespace, clusterName, env, apiv1.ConditionBackup)
			if err != nil {
				return "", err
			}
			return string(getBackupCondition.Status), nil
		}, 300, 5).Should(BeEquivalentTo("True"))
	})
}

func AssertClusterReadinessStatusIsReached(
	namespace,
	clusterName string,
	conditionStatus apiv1.ConditionStatus,
	timeout int,
	env *testsUtils.TestingEnvironment,
) {
	By(fmt.Sprintf("waiting for cluster condition status in cluster '%v'", clusterName), func() {
		Eventually(func() (string, error) {
			clusterCondition, err := testsUtils.GetConditionsInClusterStatus(
				namespace, clusterName, env, apiv1.ConditionClusterReady)
			if err != nil {
				return "", err
			}
			return string(clusterCondition.Status), nil
		}, timeout, 2).Should(BeEquivalentTo(conditionStatus))
	})
}

// AssertPvcHasLabels verifies if the PVCs of a cluster in a given namespace
// contains the expected labels, and their values reflect the current status
// of the related pods.
func AssertPvcHasLabels(
	namespace,
	clusterName string,
) {
	By("checking PVC have the correct role labels", func() {
		Eventually(func(g Gomega) {
			// Gather the list of PVCs in the current namespace
			pvcList, err := env.GetPVCList(namespace)
			g.Expect(err).ToNot(HaveOccurred())

			// Iterating through PVC list
			for _, pvc := range pvcList.Items {
				// Gather the podName related to the current pvc using nodeSerial
				podName := fmt.Sprintf("%v-%v", clusterName, pvc.Annotations[utils.ClusterSerialAnnotationName])
				pod := &corev1.Pod{}
				podNamespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      podName,
				}
				err = env.Client.Get(env.Ctx, podNamespacedName, pod)
				g.Expect(err).ToNot(HaveOccurred())

				ExpectedRole := "replica"
				if specs.IsPodPrimary(*pod) {
					ExpectedRole = "primary"
				}
				ExpectedPvcRole := "PG_DATA"
				if pvc.Name == podName+"-wal" {
					ExpectedPvcRole = "PG_WAL"
				}
				expectedLabels := map[string]string{
					utils.ClusterLabelName:             clusterName,
					utils.PvcRoleLabelName:             ExpectedPvcRole,
					utils.ClusterInstanceRoleLabelName: ExpectedRole,
				}
				g.Expect(testsUtils.PvcHasLabels(pvc, expectedLabels)).To(BeTrue(),
					fmt.Sprintf("expectedLabels: %v and found actualLabels on pvc: %v",
						expectedLabels, pod.GetLabels()))
			}
		}, 300, 5).Should(Succeed())
	})
}

// AssertReplicationSlotsOnPod checks that all the required replication slots exist in a given pod,
// and that obsolete slots are correctly deleted (post management operations).
// In the primary, it will also check if the slots are active.
func AssertReplicationSlotsOnPod(
	namespace,
	clusterName string,
	pod corev1.Pod,
	expectedSlots []string,
	isActiveOnPrimary bool,
	isActiveOnReplica bool,
) {
	GinkgoWriter.Println("checking contain slots:", expectedSlots, "for pod:", pod.Name)
	Eventually(func() ([]string, error) {
		currentSlots, err := testsUtils.GetReplicationSlotsOnPod(namespace, pod.GetName(), env)
		return currentSlots, err
	}, 300).Should(ContainElements(expectedSlots),
		func() string {
			return testsUtils.PrintReplicationSlots(namespace, clusterName, env)
		})

	GinkgoWriter.Println("executing replication slot assertion query on pod", pod.Name)

	for _, slot := range expectedSlots {
		query := fmt.Sprintf(
			"SELECT EXISTS (SELECT 1 FROM pg_replication_slots "+
				"WHERE slot_name = '%v' AND active = '%t' "+
				"AND temporary = 'f' AND slot_type = 'physical')", slot, isActiveOnReplica)
		if specs.IsPodPrimary(pod) {
			query = fmt.Sprintf(
				"SELECT EXISTS (SELECT 1 FROM pg_replication_slots "+
					"WHERE slot_name = '%v' AND active = '%t' "+
					"AND temporary = 'f' AND slot_type = 'physical')", slot, isActiveOnPrimary)
		}
		Eventually(func() (string, error) {
			stdout, _, err := testsUtils.RunQueryFromPod(&pod, testsUtils.PGLocalSocketDir,
				"app", "postgres", "''", query, env)
			return strings.TrimSpace(stdout), err
		}, 300).Should(BeEquivalentTo("t"),
			func() string {
				return testsUtils.PrintReplicationSlots(namespace, clusterName, env)
			})
	}
}

// AssertClusterReplicationSlotsAligned will compare all the replication slot restart_lsn
// in a cluster. The assertion will succeed if they are all equivalent.
func AssertClusterReplicationSlotsAligned(
	namespace,
	clusterName string,
) {
	podList, err := env.GetClusterPodList(namespace, clusterName)
	Expect(err).ToNot(HaveOccurred())
	Eventually(func() bool {
		var lsnList []string
		for _, pod := range podList.Items {
			out, err := testsUtils.GetReplicationSlotLsnsOnPod(namespace, clusterName, pod, env)
			Expect(err).ToNot(HaveOccurred())
			lsnList = append(lsnList, out...)
		}
		return testsUtils.AreSameLsn(lsnList)
	}, 300).Should(BeEquivalentTo(true),
		func() string {
			return testsUtils.PrintReplicationSlots(namespace, clusterName, env)
		})
}

// AssertClusterHAReplicationSlots will verify if the replication slots of each pod
// of the cluster exist and are aligned.
func AssertClusterHAReplicationSlots(namespace, clusterName string) {
	By("verifying all cluster's replication slots exist and are aligned", func() {
		podList, err := env.GetClusterPodList(namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		for _, pod := range podList.Items {
			expectedSlots, err := testsUtils.GetExpectedHAReplicationSlotsOnPod(namespace, clusterName, pod.GetName(), env)
			Expect(err).ToNot(HaveOccurred())
			AssertReplicationSlotsOnPod(namespace, clusterName, pod, expectedSlots, true, false)
		}
		AssertClusterReplicationSlotsAligned(namespace, clusterName)
	})
}

// AssertClusterRollingRestart restarts a given cluster
func AssertClusterRollingRestart(namespace, clusterName string) {
	By(fmt.Sprintf("restarting cluster %v", clusterName), func() {
		cluster, err := env.GetCluster(namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		clusterRestarted := cluster.DeepCopy()
		if clusterRestarted.Annotations == nil {
			clusterRestarted.Annotations = make(map[string]string)
		}
		clusterRestarted.Annotations[utils.ClusterRestartAnnotationName] = time.Now().Format(time.RFC3339)
		clusterRestarted.ManagedFields = nil
		err = env.Client.Patch(env.Ctx, clusterRestarted, ctrlclient.MergeFrom(cluster))
		Expect(err).ToNot(HaveOccurred())
	})
	AssertClusterEventuallyReachesPhase(namespace, clusterName,
		[]string{apiv1.PhaseUpgrade, apiv1.PhaseWaitingForInstancesToBeActive}, 120)
	AssertClusterIsReady(namespace, clusterName, testTimeouts[testsUtils.ClusterIsReadyQuick], env)
}

// AssertPVCCount matches count and pvc List.
func AssertPVCCount(namespace, clusterName string, pvcCount, timeout int) {
	By(fmt.Sprintf("verify cluster %v healthy pvc list", clusterName), func() {
		Eventually(func(g Gomega) {
			cluster, _ := env.GetCluster(namespace, clusterName)
			g.Expect(cluster.Status.PVCCount).To(BeEquivalentTo(pvcCount))

			pvcList := &corev1.PersistentVolumeClaimList{}
			err := env.Client.List(
				env.Ctx, pvcList, ctrlclient.MatchingLabels{utils.ClusterLabelName: clusterName},
				ctrlclient.InNamespace(namespace),
			)
			g.Expect(err).ToNot(HaveOccurred())

			g.Expect(cluster.Status.PVCCount).To(BeEquivalentTo(len(pvcList.Items)))
		}, timeout, 4).Should(Succeed())
	})
}

// AssertClusterPhaseIsConsistent expects the phase of a cluster to be consistent for a given number of seconds.
func AssertClusterPhaseIsConsistent(namespace, clusterName string, phase []string, timeout int) {
	By(fmt.Sprintf("verifying cluster '%v' phase '%+q' is consistent", clusterName, phase), func() {
		assert := assertPredicateClusterHasPhase(namespace, clusterName, phase)
		Consistently(assert, timeout, 2).Should(Succeed())
	})
}

// AssertClusterEventuallyReachesPhase checks the phase of a cluster reaches the phase argument
// within the specified timeout
func AssertClusterEventuallyReachesPhase(namespace, clusterName string, phase []string, timeout int) {
	By(fmt.Sprintf("verifying cluster '%v' phase should eventually become one of '%+q'", clusterName, phase), func() {
		assert := assertPredicateClusterHasPhase(namespace, clusterName, phase)
		Eventually(assert, timeout).Should(Succeed())
	})
}

// assertPredicateClusterHasPhase returns true if the Cluster's phase is contained in a given slice of phases
func assertPredicateClusterHasPhase(namespace, clusterName string, phase []string) func(g Gomega) {
	return func(g Gomega) {
		cluster, err := env.GetCluster(namespace, clusterName)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(slices.Contains(phase, cluster.Status.Phase)).To(BeTrue())
	}
}

// assertIncludesMetrics is a utility function used for asserting that specific metrics,
// defined by regular expressions in
// the 'expectedMetrics' map, are present in the 'rawMetricsOutput' string.
// It also checks whether the metrics match the expected format defined by their regular expressions.
// If any assertion fails, it prints an error message to GinkgoWriter.
//
// Parameters:
//   - rawMetricsOutput: The raw metrics data string to be checked.
//   - expectedMetrics: A map of metric names to regular expressions that describe the expected format of the metrics.
//
// Example usage:
//
//	expectedMetrics := map[string]*regexp.Regexp{
//	    "cpu_usage":   regexp.MustCompile(`^\d+\.\d+$`), // Example: "cpu_usage 0.25"
//	    "memory_usage": regexp.MustCompile(`^\d+\s\w+$`), // Example: "memory_usage 512 MiB"
//	}
//	assertIncludesMetrics(rawMetricsOutput, expectedMetrics)
//
// The function will assert that the specified metrics exist in 'rawMetricsOutput' and match their expected formats.
// If any assertion fails, it will print an error message with details about the failed metric collection.
//
// Note: This function is typically used in testing scenarios to validate metric collection behavior.
func assertIncludesMetrics(rawMetricsOutput string, expectedMetrics map[string]*regexp.Regexp) {
	debugDetails := fmt.Sprintf("Priting rawMetricsOutput:\n%s", rawMetricsOutput)
	withDebugDetails := func(baseErrMessage string) string {
		return fmt.Sprintf("%s\n%s\n", baseErrMessage, debugDetails)
	}

	for key, valueRe := range expectedMetrics {
		re := regexp.MustCompile(fmt.Sprintf("(?m)^(%s).*$", key))

		// match a metric with the value of expectedMetrics key
		match := re.FindString(rawMetricsOutput)
		Expect(match).NotTo(BeEmpty(), withDebugDetails(fmt.Sprintf("Found no match for metric %s", key)))

		// extract the value from the metric previously matched
		value := strings.Fields(match)[1]
		Expect(strings.Fields(match)[1]).NotTo(BeEmpty(),
			withDebugDetails(fmt.Sprintf("Found no result for metric %s.Metric line: %s", key, match)))

		// expect the expectedMetrics regexp to match the value of the metric
		Expect(valueRe.MatchString(value)).To(BeTrue(),
			withDebugDetails(fmt.Sprintf("Expected %s to have value %v but got %s", key, valueRe, value)))
	}
}

func assertExcludesMetrics(rawMetricsOutput string, nonCollected []string) {
	for _, nonCollectable := range nonCollected {
		// match a metric with the value of expectedMetrics key
		Expect(rawMetricsOutput).NotTo(ContainSubstring(nonCollectable))
	}
}
