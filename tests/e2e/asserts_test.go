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
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/thoas/go-funk"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	webhookv1 "github.com/cloudnative-pg/cloudnative-pg/internal/webhook/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/certs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	testsUtils "github.com/cloudnative-pg/cloudnative-pg/tests/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/backups"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/deployments"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/environment"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/envsubst"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/exec"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/importdb"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/minio"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/nodes"
	objectsutils "github.com/cloudnative-pg/cloudnative-pg/tests/utils/objects"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/operator"
	podutils "github.com/cloudnative-pg/cloudnative-pg/tests/utils/pods"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/proxy"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/replicationslot"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/run"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/secrets"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/services"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/storage"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/yaml"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func AssertSwitchover(namespace string, clusterName string, env *environment.TestingEnvironment) {
	AssertSwitchoverWithHistory(namespace, clusterName, false, env)
}

func AssertSwitchoverOnReplica(namespace string, clusterName string, env *environment.TestingEnvironment) {
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
	env *environment.TestingEnvironment,
) {
	var pods []string
	var oldPrimary, targetPrimary string
	var oldPodListLength int

	// First we check that the starting situation is the expected one
	By("checking that CurrentPrimary and TargetPrimary are the same", func() {
		var cluster *apiv1.Cluster

		Eventually(func(g Gomega) {
			var err error
			cluster, err = clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(cluster.Status.CurrentPrimary, err).To(
				BeEquivalentTo(cluster.Status.TargetPrimary),
			)
		}).Should(Succeed())

		oldPrimary = cluster.Status.CurrentPrimary

		// Gather pod names
		podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
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
			cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			cluster.Status.TargetPrimary = targetPrimary
			return env.Client.Status().Update(env.Ctx, cluster)
		})
		Expect(err).ToNot(HaveOccurred())
	})

	By("waiting that the TargetPrimary become also CurrentPrimary", func() {
		Eventually(func() (string, error) {
			cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			return cluster.Status.CurrentPrimary, err
		}, testTimeouts[timeouts.NewPrimaryAfterSwitchover]).Should(BeEquivalentTo(targetPrimary))
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
	AssertClusterIsReady(namespace, clusterName, testTimeouts[timeouts.ClusterIsReady], env)

	if !isReplica {
		By("confirming that the all postgres containers have *.history file after switchover", func() {
			timeout := 120

			// Gather pod names
			podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
			Expect(len(podList.Items), err).To(BeEquivalentTo(oldPodListLength))
			pods = make([]string, 0, len(podList.Items))
			for _, p := range podList.Items {
				pods = append(pods, p.Name)
			}

			Eventually(func() error {
				count := 0
				for _, pod := range pods {
					out, _, err := exec.CommandInInstancePod(
						env.Ctx, env.Client, env.Interface, env.RestClientConfig,
						exec.PodLocator{
							Namespace: namespace,
							PodName:   pod,
						}, nil, "sh", "-c", "ls $PGDATA/pg_wal/*.history")
					if err != nil {
						return err
					}

					numHistory := len(strings.Split(strings.TrimSpace(out), "\n"))
					GinkgoWriter.Printf("count %d: pod: %s, the number of history file in pg_wal: %d\n", count, pod,
						numHistory)
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
	env *environment.TestingEnvironment,
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
		}, testTimeouts[timeouts.NamespaceCreation]).Should(BeEquivalentTo(namespace))
	})

	By(fmt.Sprintf("creating a Cluster in the %v namespace", namespace), func() {
		CreateResourceFromFile(namespace, sampleFile)
	})
	// Setting up a cluster with three pods is slow, usually 200-600s
	AssertClusterIsReady(namespace, clusterName, testTimeouts[timeouts.ClusterIsReady], env)
}

// AssertClusterIsReady checks the cluster has as many pods as in spec, that
// none of them are going to be deleted, and that the status is Healthy
func AssertClusterIsReady(namespace string, clusterName string, timeout int, env *environment.TestingEnvironment) {
	By(fmt.Sprintf("having a Cluster %s with each instance in status ready", clusterName), func() {
		// Eventually the number of ready instances should be equal to the
		// amount of instances defined in the cluster and
		// the cluster status should be in healthy state
		var cluster *apiv1.Cluster

		Eventually(func(g Gomega) {
			var err error
			cluster, err = clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
			g.Expect(err).ToNot(HaveOccurred())
		}).Should(Succeed())

		start := time.Now()
		Eventually(func() (string, error) {
			podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
			if err != nil {
				return "", err
			}
			if cluster.Spec.Instances == utils.CountReadyPods(podList.Items) {
				for _, pod := range podList.Items {
					if pod.DeletionTimestamp != nil {
						return fmt.Sprintf("Pod '%s' is waiting for deletion", pod.Name), nil
					}
				}
				cluster, err = clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				return cluster.Status.Phase, err
			}
			return fmt.Sprintf("Ready pod is not as expected. Spec Instances: %d, ready pods: %d \n",
				cluster.Spec.Instances,
				utils.CountReadyPods(podList.Items)), nil
		}, timeout, 2).Should(BeEquivalentTo(apiv1.PhaseHealthy),
			func() string {
				cluster := testsUtils.PrintClusterResources(env.Ctx, env.Client, namespace, clusterName)
				kubeNodes, _ := nodes.DescribeKubernetesNodes(env.Ctx, env.Client)
				return fmt.Sprintf("CLUSTER STATE\n%s\n\nK8S NODES\n%s",
					cluster, kubeNodes)
			},
		)

		if cluster.Spec.Instances != 1 {
			Eventually(func(g Gomega) {
				podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
				g.Expect(err).ToNot(HaveOccurred(), "cannot get cluster pod list")

				primaryPod, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
				g.Expect(err).ToNot(HaveOccurred(), "cannot find cluster primary pod")

				replicaNamesList := make([]string, 0, len(podList.Items)-1)
				for _, pod := range podList.Items {
					if pod.Name != primaryPod.Name {
						replicaNamesList = append(replicaNamesList, pq.QuoteLiteral(pod.Name))
					}
				}
				replicaNamesString := strings.Join(replicaNamesList, ",")
				out, _, err := exec.QueryInInstancePod(
					env.Ctx, env.Client, env.Interface, env.RestClientConfig,
					exec.PodLocator{
						Namespace: namespace,
						PodName:   primaryPod.Name,
					},
					"postgres",
					fmt.Sprintf("SELECT COUNT(*) FROM pg_catalog.pg_stat_replication WHERE application_name IN (%s)",
						replicaNamesString),
				)
				g.Expect(err).ToNot(HaveOccurred(), "cannot extract the list of streaming replicas")
				g.Expect(strings.TrimSpace(out)).To(BeEquivalentTo(fmt.Sprintf("%d", len(replicaNamesList))))
			}, timeout, 2).Should(Succeed(), "Replicas are attached via streaming connection")
		}
		GinkgoWriter.Println("Cluster ready, took", time.Since(start))
	})
}

func AssertClusterDefault(
	namespace string,
	clusterName string,
	env *environment.TestingEnvironment,
) {
	By("having a Cluster object populated with default values", func() {
		// Eventually the number of ready instances should be equal to the
		// amount of instances defined in the cluster and
		// the cluster status should be in healthy state
		var cluster *apiv1.Cluster
		Eventually(func(g Gomega) {
			var err error
			cluster, err = clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			g.Expect(err).ToNot(HaveOccurred())
		}).Should(Succeed())

		validator := webhookv1.ClusterCustomValidator{}
		validationWarn, validationErr := validator.ValidateCreate(env.Ctx, cluster)
		Expect(validationWarn).To(BeEmpty())
		Expect(validationErr).ToNot(HaveOccurred())
	})
}

func AssertWebhookEnabled(env *environment.TestingEnvironment, mutating, validating string) {
	By("re-setting namespace selector for all admission controllers", func() {
		// Setting the namespace selector in MutatingWebhook and ValidatingWebhook
		// to nil will go back to the default behaviour
		mWhc, position, err := operator.GetMutatingWebhookByName(env.Ctx, env.Client, mutating)
		Expect(err).ToNot(HaveOccurred())
		mWhc.Webhooks[position].NamespaceSelector = nil
		err = operator.UpdateMutatingWebhookConf(env.Ctx, env.Interface, mWhc)
		Expect(err).ToNot(HaveOccurred())

		vWhc, position, err := operator.GetValidatingWebhookByName(env.Ctx, env.Client, validating)
		Expect(err).ToNot(HaveOccurred())
		vWhc.Webhooks[position].NamespaceSelector = nil
		err = operator.UpdateValidatingWebhookConf(env.Ctx, env.Interface, vWhc)
		Expect(err).ToNot(HaveOccurred())
	})
}

// Update the secrets and verify cluster reference the updated resource version of secrets
func AssertUpdateSecret(
	field string,
	value string,
	secretName string,
	namespace string,
	clusterName string,
	timeout int,
	env *environment.TestingEnvironment,
) {
	var secret corev1.Secret

	// Gather the secret
	Eventually(func(g Gomega) {
		err := env.Client.Get(env.Ctx,
			ctrlclient.ObjectKey{Namespace: namespace, Name: secretName},
			&secret)
		g.Expect(err).ToNot(HaveOccurred())
	}).Should(Succeed())

	// Change the given field to the new value provided
	secret.Data[field] = []byte(value)
	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		return env.Client.Update(env.Ctx, &secret)
	})
	Expect(err).ToNot(HaveOccurred())

	// Wait for the cluster to pick up the updated secrets version first
	Eventually(func() string {
		cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
		if err != nil {
			GinkgoWriter.Printf("Error reports while retrieving cluster %v\n", err.Error())
			return ""
		}
		switch {
		case strings.HasSuffix(secretName, apiv1.ApplicationUserSecretSuffix):
			GinkgoWriter.Printf("Resource version of %s secret referenced in the cluster is %v\n",
				secretName,
				cluster.Status.SecretsResourceVersion.ApplicationSecretVersion)
			return cluster.Status.SecretsResourceVersion.ApplicationSecretVersion

		case strings.HasSuffix(secretName, apiv1.SuperUserSecretSuffix):
			GinkgoWriter.Printf("Resource version of %s secret referenced in the cluster is %v\n",
				secretName,
				cluster.Status.SecretsResourceVersion.SuperuserSecretVersion)
			return cluster.Status.SecretsResourceVersion.SuperuserSecretVersion

		case cluster.UsesSecretInManagedRoles(secretName):
			GinkgoWriter.Printf("Resource version of %s ManagedRole secret referenced in the cluster is %v\n",
				secretName,
				cluster.Status.SecretsResourceVersion.ManagedRoleSecretVersions[secretName])
			return cluster.Status.SecretsResourceVersion.ManagedRoleSecretVersions[secretName]

		default:
			GinkgoWriter.Printf("Unsupported secrets name found %v\n", secretName)
			return ""
		}
	}, timeout).Should(BeEquivalentTo(secret.ResourceVersion))
}

// AssertConnection is used if a connection from a pod to a postgresql database works
func AssertConnection(
	namespace string,
	service string,
	dbname string,
	user string,
	password string,
	env *environment.TestingEnvironment,
) {
	By(fmt.Sprintf("connecting to the %v service as %v", service, user), func() {
		Eventually(func(g Gomega) {
			forwardConn, conn, err := postgres.ForwardPSQLServiceConnection(
				env.Ctx, env.Interface, env.RestClientConfig,
				namespace, service, dbname, user, password,
			)
			defer func() {
				_ = conn.Close()
				forwardConn.Close()
			}()
			g.Expect(err).ToNot(HaveOccurred())

			var rawValue string
			row := conn.QueryRow("SELECT 1")
			err = row.Scan(&rawValue)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(strings.TrimSpace(rawValue)).To(BeEquivalentTo("1"))
		}, RetryTimeout).Should(Succeed())
	})
}

type TableLocator struct {
	Namespace    string
	ClusterName  string
	DatabaseName string
	TableName    string
	Tablespace   string
}

// AssertCreateTestData create test data on a given TableLocator
func AssertCreateTestData(env *environment.TestingEnvironment, tl TableLocator) {
	if tl.DatabaseName == "" {
		tl.DatabaseName = postgres.AppDBName
	}
	if tl.Tablespace == "" {
		tl.Tablespace = postgres.TablespaceDefaultName
	}

	By(fmt.Sprintf("creating test data in table %v (cluster %v, database %v, tablespace %v)",
		tl.TableName, tl.ClusterName, tl.DatabaseName, tl.Tablespace), func() {
		forward, conn, err := postgres.ForwardPSQLConnection(
			env.Ctx,
			env.Client,
			env.Interface,
			env.RestClientConfig,
			tl.Namespace,
			tl.ClusterName,
			tl.DatabaseName,
			apiv1.ApplicationUserSecretSuffix,
		)
		defer func() {
			_ = conn.Close()
			forward.Close()
		}()
		Expect(err).ToNot(HaveOccurred())

		query := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %v TABLESPACE %v AS VALUES (1),(2);",
			tl.TableName, tl.Tablespace)

		_, err = conn.Exec(query)
		Expect(err).ToNot(HaveOccurred())
	})
}

