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

// Package backup provides Ginkgo/Gomega assertions around backup/restore
// flows: BackupCondition checks, ScheduledBackup scheduling/suspension,
// continuous archiving, restoration (with/without application DB), and
// PITR verification.
package backup

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	clusterasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/cluster"
	pgasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/replication"
	secretsasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/secrets"
	"github.com/cloudnative-pg/cloudnative-pg/tests/internal/resources"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/backups"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/environment"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/exec"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/importdb"
	pgutils "github.com/cloudnative-pg/cloudnative-pg/tests/utils/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/secrets"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/yaml"

	. "github.com/onsi/ginkgo/v2" //nolint
	. "github.com/onsi/gomega"    //nolint
)

// AssertBackupConditionInClusterStatus checks that the backup condition in
// the Cluster's Status eventually becomes "True".
func AssertBackupConditionInClusterStatus(
	env *environment.TestingEnvironment,
	namespace, clusterName string,
) {
	GinkgoHelper()
	By(fmt.Sprintf("waiting for backup condition status in cluster '%v'", clusterName), func() {
		Eventually(func() (string, error) {
			getBackupCondition, err := backups.GetConditionsInClusterStatus(
				env.Ctx, env.Client,
				namespace, clusterName,
				apiv1.ConditionBackup,
			)
			if err != nil {
				return "", err
			}
			return string(getBackupCondition.Status), nil
		}, 300, 5).Should(BeEquivalentTo("True"))
	})
}

// AssertScheduledBackupsAreScheduled creates the ScheduledBackup described
// in backupYAMLPath and asserts that it eventually fires at least twice.
func AssertScheduledBackupsAreScheduled(
	env *environment.TestingEnvironment,
	namespace, backupYAMLPath string,
	timeout int,
) {
	GinkgoHelper()
	resources.CreateResourceFromFile(env, namespace, backupYAMLPath)
	scheduledBackupName, err := yaml.GetResourceNameFromYAML(env.Scheme, backupYAMLPath)
	Expect(err).NotTo(HaveOccurred())

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

	Eventually(func() (int, error) {
		return getScheduledBackupCompleteBackupsCount(env, namespace, scheduledBackupName)
	}, timeout).Should(BeNumerically(">=", 2))
}

// AssertScheduledBackupsImmediate creates the ScheduledBackup described in
// backupYAMLPath, asserts the first backup runs immediately, and checks
// that no additional backups fire within the test window.
func AssertScheduledBackupsImmediate(
	env *environment.TestingEnvironment,
	namespace, backupYAMLPath, scheduledBackupName string,
) {
	GinkgoHelper()
	By("scheduling immediate backups", func() {
		var err error
		resources.CreateResourceFromFile(env, namespace, backupYAMLPath)

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

		Eventually(func() (int, error) {
			currentBackupCount, err := getScheduledBackupCompleteBackupsCount(env, namespace, scheduledBackupName)
			if err != nil {
				return 0, err
			}
			return currentBackupCount, err
		}, 120).Should(BeNumerically("==", 1))
	})
}

