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

package postgres

import (
	"context"
	"os"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	barmanCapabilities "github.com/cloudnative-pg/cloudnative-pg/pkg/management/barman/capabilities"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/catalog"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("testing backup command", func() {
	const namespace = "test"

	var cluster *apiv1.Cluster
	var backupCommand BackupCommand
	var backup *apiv1.Backup

	BeforeEach(func() {
		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test-cluster", Namespace: namespace},
			Spec: apiv1.ClusterSpec{
				Backup: &apiv1.BackupConfiguration{
					BarmanObjectStore: &apiv1.BarmanObjectStoreConfiguration{
						BarmanCredentials: apiv1.BarmanCredentials{
							AWS: &apiv1.S3Credentials{
								AccessKeyIDReference: &apiv1.SecretKeySelector{
									LocalObjectReference: apiv1.LocalObjectReference{
										Name: "",
									},
									Key: "",
								},
								SecretAccessKeyReference: &apiv1.SecretKeySelector{
									LocalObjectReference: apiv1.LocalObjectReference{
										Name: "",
									},
									Key: "",
								},
								RegionReference: &apiv1.SecretKeySelector{
									LocalObjectReference: apiv1.LocalObjectReference{
										Name: "",
									},
									Key: "",
								},
								SessionToken: &apiv1.SecretKeySelector{
									LocalObjectReference: apiv1.LocalObjectReference{
										Name: "",
									},
									Key: "",
								},
								InheritFromIAMRole: false,
							},
						},
						EndpointURL: "",
						EndpointCA: &apiv1.SecretKeySelector{
							LocalObjectReference: apiv1.LocalObjectReference{
								Name: "",
							},
							Key: "",
						},
						DestinationPath: "",
						ServerName:      "",
						Wal: &apiv1.WalBackupConfiguration{
							Compression: "",
							Encryption:  "",
							MaxParallel: 0,
						},
						Data: &apiv1.DataBackupConfiguration{
							Compression:         "",
							Encryption:          "",
							ImmediateCheckpoint: false,
							Jobs:                nil,
						},
						Tags:        nil,
						HistoryTags: nil,
					},
				},
			},
		}
		backup = &apiv1.Backup{
			TypeMeta: metav1.TypeMeta{},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-backup",
				Namespace: namespace,
			},
			Spec: apiv1.BackupSpec{
				Cluster: apiv1.LocalObjectReference{
					Name: "test-cluster",
				},
			},
		}
		capabilities, err := barmanCapabilities.CurrentCapabilities()
		Expect(err).ShouldNot(HaveOccurred())
		backupCommand = BackupCommand{
			Cluster: cluster,
			Backup:  backup,
			Client: fake.NewClientBuilder().
				WithScheme(scheme.BuildWithAllKnownScheme()).
				WithObjects(cluster, backup).
				Build(),
			Recorder:     &record.FakeRecorder{},
			Env:          os.Environ(),
			Log:          log.FromContext(context.Background()),
			Instance:     &Instance{},
			Capabilities: capabilities,
		}
	})

	It("should fail and update cluster and backup resource", func() {
		backupCommand.run(context.Background())
		Expect(cluster.Status.LastFailedBackup).ToNot(BeEmpty())

		clusterCond := meta.FindStatusCondition(cluster.Status.Conditions, string(apiv1.ConditionBackup))
		Expect(clusterCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(clusterCond.Message).ToNot(BeEmpty())
		Expect(clusterCond.Reason).To(Equal(string(apiv1.ConditionReasonLastBackupFailed)))

		Expect(backup.Status.Error).To(Equal(clusterCond.Message))
	})
})