// AssertCreateTestDataLargeObject create large objects with oid and data
func AssertCreateTestDataLargeObject(namespace, clusterName string, oid int, data string) {
	By("creating large object", func() {
		query := fmt.Sprintf("CREATE TABLE IF NOT EXISTS image (name text,raster oid); "+
			"INSERT INTO image (name, raster) VALUES ('beautiful image', lo_from_bytea(%d, '%s'));", oid, data)

		_, err := postgres.RunExecOverForward(
			env.Ctx, env.Client, env.Interface, env.RestClientConfig,
			namespace, clusterName, postgres.AppDBName,
			apiv1.ApplicationUserSecretSuffix, query)
		Expect(err).ToNot(HaveOccurred())
	})
}

// insertRecordIntoTable insert an entry into a table
func insertRecordIntoTable(tableName string, value int, conn *sql.DB) {
	_, err := conn.Exec(fmt.Sprintf("INSERT INTO %s VALUES (%d)", tableName, value))
	Expect(err).ToNot(HaveOccurred())
}

func QueryMatchExpectationPredicate(
	pod *corev1.Pod,
	dbname exec.DatabaseName,
	query string,
	expectedOutput string,
) func(g Gomega) {
	return func(g Gomega) {
		// executor
		stdout, stderr, err := exec.QueryInInstancePod(
			env.Ctx, env.Client, env.Interface, env.RestClientConfig,
			exec.PodLocator{Namespace: pod.Namespace, PodName: pod.Name},
			dbname,
			query,
		)
		if err != nil {
			GinkgoWriter.Printf("stdout: %v\nstderr: %v", stdout, stderr)
		}
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(strings.Trim(stdout, "\n")).To(BeEquivalentTo(expectedOutput),
			fmt.Sprintf("expected query %q to return %q", query, expectedOutput))
	}
}

func roleExistsQuery(roleName string) string {
	return fmt.Sprintf("SELECT EXISTS(SELECT 1 FROM pg_catalog.pg_roles WHERE rolname='%v')", roleName)
}

func databaseExistsQuery(dbName string) string {
	return fmt.Sprintf("SELECT EXISTS(SELECT 1 FROM pg_catalog.pg_database WHERE datname='%v')", dbName)
}

// AssertDataExpectedCount verifies that an expected amount of rows exists on the table
func AssertDataExpectedCount(
	env *environment.TestingEnvironment,
	tl TableLocator,
	expectedValue int,
) {
	By(fmt.Sprintf("verifying test data in table %v (cluster %v, database %v, tablespace %v)",
		tl.TableName, tl.ClusterName, tl.DatabaseName, tl.Tablespace), func() {
		row, err := postgres.RunQueryRowOverForward(
			env.Ctx,
			env.Client,
			env.Interface,
			env.RestClientConfig,
			tl.Namespace,
			tl.ClusterName,
			tl.DatabaseName,
			apiv1.ApplicationUserSecretSuffix,
			fmt.Sprintf("SELECT COUNT(*) FROM %s", tl.TableName),
		)
		Expect(err).ToNot(HaveOccurred())

		var nRows int
		err = row.Scan(&nRows)
		Expect(err).ToNot(HaveOccurred())
		Expect(nRows).Should(BeEquivalentTo(expectedValue))
	})
}

// AssertLargeObjectValue verifies the presence of a Large Object given by its OID and data
func AssertLargeObjectValue(namespace, clusterName string, oid int, data string) {
	By("verifying large object", func() {
		query := fmt.Sprintf("SELECT encode(lo_get(%v), 'escape');", oid)
		Eventually(func() (string, error) {
			// We keep getting the pod, since there could be a new pod with the same name
			primaryPod, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
			if err != nil {
				return "", err
			}
			stdout, _, err := exec.QueryInInstancePod(
				env.Ctx, env.Client, env.Interface, env.RestClientConfig,
				exec.PodLocator{
					Namespace: primaryPod.Namespace,
					PodName:   primaryPod.Name,
				},
				postgres.AppDBName,
				query)
			if err != nil {
				return "", err
			}
			return strings.Trim(stdout, "\n"), nil
		}, testTimeouts[timeouts.LargeObject]).Should(BeEquivalentTo(data))
	})
}