// AssertSuspendScheduleBackups suspends and resumes a ScheduledBackup,
// verifying no new backups land while suspended.
func AssertSuspendScheduleBackups(env *environment.TestingEnvironment, namespace, scheduledBackupName string) {
	GinkgoHelper()
	var completedBackupsCount int
	var err error
	scheduledBackupNamespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      scheduledBackupName,
	}
	By("suspending the scheduled backup", func() {
		setScheduledBackupSuspend(env, scheduledBackupNamespacedName, true)
		Eventually(func() bool {
			scheduledBackup := &apiv1.ScheduledBackup{}
			err = env.Client.Get(env.Ctx, scheduledBackupNamespacedName, scheduledBackup)
			return *scheduledBackup.Spec.Suspend
		}, 30).Should(BeTrue())
	})
	By("waiting for ongoing backups to complete", func() {
		Eventually(func() (bool, error) {
			completedBackupsCount, err = getScheduledBackupCompleteBackupsCount(env, namespace, scheduledBackupName)
			if err != nil {
				return false, err
			}
			scheduledBackupRuns, err := getScheduledBackupBackups(env, namespace, scheduledBackupName)
			if err != nil {
				return false, err
			}
			return len(scheduledBackupRuns) == completedBackupsCount, nil
		}, 80).Should(BeTrue())
	})
	By("verifying backup has suspended", func() {
		Consistently(func() (int, error) {
			scheduledBackupRuns, err := getScheduledBackupBackups(env, namespace, scheduledBackupName)
			if err != nil {
				return 0, err
			}
			return len(scheduledBackupRuns), err
		}, 80).Should(BeEquivalentTo(completedBackupsCount))
	})
	By("resuming suspended backup", func() {
		completedBackupsCount, err = getScheduledBackupCompleteBackupsCount(env, namespace, scheduledBackupName)
		Expect(err).ToNot(HaveOccurred())
		setScheduledBackupSuspend(env, scheduledBackupNamespacedName, false)
		Eventually(func() bool {
			scheduledBackup := &apiv1.ScheduledBackup{}
			err = env.Client.Get(env.Ctx, scheduledBackupNamespacedName, scheduledBackup)
			return *scheduledBackup.Spec.Suspend
		}, 30).Should(BeFalse())
	})
	By("verifying backup has resumed", func() {
		Eventually(func() (int, error) {
			currentBackupCount, err := getScheduledBackupCompleteBackupsCount(env, namespace, scheduledBackupName)
			if err != nil {
				return 0, err
			}
			return currentBackupCount, err
		}, 180).Should(BeNumerically(">", completedBackupsCount))
	})
}

// setScheduledBackupSuspend patches the named ScheduledBackup's
// spec.suspend, retrying on conflict.
func setScheduledBackupSuspend(
	env *environment.TestingEnvironment,
	key types.NamespacedName,
	suspend bool,
) {
	GinkgoHelper()
	Eventually(func() error {
		sb := &apiv1.ScheduledBackup{}
		if err := env.Client.Get(env.Ctx, key, sb); err != nil {
			return err
		}
		original := sb.DeepCopy()
		sb.Spec.Suspend = &suspend
		return env.Client.Patch(env.Ctx, sb, ctrlclient.MergeFrom(original))
	}, 60, 5).Should(Succeed())
}

// AssertArchiveConditionMet waits for the Cluster's ContinuousArchiving
// condition to become True within timeout seconds.
func AssertArchiveConditionMet(
	env *environment.TestingEnvironment,
	namespace, clusterName string,
	timeout int,
) {
	GinkgoHelper()
	By("Waiting for the ContinuousArchiving condition", func() {
		Eventually(func(g Gomega) {
			cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
			g.Expect(err).ToNot(HaveOccurred())
			cond := meta.FindStatusCondition(
				cluster.Status.Conditions,
				string(apiv1.ConditionContinuousArchiving),
			)
			g.Expect(cond).ToNot(BeNil())
			g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
		}, timeout).Should(Succeed())
	})
}