var _ = Describe("update barman backup metadata", func() {
	const namespace = "test"

	var cluster *apiv1.Cluster
	var barmanBackups *catalog.Catalog

	var (
		now           = metav1.NewTime(time.Now().Local().Truncate(time.Second))
		oneHourAgo    = metav1.NewTime(now.Add(-1 * time.Hour))
		twoHoursAgo   = metav1.NewTime(now.Add(-2 * time.Hour))
		threeHoursAgo = metav1.NewTime(now.Add(-3 * time.Hour))
	)

	BeforeEach(func() {
		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test-cluster", Namespace: namespace},
			Spec: apiv1.ClusterSpec{
				Backup: &apiv1.BackupConfiguration{},
			},
		}

		barmanBackups = &catalog.Catalog{
			List: []catalog.BarmanBackup{
				{
					BackupName: "twoHoursAgo",
					BeginTime:  threeHoursAgo.Time,
					EndTime:    twoHoursAgo.Time,
				},
				{
					BackupName: "youngest",
					BeginTime:  twoHoursAgo.Time,
					EndTime:    oneHourAgo.Time,
				},
			},
		}
	})

	It("should update cluster with no metadata", func() {
		Expect(cluster.Status.FirstRecoverabilityPoint).To(BeEmpty())
		Expect(cluster.Status.FirstRecoverabilityPointByMethod).To(BeEmpty())
		Expect(cluster.Status.LastSuccessfulBackup).To(BeEmpty())
		Expect(cluster.Status.LastSuccessfulBackupByMethod).To(BeEmpty())

		updateClusterStatusWithBackupTimes(cluster, barmanBackups)

		Expect(cluster.Status.FirstRecoverabilityPoint).To(Equal(twoHoursAgo.Format(time.RFC3339)))
		Expect(cluster.Status.FirstRecoverabilityPointByMethod[apiv1.BackupMethodBarmanObjectStore]).
			To(Equal(twoHoursAgo))
		Expect(cluster.Status.FirstRecoverabilityPointByMethod).
			ToNot(HaveKey(apiv1.BackupMethodVolumeSnapshot))
		Expect(cluster.Status.LastSuccessfulBackup).To(Equal(oneHourAgo.Format(time.RFC3339)))
		Expect(cluster.Status.LastSuccessfulBackupByMethod[apiv1.BackupMethodBarmanObjectStore]).
			To(Equal(oneHourAgo))
		Expect(cluster.Status.LastSuccessfulBackupByMethod).
			ToNot(HaveKey(apiv1.BackupMethodVolumeSnapshot))
	})

	It("will update the metadata if they are outdated", func() {
		cluster.Status = apiv1.ClusterStatus{
			FirstRecoverabilityPoint: now.Format(time.RFC3339),
			FirstRecoverabilityPointByMethod: map[apiv1.BackupMethod]metav1.Time{
				apiv1.BackupMethodBarmanObjectStore: now,
			},
			LastSuccessfulBackup: threeHoursAgo.Format(time.RFC3339),
			LastSuccessfulBackupByMethod: map[apiv1.BackupMethod]metav1.Time{
				apiv1.BackupMethodBarmanObjectStore: threeHoursAgo,
			},
		}

		updateClusterStatusWithBackupTimes(cluster, barmanBackups)

		Expect(cluster.Status.FirstRecoverabilityPoint).To(Equal(twoHoursAgo.Format(time.RFC3339)))
		Expect(cluster.Status.FirstRecoverabilityPointByMethod[apiv1.BackupMethodBarmanObjectStore]).
			To(Equal(twoHoursAgo))
		Expect(cluster.Status.FirstRecoverabilityPointByMethod).
			ToNot(HaveKey(apiv1.BackupMethodVolumeSnapshot))
		Expect(cluster.Status.LastSuccessfulBackup).To(Equal(oneHourAgo.Format(time.RFC3339)))
		Expect(cluster.Status.LastSuccessfulBackupByMethod[apiv1.BackupMethodBarmanObjectStore]).
			To(Equal(oneHourAgo))
		Expect(cluster.Status.LastSuccessfulBackupByMethod).
			ToNot(HaveKey(apiv1.BackupMethodVolumeSnapshot))
	})

	It("will keep metadata from other methods if appropriate", func() {
		cluster.Status = apiv1.ClusterStatus{
			FirstRecoverabilityPoint: now.Format(time.RFC3339),
			FirstRecoverabilityPointByMethod: map[apiv1.BackupMethod]metav1.Time{
				apiv1.BackupMethodBarmanObjectStore: now,
				apiv1.BackupMethodVolumeSnapshot:    threeHoursAgo,
			},
			LastSuccessfulBackup: threeHoursAgo.Format(time.RFC3339),
			LastSuccessfulBackupByMethod: map[apiv1.BackupMethod]metav1.Time{
				apiv1.BackupMethodBarmanObjectStore: threeHoursAgo,
				apiv1.BackupMethodVolumeSnapshot:    now,
			},
		}

		updateClusterStatusWithBackupTimes(cluster, barmanBackups)

		Expect(cluster.Status.FirstRecoverabilityPoint).To(Equal(threeHoursAgo.Format(time.RFC3339)))
		Expect(cluster.Status.FirstRecoverabilityPointByMethod[apiv1.BackupMethodBarmanObjectStore]).
			To(Equal(twoHoursAgo))
		Expect(cluster.Status.FirstRecoverabilityPointByMethod[apiv1.BackupMethodVolumeSnapshot]).
			To(Equal(threeHoursAgo))
		Expect(cluster.Status.LastSuccessfulBackup).To(Equal(now.Format(time.RFC3339)))
		Expect(cluster.Status.LastSuccessfulBackupByMethod[apiv1.BackupMethodBarmanObjectStore]).
			To(Equal(oneHourAgo))
		Expect(cluster.Status.LastSuccessfulBackupByMethod[apiv1.BackupMethodVolumeSnapshot]).
			To(Equal(now))
	})
})