// AssertClusterStandbysAreStreaming verifies that all the standbys of a cluster have a wal-receiver running.
func AssertClusterStandbysAreStreaming(namespace string, clusterName string, timeout int32) {
	query := "SELECT count(*) FROM pg_catalog.pg_stat_wal_receiver"
	Eventually(func() error {
		standbyPods, err := clusterutils.GetReplicas(env.Ctx, env.Client, namespace, clusterName)
		if err != nil {
			return err
		}

		for _, pod := range standbyPods.Items {
			out, _, err := exec.QueryInInstancePod(
				env.Ctx, env.Client, env.Interface, env.RestClientConfig,
				exec.PodLocator{
					Namespace: pod.Namespace,
					PodName:   pod.Name,
				},
				postgres.PostgresDBName,
				query)
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

		for i := 1; i < 4; i++ {
			podName := fmt.Sprintf("%v-%v", clusterName, i)
			podNamespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      podName,
			}
			query := "SELECT count(*) > 0 FROM tps.tl WHERE timeline = '00000002'"
			Eventually(func() (string, error) {
				pod := &corev1.Pod{}
				if err := env.Client.Get(env.Ctx, podNamespacedName, pod); err != nil {
					return "", err
				}
				out, _, err := exec.QueryInInstancePod(
					env.Ctx, env.Client, env.Interface, env.RestClientConfig,
					exec.PodLocator{
						Namespace: pod.Namespace,
						PodName:   pod.Name,
					},
					postgres.AppDBName,
					query)
				return strings.TrimSpace(out), err
			}, timeout).Should(BeEquivalentTo("t"),
				"Pod %v should have moved to timeline 2", podName)
		}
	})

	By("having all the instances ready", func() {
		AssertClusterIsReady(namespace, clusterName, testTimeouts[timeouts.ClusterIsReady], env)
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
		pod := &corev1.Pod{}
		err := env.Client.Get(env.Ctx, namespacedName, pod)
		Expect(err).ToNot(HaveOccurred())
		out, _, err := exec.EventuallyExecQueryInInstancePod(
			env.Ctx, env.Client, env.Interface, env.RestClientConfig,
			exec.PodLocator{
				Namespace: pod.Namespace,
				PodName:   pod.Name,
			}, postgres.AppDBName,
			query,
			RetryTimeout,
			PollingTime,
		)
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
			cluster, err = clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
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
		namespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      newPrimaryPod,
		}
		pod := corev1.Pod{}
		err := env.Client.Get(env.Ctx, namespacedName, &pod)
		Expect(err).ToNot(HaveOccurred())
		// Expect write operation to succeed
		query := "CREATE TABLE IF NOT EXISTS assert_new_primary(var1 text);"
		_, _, err = exec.EventuallyExecQueryInInstancePod(
			env.Ctx, env.Client, env.Interface, env.RestClientConfig,
			exec.PodLocator{
				Namespace: pod.Namespace,
				PodName:   pod.Name,
			}, postgres.AppDBName,
			query,
			RetryTimeout,
			PollingTime,
		)
		Expect(err).ToNot(HaveOccurred())
	})
}

// CheckPointAndSwitchWalOnPrimary trigger a checkpoint and switch wal on primary pod and returns the latest WAL file
func CheckPointAndSwitchWalOnPrimary(namespace, clusterName string) string {
	var latestWAL string
	By("trigger checkpoint and switch wal on primary", func() {
		pod, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
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
		pod, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		primary := pod.GetName()
		latestWAL := switchWalAndGetLatestArchive(namespace, primary)
		latestWALPath = minio.GetFilePath(serverName, latestWAL+".gz")
	})

	By(fmt.Sprintf("verify the existence of WAL %v in minio", latestWALPath), func() {
		Eventually(func() (int, error) {
			// WALs are compressed with gzip in the fixture
			return minio.CountFiles(minioEnv, latestWALPath)
		}, testTimeouts[timeouts.WalsInMinio]).Should(BeEquivalentTo(1))
	})
}

func AssertScheduledBackupsAreScheduled(namespace string, backupYAMLPath string, timeout int) {
	CreateResourceFromFile(namespace, backupYAMLPath)
	scheduledBackupName, err := yaml.GetResourceNameFromYAML(env.Scheme, backupYAMLPath)
	Expect(err).NotTo(HaveOccurred())

	// We expect the scheduled backup to be scheduled before a
	// timeout
	scheduledBackupNamespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      scheduledBackupName,
	}

	Eventually(func() (*metav1.Time, error) {
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
		Eventually(func() (string, error) {
			stdOut, stdErr, err := exec.QueryInInstancePod(
				env.Ctx, env.Client, env.Interface, env.RestClientConfig,
				exec.PodLocator{
					Namespace: pod.Namespace,
					PodName:   pod.Name,
				},
				postgres.PostgresDBName,
				"select pg_catalog.pg_is_in_recovery()")
			if err != nil {
				GinkgoWriter.Printf("stdout: %v\nstderr: %v\n", stdOut, stdErr)
			}
			return strings.Trim(stdOut, "\n"), err
		}, 300, 10).Should(BeEquivalentTo(boolPGOutput(expectedValue)))
	})
}

func boolPGOutput(expectedValue bool) string {
	stringExpectedValue := "f"
	if expectedValue {
		stringExpectedValue = "t"
	}
	return stringExpectedValue
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
	checkQuery := fmt.Sprintf("SELECT count(*) FROM %v", testTableName)

	tableLocator := TableLocator{
		Namespace:    namespace,
		ClusterName:  srcClusterName,
		DatabaseName: srcClusterDBName,
		TableName:    testTableName,
	}
	AssertCreateTestData(env, tableLocator)

	By("creating replica cluster", func() {
		replicaClusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, replicaClusterSample)
		Expect(err).ToNot(HaveOccurred())
		AssertCreateCluster(namespace, replicaClusterName, replicaClusterSample, env)
		// Get primary from replica cluster
		Eventually(func() error {
			primaryReplicaCluster, err = clusterutils.GetPrimary(env.Ctx, env.Client, namespace,
				replicaClusterName)
			return err
		}, 30, 3).Should(Succeed())
		AssertPgRecoveryMode(primaryReplicaCluster, true)
	})

	By("checking data have been copied correctly in replica cluster", func() {
		Eventually(func() (string, error) {
			stdOut, _, err := exec.QueryInInstancePod(
				env.Ctx, env.Client, env.Interface, env.RestClientConfig,
				exec.PodLocator{
					Namespace: primaryReplicaCluster.Namespace,
					PodName:   primaryReplicaCluster.Name,
				},
				exec.DatabaseName(srcClusterDBName),
				checkQuery)
			return strings.Trim(stdOut, "\n"), err
		}, 180, 10).Should(BeEquivalentTo("2"))
	})

	By("writing some new data to the source cluster", func() {
		forwardSource, connSource, err := postgres.ForwardPSQLConnection(
			env.Ctx,
			env.Client,
			env.Interface,
			env.RestClientConfig,
			namespace,
			srcClusterName,
			srcClusterDBName,
			apiv1.ApplicationUserSecretSuffix,
		)
		defer func() {
			_ = connSource.Close()
			forwardSource.Close()
		}()
		Expect(err).ToNot(HaveOccurred())
		insertRecordIntoTable(testTableName, 3, connSource)
	})

	By("checking new data have been copied correctly in replica cluster", func() {
		Eventually(func() (string, error) {
			stdOut, _, err := exec.QueryInInstancePod(
				env.Ctx, env.Client, env.Interface, env.RestClientConfig,
				exec.PodLocator{
					Namespace: primaryReplicaCluster.Namespace,
					PodName:   primaryReplicaCluster.Name,
				},
				exec.DatabaseName(srcClusterDBName),
				checkQuery)
			return strings.Trim(stdOut, "\n"), err
		}, 180, 15).Should(BeEquivalentTo("3"))
	})

	if srcClusterDBName != "app" {
		// verify the replica database created followed the source database, rather than
		// default to the "app" db and user
		By("checking that in replica cluster there is no database app and user app", func() {
			Eventually(QueryMatchExpectationPredicate(primaryReplicaCluster, postgres.PostgresDBName,
				databaseExistsQuery("app"), "f"), 30).Should(Succeed())
			Eventually(QueryMatchExpectationPredicate(primaryReplicaCluster, postgres.PostgresDBName,
				roleExistsQuery("app"), "f"), 30).Should(Succeed())
		})
	}
}