// AssertClusterWasRestoredWithPITR verifies a PITR-restored cluster came up
// on the expected WAL position with both standbys streaming, and that the
// third record from the post-backup write is gone.
func AssertClusterWasRestoredWithPITR(
	env *environment.TestingEnvironment,
	testTimeouts map[timeouts.Timeout]int,
	namespace, clusterName, tableName, lsn string,
) {
	GinkgoHelper()
	By("restoring a backup cluster with PITR in a new cluster", func() {
		clusterasserts.AssertClusterIsReady(env, namespace, clusterName, testTimeouts[timeouts.ClusterIsReadySlow])
		primaryInfo, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())

		row, err := pgutils.RunQueryRowOverForward(
			env.Ctx,
			env.Client,
			env.Interface,
			env.RestClientConfig,
			namespace,
			clusterName,
			pgutils.AppDBName,
			apiv1.ApplicationUserSecretSuffix,
			"select substring(pg_walfile_name(pg_current_wal_lsn()), 1, 8)",
		)
		Expect(err).ToNot(HaveOccurred())

		var currentWalLsn string
		err = row.Scan(&currentWalLsn)
		Expect(err).ToNot(HaveOccurred())
		Expect(currentWalLsn).To(Equal(lsn))

		Expect(pgutils.CountReplicas(
			env.Ctx, env.Client, env.Interface, env.RestClientConfig,
			primaryInfo, environment.RetryTimeout,
		)).To(BeEquivalentTo(2))
	})

	By(fmt.Sprintf("after restored, 3rd entry should not be exists in table '%v'", tableName), func() {
		tableLocator := pgasserts.TableLocator{
			Namespace:    namespace,
			ClusterName:  clusterName,
			DatabaseName: pgutils.AppDBName,
			TableName:    tableName,
		}
		pgasserts.AssertDataExpectedCount(env, tableLocator, 2)
	})
}

// getScheduledBackupBackups returns all Backup children of the named
// ScheduledBackup, identified by the "<scheduledBackupName>-" prefix.
func getScheduledBackupBackups(
	env *environment.TestingEnvironment,
	namespace, scheduledBackupName string,
) ([]apiv1.Backup, error) {
	scheduledBackupNamespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      scheduledBackupName,
	}
	scheduledBackup := &apiv1.ScheduledBackup{}
	err := env.Client.Get(env.Ctx, scheduledBackupNamespacedName,
		scheduledBackup)
	if err != nil {
		return nil, err
	}
	backupList := &apiv1.BackupList{}
	err = env.Client.List(env.Ctx, backupList,
		ctrlclient.InNamespace(namespace))
	if err != nil {
		return nil, err
	}
	var ret []apiv1.Backup

	for _, backup := range backupList.Items {
		if strings.HasPrefix(backup.Name, scheduledBackup.Name+"-") {
			ret = append(ret, backup)
		}
	}
	return ret, nil
}

// getScheduledBackupCompleteBackupsCount returns how many children of the
// named ScheduledBackup have reached BackupPhaseCompleted.
func getScheduledBackupCompleteBackupsCount(
	env *environment.TestingEnvironment,
	namespace, scheduledBackupName string,
) (int, error) {
	scheduledBackupRuns, err := getScheduledBackupBackups(env, namespace, scheduledBackupName)
	if err != nil {
		return -1, err
	}
	completed := 0
	for _, backup := range scheduledBackupRuns {
		if strings.HasPrefix(backup.Name, scheduledBackupName+"-") &&
			backup.Status.Phase == apiv1.BackupPhaseCompleted {
			completed++
		}
	}
	return completed, nil
}

// AssertClusterRestore restores a cluster from restoreClusterFile, waits
// for it to be ready, checks that the test data table is there, the
// timeline is 2, and the standbys stream from the new primary.
func AssertClusterRestore(
	env *environment.TestingEnvironment,
	testTimeouts map[timeouts.Timeout]int,
	namespace, restoreClusterFile, tableName string,
) {
	GinkgoHelper()
	restoredClusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, restoreClusterFile)
	Expect(err).ToNot(HaveOccurred())

	By("Restoring a backup in a new cluster", func() {
		clusterasserts.AssertNoBootstrapJobCreatedDuring(env, namespace, func() {
			resources.CreateResourceFromFile(env, namespace, restoreClusterFile)

			clusterasserts.AssertClusterIsReady(env, namespace, restoredClusterName, testTimeouts[timeouts.ClusterIsReadySlow])
		})
		clusterasserts.AssertClusterInstancesHaveNoRestart(env, namespace, restoredClusterName)

		primary := restoredClusterName + "-1"
		tableLocator := pgasserts.TableLocator{
			Namespace:    namespace,
			ClusterName:  restoredClusterName,
			DatabaseName: pgutils.AppDBName,
			TableName:    tableName,
		}
		pgasserts.AssertDataExpectedCount(env, tableLocator, 2)

		// Restored primary should be on timeline 2
		out, _, err := exec.QueryInInstancePod(
			env.Ctx, env.Client, env.Interface, env.RestClientConfig,
			exec.PodLocator{
				Namespace: namespace,
				PodName:   primary,
			},
			pgutils.AppDBName,
			"select substring(pg_walfile_name(pg_current_wal_lsn()), 1, 8)",
		)
		Expect(strings.Trim(out, "\n"), err).To(Equal("00000002"))

		replication.AssertClusterStandbysAreStreaming(env, namespace, restoredClusterName, 140)
	})
}