var _ = Describe("generate backup options", func() {
	const namespace = "test"
	capabilities := barmanCapabilities.Capabilities{
		Version:                    nil,
		HasAzure:                   true,
		HasS3:                      true,
		HasGoogle:                  true,
		HasRetentionPolicy:         true,
		HasTags:                    true,
		HasCheckWalArchive:         true,
		HasSnappy:                  true,
		HasErrorCodesForWALRestore: true,
		HasErrorCodesForRestore:    true,
		HasAzureManagedIdentity:    true,
	}
	cluster := &apiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cluster", Namespace: namespace},
		Spec: apiv1.ClusterSpec{
			Backup: &apiv1.BackupConfiguration{
				BarmanObjectStore: &apiv1.BarmanObjectStoreConfiguration{
					Data: &apiv1.DataBackupConfiguration{
						Compression:         "gzip",
						Encryption:          "aes256",
						ImmediateCheckpoint: true,
						Jobs:                ptr.To(int32(2)),
					},
				},
			},
		},
	}

	It("should generate correct options", func() {
		extraOptions := []string{"--min-chunk-size=5MB", "--read-timeout=60", "-vv"}
		cluster.Spec.Backup.BarmanObjectStore.Data.AdditionalCommandArgs = extraOptions
		options := []string{}
		options, err := getDataConfiguration(options, cluster.Spec.Backup.BarmanObjectStore, &capabilities)
		Expect(err).ToNot(HaveOccurred())

		Expect(strings.Join(options, " ")).
			To(
				Equal(
					"--gzip --encryption aes256 --immediate-checkpoint --jobs 2 " +
						"--min-chunk-size=5MB --read-timeout=60 -vv",
				))
	})

	It("should not overwrite declared options if conflict", func() {
		extraOptions := []string{
			"--min-chunk-size=5MB",
			"--read-timeout=60",
			"-vv",
			"--immediate-checkpoint=false",
			"--encryption=aes256",
		}
		cluster.Spec.Backup.BarmanObjectStore.Data.AdditionalCommandArgs = extraOptions
		options := []string{}
		options, err := getDataConfiguration(options, cluster.Spec.Backup.BarmanObjectStore, &capabilities)
		Expect(err).ToNot(HaveOccurred())

		Expect(strings.Join(options, " ")).
			To(
				Equal(
					"--gzip --encryption aes256 --immediate-checkpoint --jobs 2 " +
						"--min-chunk-size=5MB --read-timeout=60 -vv",
				))
	})
})