// AssertDetachReplicaModeCluster verifies that a replica cluster can be detached from the
// source cluster, and its target primary can be promoted. As such, new write operation
// on the source cluster shouldn't be received anymore by the detached replica cluster.
// Also, make sure the bootstrap fields database and owner of the replica cluster are
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

	var referenceTime time.Time
	By("taking the reference time before the detaching", func() {
		Eventually(func(g Gomega) {
			referenceCondition, err := backups.GetConditionsInClusterStatus(
				env.Ctx, env.Client,
				namespace, replicaClusterName,
				apiv1.ConditionClusterReady)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(referenceCondition.Status).To(BeEquivalentTo(corev1.ConditionTrue))
			g.Expect(referenceCondition).ToNot(BeNil())
			referenceTime = referenceCondition.LastTransitionTime.Time
		}, 60, 5).Should(Succeed())
	})

	By("disabling the replica mode", func() {
		Eventually(func(g Gomega) {
			_, _, err := run.Unchecked(fmt.Sprintf(
				"kubectl patch cluster %v -n %v  -p '{\"spec\":{\"replica\":{\"enabled\":false}}}'"+
					" --type='merge'",
				replicaClusterName, namespace))
			g.Expect(err).ToNot(HaveOccurred())
		}, 60, 5).Should(Succeed())
	})

	By("ensuring the replica cluster got promoted and restarted", func() {
		Eventually(func(g Gomega) {
			cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, replicaClusterName)
			g.Expect(err).ToNot(HaveOccurred())
			condition, err := backups.GetConditionsInClusterStatus(
				env.Ctx, env.Client,
				namespace, cluster.Name,
				apiv1.ConditionClusterReady)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(condition).ToNot(BeNil())
			g.Expect(condition.Status).To(BeEquivalentTo(corev1.ConditionTrue))
			g.Expect(condition.LastTransitionTime.Time).To(BeTemporally(">", referenceTime))
		}).WithTimeout(60 * time.Second).Should(Succeed())
		AssertClusterIsReady(namespace, replicaClusterName, testTimeouts[timeouts.ClusterIsReady], env)
	})

	By("verifying write operation on the replica cluster primary pod", func() {
		query := "CREATE TABLE IF NOT EXISTS replica_cluster_primary AS VALUES (1),(2);"
		// Expect write operation to succeed
		Eventually(func(g Gomega) {
			var err error

			// Get primary from replica cluster
			primaryReplicaCluster, err = clusterutils.GetPrimary(env.Ctx, env.Client, namespace,
				replicaClusterName)
			g.Expect(err).ToNot(HaveOccurred())
			_, _, err = exec.QueryInInstancePod(
				env.Ctx, env.Client, env.Interface, env.RestClientConfig,
				exec.PodLocator{
					Namespace: primaryReplicaCluster.Namespace,
					PodName:   primaryReplicaCluster.Name,
				}, exec.DatabaseName(srcDatabaseName),
				query,
			)
			g.Expect(err).ToNot(HaveOccurred())
		}, 300, 15).Should(Succeed())
	})

	By("verifying the replica database doesn't exist in the replica cluster", func() {
		// Application database configuration is skipped for replica clusters,
		// so we expect these to not be present
		Eventually(QueryMatchExpectationPredicate(primaryReplicaCluster, postgres.PostgresDBName,
			databaseExistsQuery(replicaDatabaseName), "f"), 30).Should(Succeed())
		Eventually(QueryMatchExpectationPredicate(primaryReplicaCluster, postgres.PostgresDBName,
			roleExistsQuery(replicaUserName), "f"), 30).Should(Succeed())
	})

	By("writing some new data to the source cluster", func() {
		tableLocator := TableLocator{
			Namespace:    namespace,
			ClusterName:  srcClusterName,
			DatabaseName: srcDatabaseName,
			TableName:    testTableName,
		}
		AssertCreateTestData(env, tableLocator)
	})

	By("verifying that replica cluster was not modified", func() {
		outTables, stdErr, err := exec.EventuallyExecQueryInInstancePod(
			env.Ctx, env.Client, env.Interface, env.RestClientConfig,
			exec.PodLocator{
				Namespace: primaryReplicaCluster.Namespace,
				PodName:   primaryReplicaCluster.Name,
			}, exec.DatabaseName(srcDatabaseName),
			"\\dt",
			RetryTimeout,
			PollingTime,
		)
		if err != nil {
			GinkgoWriter.Printf("stdout: %v\nstderr: %v\n", outTables, stdErr)
		}
		Expect(err).ToNot(HaveOccurred())
		Expect(strings.Contains(outTables, testTableName), err).Should(BeFalse())
	})
}

func AssertWritesToReplicaFails(
	namespace, service, appDBName, appDBUser, appDBPass string,
) {
	By(fmt.Sprintf("Verifying %v service doesn't allow writes", service), func() {
		Eventually(func(g Gomega) {
			forwardConn, conn, err := postgres.ForwardPSQLServiceConnection(
				env.Ctx, env.Interface, env.RestClientConfig,
				namespace, service,
				appDBName, appDBUser, appDBPass)
			defer func() {
				_ = conn.Close()
				forwardConn.Close()
			}()
			g.Expect(err).ToNot(HaveOccurred())

			var rawValue string
			// Expect to be connected to a replica
			row := conn.QueryRow("SELECT pg_catalog.pg_is_in_recovery()")
			err = row.Scan(&rawValue)
			g.Expect(err).ToNot(HaveOccurred())
			isReplica := strings.TrimSpace(rawValue)
			g.Expect(isReplica).To(BeEquivalentTo("true"))

			// Expect to be in a read-only transaction
			_, err = conn.Exec("CREATE TABLE IF NOT EXISTS table1(var1 text)")
			g.Expect(err).To(HaveOccurred())
			g.Expect(err.Error()).Should(ContainSubstring("cannot execute CREATE TABLE in a read-only transaction"))
		}, RetryTimeout).Should(Succeed())
	})
}

func AssertWritesToPrimarySucceeds(namespace, service, appDBName, appDBUser, appDBPass string) {
	By(fmt.Sprintf("Verifying %v service correctly manages writes", service), func() {
		Eventually(func(g Gomega) {
			forwardConn, conn, err := postgres.ForwardPSQLServiceConnection(
				env.Ctx, env.Interface, env.RestClientConfig,
				namespace, service,
				appDBName, appDBUser, appDBPass)
			defer func() {
				_ = conn.Close()
				forwardConn.Close()
			}()
			g.Expect(err).ToNot(HaveOccurred())

			var rawValue string
			// Expect to be connected to a primary
			row := conn.QueryRow("SELECT pg_catalog.pg_is_in_recovery()")
			err = row.Scan(&rawValue)
			g.Expect(err).ToNot(HaveOccurred())
			isReplica := strings.TrimSpace(rawValue)
			g.Expect(isReplica).To(BeEquivalentTo("false"))

			// Expect to be able to write
			_, err = conn.Exec("CREATE TABLE IF NOT EXISTS table1(var1 text)")
			g.Expect(err).ToNot(HaveOccurred())
		}, RetryTimeout).Should(Succeed())
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
		AssertClusterIsReady(namespace, clusterName, testTimeouts[timeouts.ClusterIsReady], env)
	})

	// Node 1 should be the primary, so the -rw service should
	// point there. We verify this.
	By("having the current primary on node1", func() {
		rwServiceName := clusterName + "-rw"
		endpointSlice, err := testsUtils.GetEndpointSliceByServiceName(env.Ctx, env.Client, namespace, rwServiceName)
		Expect(err).ToNot(HaveOccurred())

		pod := &corev1.Pod{}
		podName := clusterName + "-1"
		err = env.Client.Get(env.Ctx, types.NamespacedName{Namespace: namespace, Name: podName}, pod)
		Expect(err).ToNot(HaveOccurred())
		Expect(testsUtils.FirstEndpointSliceIP(endpointSlice)).To(BeEquivalentTo(pod.Status.PodIP))
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

		_, err = postgres.RunExecOverForward(
			env.Ctx, env.Client, env.Interface, env.RestClientConfig,
			namespace, clusterName, postgres.AppDBName,
			apiv1.ApplicationUserSecretSuffix, query)
		Expect(err).ToNot(HaveOccurred())
	})

	By("starting load", func() {
		// We set up Apache Benchmark and webtest. Apache Benchmark, a load generator,
		// continuously calls the webtest api to execute inserts
		// on the postgres primary. We make sure that the first
		// records appear on the database before moving to the next
		// step.
		_, _, err = run.Run("kubectl create -n " + namespace +
			" -f " + webTestFile)
		Expect(err).ToNot(HaveOccurred())

		_, _, err = run.Run("kubectl create -n " + namespace +
			" -f " + webTestJob)
		Expect(err).ToNot(HaveOccurred())

		primaryPodName := clusterName + "-1"
		primaryPodNamespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      primaryPodName,
		}

		query := "SELECT count(*) > 0 FROM tps.tl"
		Eventually(func() (string, error) {
			primaryPod := &corev1.Pod{}
			if err = env.Client.Get(env.Ctx, primaryPodNamespacedName, primaryPod); err != nil {
				return "", err
			}
			out, _, err := exec.QueryInInstancePod(
				env.Ctx, env.Client, env.Interface, env.RestClientConfig,
				exec.PodLocator{
					Namespace: primaryPod.Namespace,
					PodName:   primaryPod.Name,
				},
				postgres.AppDBName,
				query)
			return strings.TrimSpace(out), err
		}, RetryTimeout).Should(BeEquivalentTo("t"))
	})

	By("deleting the primary", func() {
		// The primary is force-deleted.
		quickDelete := &ctrlclient.DeleteOptions{
			GracePeriodSeconds: &quickDeletionPeriod,
		}
		lm := clusterName + "-1"
		err = podutils.Delete(env.Ctx, env.Client, namespace, lm, quickDelete)
		Expect(err).ToNot(HaveOccurred())
	})

	AssertStandbysFollowPromotion(namespace, clusterName, maxReattachTime)

	AssertWritesResumedBeforeTimeout(namespace, clusterName, maxFailoverTime)
}