// AssertClusterRestoreWithApplicationDB asserts the full
// AssertClusterRestore flow and additionally rotates the application
// user password and verifies the cluster remains reachable.
func AssertClusterRestoreWithApplicationDB(
	env *environment.TestingEnvironment,
	testTimeouts map[timeouts.Timeout]int,
	namespace, restoreClusterFile, tableName string,
) {
	GinkgoHelper()
	restoredClusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, restoreClusterFile)
	Expect(err).ToNot(HaveOccurred())

	By("Restoring a backup in a new cluster", func() {
		resources.CreateResourceFromFile(env, namespace, restoreClusterFile)

		clusterasserts.AssertClusterIsReady(env, namespace, restoredClusterName, testTimeouts[timeouts.ClusterIsReadySlow])

		tableLocator := pgasserts.TableLocator{
			Namespace:    namespace,
			ClusterName:  restoredClusterName,
			DatabaseName: pgutils.AppDBName,
			TableName:    tableName,
		}
		pgasserts.AssertDataExpectedCount(env, tableLocator, 2)
	})

	By("Ensuring the restored cluster is on timeline 2", func() {
		row, err := pgutils.RunQueryRowOverForward(
			env.Ctx,
			env.Client,
			env.Interface,
			env.RestClientConfig,
			namespace,
			restoredClusterName,
			pgutils.AppDBName,
			apiv1.ApplicationUserSecretSuffix,
			"SELECT substring(pg_walfile_name(pg_current_wal_lsn()), 1, 8)",
		)
		Expect(err).ToNot(HaveOccurred())

		var timeline string
		err = row.Scan(&timeline)
		Expect(err).ToNot(HaveOccurred())
		Expect(timeline).To(BeEquivalentTo("00000002"))
	})

	replication.AssertClusterStandbysAreStreaming(env, namespace, restoredClusterName, 140)

	appUser, appUserPass, err := secrets.GetCredentials(
		env.Ctx, env.Client,
		restoredClusterName, namespace,
		apiv1.ApplicationUserSecretSuffix,
	)
	Expect(err).ToNot(HaveOccurred())
	secretName := restoredClusterName + apiv1.ApplicationUserSecretSuffix

	By("checking the restored cluster with pre-defined app password connectable", func() {
		pgasserts.AssertApplicationDatabaseConnection(env,
			namespace,
			restoredClusterName,
			appUser,
			pgutils.AppDBName,
			appUserPass,
			secretName)
	})

	By("update user application password for restored cluster and verify connectivity", func() {
		const newPassword = "eeh2Zahohx"
		secretsasserts.AssertUpdateSecret(env, namespace, restoredClusterName, secretName, "password", newPassword, 30)

		pgasserts.AssertApplicationDatabaseConnection(env,
			namespace,
			restoredClusterName,
			appUser,
			pgutils.AppDBName,
			newPassword,
			secretName)
	})
}

// AssertClusterWasRestoredWithPITRAndApplicationDB is the PITR variant of
// AssertClusterRestoreWithApplicationDB: verifies timeline 3, table
// contents, and that the application database password can be rotated.
func AssertClusterWasRestoredWithPITRAndApplicationDB(
	env *environment.TestingEnvironment,
	testTimeouts map[timeouts.Timeout]int,
	namespace, clusterName, tableName, lsn string,
) {
	GinkgoHelper()
	clusterasserts.AssertClusterIsReady(env, namespace, clusterName, testTimeouts[timeouts.ClusterIsReadySlow])

	primaryInfo, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
	Expect(err).ToNot(HaveOccurred())
	secretName := clusterName + apiv1.ApplicationUserSecretSuffix

	By("Ensuring the restored cluster is on timeline 3", func() {
		row, err := pgutils.RunQueryRowOverForward(
			env.Ctx,
			env.Client,
			env.Interface,
			env.RestClientConfig,
			namespace,
			clusterName,
			pgutils.AppDBName,
			apiv1.ApplicationUserSecretSuffix,
			"select substring(pg_walfile_name(pg_current_wal_lsn()), 1, 8)",
		)
		Expect(err).ToNot(HaveOccurred())

		var currentWalLsn string
		err = row.Scan(&currentWalLsn)
		Expect(err).ToNot(HaveOccurred())
		Expect(currentWalLsn).To(Equal(lsn))

		Expect(pgutils.CountReplicas(
			env.Ctx, env.Client, env.Interface, env.RestClientConfig,
			primaryInfo, environment.RetryTimeout,
		)).To(BeEquivalentTo(2))
	})

	By(fmt.Sprintf("after restored, 3rd entry should not be exists in table '%v'", tableName), func() {
		tableLocator := pgasserts.TableLocator{
			Namespace:    namespace,
			ClusterName:  clusterName,
			DatabaseName: pgutils.AppDBName,
			TableName:    tableName,
		}
		pgasserts.AssertDataExpectedCount(env, tableLocator, 2)
	})

	appUser, appUserPass, err := secrets.GetCredentials(
		env.Ctx, env.Client,
		clusterName, namespace, apiv1.ApplicationUserSecretSuffix,
	)
	Expect(err).ToNot(HaveOccurred())

	By("checking the restored cluster with auto generated app password connectable", func() {
		pgasserts.AssertApplicationDatabaseConnection(env,
			namespace,
			clusterName,
			appUser,
			pgutils.AppDBName,
			appUserPass,
			secretName)
	})

	By("update user application password for restored cluster and verify connectivity", func() {
		const newPassword = "eeh2Zahohx"
		secretsasserts.AssertUpdateSecret(env, namespace, clusterName, secretName, "password", newPassword, 30)
		pgasserts.AssertApplicationDatabaseConnection(env,
			namespace,
			clusterName,
			appUser,
			pgutils.AppDBName,
			newPassword,
			secretName)
	})
}

// AssertClusterImport imports a database into a new cluster, waits for
// it to be ready, and verifies the standbys are streaming.
func AssertClusterImport(
	env *environment.TestingEnvironment,
	testTimeouts map[timeouts.Timeout]int,
	namespace, clusterWithExternalClusterName, clusterName, databaseName string,
) *apiv1.Cluster {
	GinkgoHelper()
	var cluster *apiv1.Cluster
	By("Importing Database in a new cluster", func() {
		var err error
		cluster, err = importdb.ImportDatabaseMicroservice(env.Ctx, env.Client, namespace, clusterName,
			clusterWithExternalClusterName, "", databaseName)
		Expect(err).ToNot(HaveOccurred())
		clusterasserts.AssertClusterIsReady(env, namespace, clusterWithExternalClusterName,
			testTimeouts[timeouts.ClusterIsReadySlow])

		replication.AssertClusterStandbysAreStreaming(env, namespace, clusterWithExternalClusterName, 140)
	})
	return cluster
}