func AssertCustomMetricsResourcesExist(namespace, sampleFile string, configMapsCount, secretsCount int) {
	By("verifying the custom metrics ConfigMaps and Secrets exist", func() {
		// Create the ConfigMaps and a Secret
		_, _, err := run.Run("kubectl apply -n " + namespace + " -f " + sampleFile)
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
	env *environment.TestingEnvironment,
	namespace,
	clusterName,
	targetDBName,
	tableName string,
) {
	By(fmt.Sprintf("creating target database '%v' and table '%v'", targetDBName, tableName), func() {
		// We need to gather the cluster primary to create the database via superuser
		currentPrimary, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())

		appUser, _, err := secrets.GetCredentials(
			env.Ctx, env.Client,
			clusterName, namespace, apiv1.ApplicationUserSecretSuffix,
		)
		Expect(err).ToNot(HaveOccurred())

		// Create database
		createDBQuery := fmt.Sprintf("CREATE DATABASE %v OWNER %v", targetDBName, appUser)
		_, _, err = exec.QueryInInstancePod(
			env.Ctx, env.Client, env.Interface, env.RestClientConfig,
			exec.PodLocator{
				Namespace: currentPrimary.Namespace,
				PodName:   currentPrimary.Name,
			},
			postgres.PostgresDBName,
			createDBQuery)
		Expect(err).ToNot(HaveOccurred())

		// Open a connection to the newly created database
		forward, conn, err := postgres.ForwardPSQLConnection(
			env.Ctx,
			env.Client,
			env.Interface,
			env.RestClientConfig,
			namespace,
			clusterName,
			targetDBName,
			apiv1.ApplicationUserSecretSuffix,
		)
		defer func() {
			_ = conn.Close()
			forward.Close()
		}()
		Expect(err).ToNot(HaveOccurred())

		// Create table on target database
		createTableQuery := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %v (id int);", tableName)
		_, err = conn.Exec(createTableQuery)
		Expect(err).ToNot(HaveOccurred())

		// Grant a permission
		grantRoleQuery := "GRANT SELECT ON all tables in schema public to pg_monitor;"
		_, err = conn.Exec(grantRoleQuery)
		Expect(err).ToNot(HaveOccurred())
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
		rwService := services.GetReadWriteServiceName(clusterName)

		AssertConnection(namespace, rwService, appDB, appUser, appPassword, env)
	})
}

func AssertMetricsData(namespace, targetOne, targetTwo, targetSecret string, cluster *apiv1.Cluster) {
	By("collect and verify metric being exposed with target databases", func() {
		podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, cluster.Name)
		Expect(err).ToNot(HaveOccurred())
		for _, pod := range podList.Items {
			podName := pod.GetName()
			out, err := proxy.RetrieveMetricsFromInstance(env.Ctx, env.Interface, pod, cluster.IsMetricsTLSEnabled())
			Expect(err).ToNot(HaveOccurred())
			Expect(strings.Contains(out,
				fmt.Sprintf(`cnpg_some_query_rows{datname="%v"} 0`, targetOne))).Should(BeTrue(),
				"Metric collection issues on %v.\nCollected metrics:\n%v", podName, out)
			Expect(strings.Contains(out,
				fmt.Sprintf(`cnpg_some_query_rows{datname="%v"} 0`, targetTwo))).Should(BeTrue(),
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
	cluster, caPair, err := secrets.CreateSecretCA(
		env.Ctx, env.Client,
		namespace, clusterName, caSecName, includeCAPrivateKey,
	)
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
	_, caPair, err := secrets.CreateSecretCA(
		env.Ctx, env.Client,
		namespace, clusterName, caSecName, includeCAPrivateKey)
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
			stdout, stderr, err := exec.Command(
				env.Ctx, env.Interface, env.RestClientConfig,
				appPod, appPod.Spec.Containers[0].Name, &timeout,
				"psql", dsn, "-tAc", "SELECT 1")
			return stdout, stderr, err
		}, 360).Should(BeEquivalentTo("1\n"))
	})
}

func AssertClusterAsyncReplica(namespace, sourceClusterFile, restoreClusterFile, tableName string) {
	By("Async Replication into external cluster", func() {
		restoredClusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, restoreClusterFile)
		Expect(err).ToNot(HaveOccurred())
		// Add additional data to the source cluster
		sourceClusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, sourceClusterFile)
		Expect(err).ToNot(HaveOccurred())
		CreateResourceFromFile(namespace, restoreClusterFile)
		// We give more time than the usual 600s, since the recovery is slower
		AssertClusterIsReady(namespace, restoredClusterName, testTimeouts[timeouts.ClusterIsReadySlow], env)

		// Test data should be present on restored primary
		restoredPrimary, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, restoredClusterName)
		Expect(err).ToNot(HaveOccurred())

		// We need the credentials from the source cluster because the replica cluster
		// doesn't create the credentials on its own namespace
		appUser, appUserPass, err := secrets.GetCredentials(
			env.Ctx,
			env.Client,
			sourceClusterName,
			namespace,
			apiv1.ApplicationUserSecretSuffix,
		)
		Expect(err).ToNot(HaveOccurred())

		forwardRestored, connRestored, err := postgres.ForwardPSQLConnectionWithCreds(
			env.Ctx,
			env.Client,
			env.Interface,
			env.RestClientConfig,
			namespace,
			restoredClusterName,
			postgres.AppDBName,
			appUser,
			appUserPass,
		)
		defer func() {
			_ = connRestored.Close()
			forwardRestored.Close()
		}()
		Expect(err).ToNot(HaveOccurred())

		row := connRestored.QueryRow(fmt.Sprintf("SELECT count(*) FROM %s", tableName))
		var countString string
		err = row.Scan(&countString)
		Expect(err).ToNot(HaveOccurred())
		Expect(countString).To(BeEquivalentTo("2"))

		forwardSource, connSource, err := postgres.ForwardPSQLConnection(
			env.Ctx,
			env.Client,
			env.Interface,
			env.RestClientConfig,
			namespace,
			sourceClusterName,
			postgres.AppDBName,
			apiv1.ApplicationUserSecretSuffix,
		)
		defer func() {
			_ = connSource.Close()
			forwardSource.Close()
		}()
		Expect(err).ToNot(HaveOccurred())

		// Insert new data in the source cluster
		insertRecordIntoTable(tableName, 3, connSource)
		AssertArchiveWalOnMinio(namespace, sourceClusterName, sourceClusterName)
		tableLocator := TableLocator{
			Namespace:    namespace,
			ClusterName:  sourceClusterName,
			DatabaseName: postgres.AppDBName,
			TableName:    tableName,
		}
		AssertDataExpectedCount(env, tableLocator, 3)

		cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, restoredClusterName)
		Expect(err).ToNot(HaveOccurred())
		expectedReplicas := cluster.Spec.Instances - 1
		// Cascading replicas should be attached to primary replica
		connectedReplicas, err := postgres.CountReplicas(
			env.Ctx, env.Client, env.Interface, env.RestClientConfig,
			restoredPrimary, RetryTimeout,
		)
		Expect(connectedReplicas, err).To(BeEquivalentTo(expectedReplicas))
	})
}

func AssertClusterRestoreWithApplicationDB(namespace, restoreClusterFile, tableName string) {
	restoredClusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, restoreClusterFile)
	Expect(err).ToNot(HaveOccurred())

	By("Restoring a backup in a new cluster", func() {
		CreateResourceFromFile(namespace, restoreClusterFile)

		// We give more time than the usual 600s, since the recovery is slower
		AssertClusterIsReady(namespace, restoredClusterName, testTimeouts[timeouts.ClusterIsReadySlow], env)

		// Test data should be present on restored primary
		tableLocator := TableLocator{
			Namespace:    namespace,
			ClusterName:  restoredClusterName,
			DatabaseName: postgres.AppDBName,
			TableName:    tableName,
		}
		AssertDataExpectedCount(env, tableLocator, 2)
	})

	By("Ensuring the restored cluster is on timeline 2", func() {
		row, err := postgres.RunQueryRowOverForward(
			env.Ctx,
			env.Client,
			env.Interface,
			env.RestClientConfig,
			namespace,
			restoredClusterName,
			postgres.AppDBName,
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
	appUser, appUserPass, err := secrets.GetCredentials(
		env.Ctx, env.Client,
		restoredClusterName, namespace,
		apiv1.ApplicationUserSecretSuffix)
	Expect(err).ToNot(HaveOccurred())
	secretName := restoredClusterName + apiv1.ApplicationUserSecretSuffix

	By("checking the restored cluster with pre-defined app password connectable", func() {
		AssertApplicationDatabaseConnection(
			namespace,
			restoredClusterName,
			appUser,
			postgres.AppDBName,
			appUserPass,
			secretName,
		)
	})

	By("update user application password for restored cluster and verify connectivity", func() {
		const newPassword = "eeh2Zahohx" //nolint:gosec
		AssertUpdateSecret("password", newPassword, secretName, namespace, restoredClusterName, 30, env)

		AssertApplicationDatabaseConnection(
			namespace,
			restoredClusterName,
			appUser,
			postgres.AppDBName,
			newPassword,
			secretName,
		)
	})
}

func AssertClusterRestore(namespace, restoreClusterFile, tableName string) {
	restoredClusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, restoreClusterFile)
	Expect(err).ToNot(HaveOccurred())

	By("Restoring a backup in a new cluster", func() {
		CreateResourceFromFile(namespace, restoreClusterFile)

		// We give more time than the usual 600s, since the recovery is slower
		AssertClusterIsReady(namespace, restoredClusterName, testTimeouts[timeouts.ClusterIsReadySlow], env)

		// Test data should be present on restored primary
		primary := restoredClusterName + "-1"
		tableLocator := TableLocator{
			Namespace:    namespace,
			ClusterName:  restoredClusterName,
			DatabaseName: postgres.AppDBName,
			TableName:    tableName,
		}
		AssertDataExpectedCount(env, tableLocator, 2)

		// Restored primary should be on timeline 2
		out, _, err := exec.QueryInInstancePod(
			env.Ctx, env.Client, env.Interface, env.RestClientConfig,
			exec.PodLocator{
				Namespace: namespace,
				PodName:   primary,
			},
			postgres.AppDBName,
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
		cluster, err = importdb.ImportDatabaseMicroservice(env.Ctx, env.Client, namespace, clusterName,
			clusterWithExternalClusterName, "", databaseName)
		Expect(err).ToNot(HaveOccurred())
		// We give more time than the usual 600s, since the recovery is slower
		AssertClusterIsReady(namespace, clusterWithExternalClusterName,
			testTimeouts[timeouts.ClusterIsReadySlow], env)
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
		Eventually(func() (*metav1.Time, error) {
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
			_, _, err = run.Unchecked(cmd)
			if err != nil {
				return err
			}
			return nil
		}, 60, 5).Should(Succeed())
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
			_, _, err = run.Unchecked(cmd)
			if err != nil {
				return err
			}
			return nil
		}, 60, 5).Should(Succeed())
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
	AssertClusterIsReady(namespace, clusterName, testTimeouts[timeouts.ClusterIsReadySlow], env)

	// Gather the recovered cluster primary
	primaryInfo, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
	Expect(err).ToNot(HaveOccurred())
	secretName := clusterName + apiv1.ApplicationUserSecretSuffix

	By("Ensuring the restored cluster is on timeline 3", func() {
		// Restored primary should be on timeline 3
		row, err := postgres.RunQueryRowOverForward(
			env.Ctx,
			env.Client,
			env.Interface,
			env.RestClientConfig,
			namespace,
			clusterName,
			postgres.AppDBName,
			apiv1.ApplicationUserSecretSuffix,
			"select substring(pg_walfile_name(pg_current_wal_lsn()), 1, 8)",
		)
		Expect(err).ToNot(HaveOccurred())

		var currentWalLsn string
		err = row.Scan(&currentWalLsn)
		Expect(err).ToNot(HaveOccurred())
		Expect(currentWalLsn).To(Equal(lsn))

		// Restored standby should be attached to restored primary
		Expect(postgres.CountReplicas(
			env.Ctx, env.Client, env.Interface, env.RestClientConfig,
			primaryInfo, RetryTimeout)).To(BeEquivalentTo(2))
	})

	By(fmt.Sprintf("after restored, 3rd entry should not be exists in table '%v'", tableName), func() {
		// Only 2 entries should be present
		tableLocator := TableLocator{
			Namespace:    namespace,
			ClusterName:  clusterName,
			DatabaseName: postgres.AppDBName,
			TableName:    tableName,
		}
		AssertDataExpectedCount(env, tableLocator, 2)
	})

	// Gather credentials
	appUser, appUserPass, err := secrets.GetCredentials(
		env.Ctx, env.Client,
		clusterName, namespace, apiv1.ApplicationUserSecretSuffix)
	Expect(err).ToNot(HaveOccurred())

	By("checking the restored cluster with auto generated app password connectable", func() {
		AssertApplicationDatabaseConnection(
			namespace,
			clusterName,
			appUser,
			postgres.AppDBName,
			appUserPass,
			secretName,
		)
	})

	By("update user application password for restored cluster and verify connectivity", func() {
		const newPassword = "eeh2Zahohx" //nolint:gosec
		AssertUpdateSecret("password", newPassword, secretName, namespace, clusterName, 30, env)
		AssertApplicationDatabaseConnection(
			namespace,
			clusterName,
			appUser,
			postgres.AppDBName,
			newPassword,
			secretName,
		)
	})
}

func AssertClusterWasRestoredWithPITR(namespace, clusterName, tableName, lsn string) {
	By("restoring a backup cluster with PITR in a new cluster", func() {
		// We give more time than the usual 600s, since the recovery is slower
		AssertClusterIsReady(namespace, clusterName, testTimeouts[timeouts.ClusterIsReadySlow], env)
		primaryInfo, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())

		// Restored primary should be on timeline 3
		row, err := postgres.RunQueryRowOverForward(
			env.Ctx,
			env.Client,
			env.Interface,
			env.RestClientConfig,
			namespace,
			clusterName,
			postgres.AppDBName,
			apiv1.ApplicationUserSecretSuffix,
			"select substring(pg_walfile_name(pg_current_wal_lsn()), 1, 8)",
		)
		Expect(err).ToNot(HaveOccurred())

		var currentWalLsn string
		err = row.Scan(&currentWalLsn)
		Expect(err).ToNot(HaveOccurred())
		Expect(currentWalLsn).To(Equal(lsn))

		// Restored standby should be attached to restored primary
		Expect(postgres.CountReplicas(
			env.Ctx, env.Client, env.Interface, env.RestClientConfig,
			primaryInfo, RetryTimeout)).To(BeEquivalentTo(2))
	})

	By(fmt.Sprintf("after restored, 3rd entry should not be exists in table '%v'", tableName), func() {
		// Only 2 entries should be present
		tableLocator := TableLocator{
			Namespace:    namespace,
			ClusterName:  clusterName,
			DatabaseName: postgres.AppDBName,
			TableName:    tableName,
		}
		AssertDataExpectedCount(env, tableLocator, 2)
	})
}

func AssertArchiveConditionMet(namespace, clusterName, timeout string) {
	By("Waiting for the condition", func() {
		out, _, err := run.Run(fmt.Sprintf(
			"kubectl -n %s wait --for=condition=ContinuousArchiving=true cluster/%s --timeout=%s",
			namespace, clusterName, timeout))
		Expect(err).ToNot(HaveOccurred())
		outPut := strings.TrimSpace(out)
		Expect(outPut).Should(ContainSubstring("condition met"))
	})
}

// switchWalAndGetLatestArchive trigger a new wal and get the name of latest wal file
func switchWalAndGetLatestArchive(namespace, podName string) string {
	_, _, err := exec.QueryInInstancePodWithTimeout(
		env.Ctx, env.Client, env.Interface, env.RestClientConfig,
		exec.PodLocator{
			Namespace: namespace,
			PodName:   podName,
		},
		postgres.PostgresDBName,
		"CHECKPOINT",
		300*time.Second,
	)
	Expect(err).ToNot(HaveOccurred(),
		"failed to trigger a new wal while executing 'switchWalAndGetLatestArchive'")

	out, _, err := exec.QueryInInstancePod(
		env.Ctx, env.Client, env.Interface, env.RestClientConfig,
		exec.PodLocator{
			Namespace: namespace,
			PodName:   podName,
		},
		postgres.PostgresDBName,
		"SELECT pg_catalog.pg_walfile_name(pg_switch_wal())",
	)
	Expect(err).ToNot(
		HaveOccurred(),
		"failed to get latest wal file name while executing 'switchWalAndGetLatestArchive")

	return strings.TrimSpace(out)
}

func createAndAssertPgBouncerPoolerIsSetUp(namespace, poolerYamlFilePath string, expectedInstanceCount int) {
	CreateResourceFromFile(namespace, poolerYamlFilePath)
	Eventually(func() (int32, error) {
		poolerName, err := yaml.GetResourceNameFromYAML(env.Scheme, poolerYamlFilePath)
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
			poolerName, err := yaml.GetResourceNameFromYAML(env.Scheme, poolerYamlFilePath)
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
		poolerName, err := yaml.GetResourceNameFromYAML(env.Scheme, poolerYamlFilePath)
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
	poolerService, err := yaml.GetResourceNameFromYAML(env.Scheme, poolerYamlFilePath)
	Expect(err).ToNot(HaveOccurred())

	appUser, generatedAppUserPassword, err := secrets.GetCredentials(
		env.Ctx, env.Client,
		clusterName, namespace, apiv1.ApplicationUserSecretSuffix)
	Expect(err).ToNot(HaveOccurred())
	AssertConnection(namespace, poolerService, postgres.AppDBName, appUser, generatedAppUserPassword, env)

	// verify that, if pooler type setup read write then it will allow both read and
	// write operations or if pooler type setup read only then it will allow only read operations
	if isPoolerRW {
		AssertWritesToPrimarySucceeds(namespace, poolerService, "app", appUser,
			generatedAppUserPassword)
	} else {
		AssertWritesToReplicaFails(namespace, poolerService, "app", appUser,
			generatedAppUserPassword)
	}
}

func assertPodIsRecreated(namespace, poolerSampleFile string) {
	var podNameBeforeDelete string
	poolerName, err := yaml.GetResourceNameFromYAML(env.Scheme, poolerSampleFile)
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
		_, _, err = run.Run(cmd)
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
					if podNameBeforeDelete != podList.Items[0].GetName() {
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

	poolerName, err := yaml.GetResourceNameFromYAML(env.Scheme, poolerSampleFile)
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
	err = deployments.WaitForReady(env.Ctx, env.Client, deployment, 60)
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
		err := deployments.WaitForReady(env.Ctx, env.Client, deployment, 120)
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
	poolerServiceName, err := yaml.GetResourceNameFromYAML(env.Scheme, poolerYamlFilePath)
	Expect(err).ToNot(HaveOccurred())

	endpointSlice := &discoveryv1.EndpointSlice{}
	Eventually(func(g Gomega) {
		var err error
		endpointSlice, err = testsUtils.GetEndpointSliceByServiceName(env.Ctx, env.Client, namespace, poolerServiceName)
		g.Expect(err).ToNot(HaveOccurred())
	}).Should(Succeed())

	poolerName, err := yaml.GetResourceNameFromYAML(env.Scheme, poolerYamlFilePath)
	Expect(err).ToNot(HaveOccurred())
	podList := &corev1.PodList{}
	err = env.Client.List(env.Ctx, podList, ctrlclient.InNamespace(namespace),
		ctrlclient.MatchingLabels{utils.PgbouncerNameLabel: poolerName})
	Expect(err).ToNot(HaveOccurred())
	Expect(endpointSlice.Endpoints).ToNot(BeEmpty())

	var pgBouncerPods []*corev1.Pod
	for _, endpoint := range endpointSlice.Endpoints {
		ip := endpoint.Addresses[0]
		for podIndex, pod := range podList.Items {
			if pod.Status.PodIP == ip {
				pgBouncerPods = append(pgBouncerPods, &podList.Items[podIndex])
				continue
			}
		}
	}

	Expect(pgBouncerPods).Should(HaveLen(expectedPodCount), "Pod length or IP mismatch in endpoint")
}

// assertPGBouncerHasServiceNameInsideHostParameter makes sure that the service name is contained inside the host file
func assertPGBouncerHasServiceNameInsideHostParameter(namespace, serviceName string, podList *corev1.PodList) {
	for _, pod := range podList.Items {
		command := fmt.Sprintf("kubectl exec -n %s %s -- /bin/bash -c 'grep "+
			" \"host=%s\" controller/configs/pgbouncer.ini'", namespace, pod.Name, serviceName)
		out, _, err := run.Run(command)
		Expect(err).ToNot(HaveOccurred())
		expectedContainedHost := fmt.Sprintf("host=%s", serviceName)
		Expect(out).To(ContainSubstring(expectedContainedHost))
	}
}

// OnlineResizePVC is for verifying if storage can be automatically expanded, or not
func OnlineResizePVC(namespace, clusterName string) {
	walStorageEnabled, err := storage.IsWalStorageEnabled(
		env.Ctx, env.Client,
		namespace, clusterName,
	)
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
				_, _, err := run.Unchecked(cmd)
				return err
			}, 60, 5).Should(Succeed())
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
	walStorageEnabled, err := storage.IsWalStorageEnabled(
		env.Ctx, env.Client,
		namespace, clusterName,
	)
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
				_, _, err := run.Unchecked(cmd)
				return err
			}, 60, 5).Should(Succeed())
		}
	})
	By("deleting Pod and PVCs, first replicas then the primary", func() {
		// Gathering cluster primary
		currentPrimary, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		currentPrimaryWalStorageName := currentPrimary.Name + "-wal"
		quickDelete := &ctrlclient.DeleteOptions{
			GracePeriodSeconds: &quickDeletionPeriod,
		}

		podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		Expect(len(podList.Items), err).To(BeEquivalentTo(3))

		// Iterating through PVC list for deleting pod and PVC for storage expansion
		for _, p := range podList.Items {
			// Comparing cluster pods to not be primary to ensure cluster is healthy.
			// Primary will be eventually deleted
			if !specs.IsPodPrimary(p) {
				// Deleting PVC
				_, _, err = run.Run(
					"kubectl delete pvc " + p.Name + " -n " + namespace + " --wait=false")
				Expect(err).ToNot(HaveOccurred())
				// Deleting WalStorage PVC if needed
				if walStorageEnabled {
					_, _, err = run.Run(
						"kubectl delete pvc " + p.Name + "-wal" + " -n " + namespace + " --wait=false")
					Expect(err).ToNot(HaveOccurred())
				}
				// Deleting standby and replica pods
				err = podutils.Delete(env.Ctx, env.Client, namespace, p.Name, quickDelete)
				Expect(err).ToNot(HaveOccurred())
			}
		}
		AssertClusterIsReady(namespace, clusterName, timeout, env)

		// Deleting primary pvc
		_, _, err = run.Run(
			"kubectl delete pvc " + currentPrimary.Name + " -n " + namespace + " --wait=false")
		Expect(err).ToNot(HaveOccurred())
		// Deleting Primary WalStorage PVC if needed
		if walStorageEnabled {
			_, _, err = run.Run(
				"kubectl delete pvc " + currentPrimaryWalStorageName + " -n " + namespace + " --wait=false")
			Expect(err).ToNot(HaveOccurred())
		}
		// Deleting primary pod
		err = podutils.Delete(env.Ctx, env.Client, namespace, currentPrimary.Name, quickDelete)
		Expect(err).ToNot(HaveOccurred())
	})

	AssertClusterIsReady(namespace, clusterName, timeout, env)
	By("verifying Cluster storage is expanded", func() {
		// Gathering PVC list for comparison
		pvcList, err := storage.GetPVCList(env.Ctx, env.Client, namespace)
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
	env *environment.TestingEnvironment,
	pod *corev1.Pod,
) {
	poolerService, err := yaml.GetResourceNameFromYAML(env.Scheme, poolerYamlFilePath)
	Expect(err).ToNot(HaveOccurred())

	appUser, generatedAppUserPassword, err := secrets.GetCredentials(
		env.Ctx, env.Client,
		clusterName, namespace, apiv1.ApplicationUserSecretSuffix,
	)
	Expect(err).ToNot(HaveOccurred())
	AssertConnection(namespace, poolerService, postgres.AppDBName, appUser, generatedAppUserPassword, env)

	connectionTimeout := time.Second * 10
	dsn := services.CreateDSN(poolerService, appUser, postgres.AppDBName, generatedAppUserPassword,
		services.Require, 5432)
	_, _, err = env.EventuallyExecCommand(env.Ctx, *pod, specs.PostgresContainerName, &connectionTimeout,
		"psql", dsn, "-tAc", "DROP TABLE table1")
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
			"cnpg_pg_stat_bgwriter",
			"cnpg_pg_stat_database",
		}

		if env.PostgresVersion > 16 {
			defaultMetrics = append(defaultMetrics,
				"cnpg_pg_stat_checkpointer",
			)
		}

		podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		for _, pod := range podList.Items {
			podName := pod.GetName()
			out, err := proxy.RetrieveMetricsFromInstance(env.Ctx, env.Interface, pod, tlsEnabled)
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

	if env.PostgresVersion >= 14 {
		cnpgCollectorMetrics = append(cnpgCollectorMetrics,
			"cnpg_collector_wal_records",
			"cnpg_collector_wal_fpi",
			"cnpg_collector_wal_bytes",
			"cnpg_collector_wal_buffers_full",
		)
		if env.PostgresVersion < 18 {
			cnpgCollectorMetrics = append(cnpgCollectorMetrics,
				"cnpg_collector_wal_write",
				"cnpg_collector_wal_sync",
				"cnpg_collector_wal_write_time",
				"cnpg_collector_wal_sync_time",
			)
		}
	}
	By("collecting and verify set of collector metrics on each pod", func() {
		podList, err := clusterutils.ListPods(env.Ctx, env.Client, cluster.Namespace, cluster.Name)
		Expect(err).ToNot(HaveOccurred())
		for _, pod := range podList.Items {
			podName := pod.GetName()
			out, err := proxy.RetrieveMetricsFromInstance(env.Ctx, env.Interface, pod, cluster.IsMetricsTLSEnabled())
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
	yamlContent, err := GetYAMLContent(sampleFilePath)
	if err != nil {
		return wrapErr(err)
	}

	objects, err := yaml.ParseObjectsFromYAML(yamlContent, namespace)
	if err != nil {
		return wrapErr(err)
	}
	for _, obj := range objects {
		_, err := objectsutils.Create(env.Ctx, env.Client, obj)
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
	}, RetryTimeout, PollingTime).Should(Succeed())
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
	yamlContent := data

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

		yamlContent, err = envsubst.Envsubst(envVars, data)
		if err != nil {
			return nil, wrapErr(err)
		}
	}
	return yamlContent, nil
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
	yamlContent, err := GetYAMLContent(sampleFilePath)
	if err != nil {
		return wrapErr(err)
	}

	objects, err := yaml.ParseObjectsFromYAML(yamlContent, namespace)
	if err != nil {
		return wrapErr(err)
	}
	for _, obj := range objects {
		err := objectsutils.Delete(env.Ctx, env.Client, obj)
		if err != nil {
			return wrapErr(err)
		}
	}
	return nil
}

func AssertBackupConditionTimestampChangedInClusterStatus(
	namespace,
	clusterName string,
	clusterConditionType apiv1.ClusterConditionType,
	lastTransactionTimeStamp *metav1.Time,
) {
	By(fmt.Sprintf("waiting for backup condition status in cluster '%v'", clusterName), func() {
		Eventually(func() (bool, error) {
			getBackupCondition, err := backups.GetConditionsInClusterStatus(
				env.Ctx, env.Client,
				namespace, clusterName, clusterConditionType)
			if err != nil {
				return false, err
			}
			return getBackupCondition.LastTransitionTime.After(lastTransactionTimeStamp.Time), nil
		}, 300, 5).Should(BeTrue())
	})
}

func AssertClusterReadinessStatusIsReached(
	namespace,
	clusterName string,
	conditionStatus apiv1.ConditionStatus,
	timeout int,
	env *environment.TestingEnvironment,
) {
	By(fmt.Sprintf("waiting for cluster condition status in cluster '%v'", clusterName), func() {
		Eventually(func() (string, error) {
			clusterCondition, err := backups.GetConditionsInClusterStatus(
				env.Ctx, env.Client,
				namespace, clusterName, apiv1.ConditionClusterReady)
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
			pvcList, err := storage.GetPVCList(env.Ctx, env.Client, namespace)
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
				g.Expect(storage.PvcHasLabels(pvc, expectedLabels)).To(BeTrue(),
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
	GinkgoWriter.Println("Checking slots presence:", expectedSlots, "in pod:", pod.Name)
	Eventually(func() ([]string, error) {
		currentSlots, err := replicationslot.GetReplicationSlotsOnPod(
			env.Ctx, env.Client, env.Interface, env.RestClientConfig,
			namespace, pod.GetName(), postgres.AppDBName)
		return currentSlots, err
	}, 300).Should(ContainElements(expectedSlots),
		func() string {
			return replicationslot.PrintReplicationSlots(
				env.Ctx, env.Client, env.Interface, env.RestClientConfig,
				namespace, clusterName, postgres.AppDBName)
		})

	GinkgoWriter.Println("Verifying slots status for pod", pod.Name)

	for _, slot := range expectedSlots {
		query := fmt.Sprintf(
			"SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_replication_slots "+
				"WHERE slot_name = '%v' AND active = '%t' "+
				"AND temporary = 'f' AND slot_type = 'physical')", slot, isActiveOnReplica)
		if specs.IsPodPrimary(pod) {
			query = fmt.Sprintf(
				"SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_replication_slots "+
					"WHERE slot_name = '%v' AND active = '%t' "+
					"AND temporary = 'f' AND slot_type = 'physical')", slot, isActiveOnPrimary)
		}
		Eventually(func() (string, error) {
			stdout, _, err := exec.QueryInInstancePod(
				env.Ctx, env.Client, env.Interface, env.RestClientConfig,
				exec.PodLocator{
					Namespace: pod.Namespace,
					PodName:   pod.Name,
				},
				postgres.PostgresDBName,
				query)
			return strings.TrimSpace(stdout), err
		}, 300).Should(BeEquivalentTo("t"),
			func() string {
				return replicationslot.PrintReplicationSlots(
					env.Ctx, env.Client, env.Interface, env.RestClientConfig,
					namespace, clusterName, postgres.AppDBName)
			})
	}
}

// AssertClusterReplicationSlotsAligned will compare all the replication slot restart_lsn
// in a cluster. The assertion will succeed if they are all equivalent.
func AssertClusterReplicationSlotsAligned(
	namespace,
	clusterName string,
) {
	podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
	Expect(err).ToNot(HaveOccurred())
	Eventually(func(g Gomega) {
		numPods := len(podList.Items)
		// Capacity calculation: primary has (N-1) HA slots, each of the (N-1) replicas
		// has (N-2) slots. Total LSNs: (N-1) + (N-1)*(N-2) = (N-1)^2
		lsnList := make([]string, 0, (numPods-1)*(numPods-1))
		for _, pod := range podList.Items {
			out, err := replicationslot.GetReplicationSlotLsnsOnPod(
				env.Ctx, env.Client, env.Interface, env.RestClientConfig,
				namespace, clusterName, postgres.AppDBName, pod)
			g.Expect(err).ToNot(HaveOccurred(), "error getting replication slot lsn on pod %v", pod.Name)
			lsnList = append(lsnList, out...)
		}
		g.Expect(replicationslot.AreSameLsn(lsnList)).To(BeTrue())
	}).WithTimeout(300*time.Second).WithPolling(2*time.Second).Should(Succeed(),
		func() string {
			return replicationslot.PrintReplicationSlots(
				env.Ctx, env.Client, env.Interface, env.RestClientConfig,
				namespace, clusterName, postgres.AppDBName)
		})
}

// AssertClusterHAReplicationSlots will verify if the replication slots of each pod
// of the cluster exist and are aligned.
func AssertClusterHAReplicationSlots(namespace, clusterName string) {
	By("verifying all cluster's replication slots exist and are aligned", func() {
		podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		for _, pod := range podList.Items {
			expectedSlots, err := replicationslot.GetExpectedHAReplicationSlotsOnPod(
				env.Ctx, env.Client,
				namespace, clusterName, pod.GetName())
			Expect(err).ToNot(HaveOccurred())
			AssertReplicationSlotsOnPod(namespace, clusterName, pod, expectedSlots, true, false)
		}
		AssertClusterReplicationSlotsAligned(namespace, clusterName)
	})
}

// AssertClusterRollingRestart restarts a given cluster
func AssertClusterRollingRestart(namespace, clusterName string) {
	By(fmt.Sprintf("restarting cluster %v", clusterName), func() {
		cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
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
	AssertClusterIsReady(namespace, clusterName, testTimeouts[timeouts.ClusterIsReadyQuick], env)
}

// AssertPVCCount matches count and pvc List.
func AssertPVCCount(namespace, clusterName string, pvcCount, timeout int) {
	By(fmt.Sprintf("verify cluster %v healthy pvc list", clusterName), func() {
		Eventually(func(g Gomega) {
			cluster, _ := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
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
		cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
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
