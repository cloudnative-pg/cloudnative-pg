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
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
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

var _ = Describe("testing FirstRecoverabilityPoint updating", func() {
	const namespace = "test"

	var cluster *apiv1.Cluster
	var barmanBackups *catalog.Catalog

	var (
		now      = metav1.Now()
		older    = metav1.NewTime(now.Add(-1 * time.Hour))
		oldest   = metav1.NewTime(older.Add(-1 * time.Hour))
		superOld = metav1.NewTime(oldest.Add(-1 * time.Hour))
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
					BackupName: "oldest",
					BeginTime:  superOld.Time,
					EndTime:    oldest.Time,
				},
				{
					BackupName: "youngest",
					BeginTime:  oldest.Time,
					EndTime:    older.Time,
				},
			},
		}
	})

	It("will not update the FRP and the barman method FRP if they matched the oldest backup", func() {
		cluster.Status = apiv1.ClusterStatus{
			FirstRecoverabilityPoint: oldest.Format(time.RFC3339),
			FirstRecoverabilityByMethod: map[apiv1.BackupMethod]metav1.Time{
				apiv1.BackupMethodBarmanObjectStore: oldest,
			},
			LastSuccessfulBackup: older.Format(time.RFC3339),
		}

		updateClusterStatusWithBackupTimes(cluster, barmanBackups)
		Expect(cluster.Status.FirstRecoverabilityPoint).To(Equal(oldest.Format(time.RFC3339)))
		Expect(cluster.Status.FirstRecoverabilityByMethod).ToNot(BeNil())
		Expect(cluster.Status.FirstRecoverabilityByMethod[apiv1.BackupMethodBarmanObjectStore]).
			To(Equal(oldest))
	})

	It("will update the FRP and the barman method FRP if there are older backups", func() {
		cluster.Status = apiv1.ClusterStatus{
			FirstRecoverabilityPoint: now.Format(time.RFC3339),
			FirstRecoverabilityByMethod: map[apiv1.BackupMethod]metav1.Time{
				apiv1.BackupMethodBarmanObjectStore: now,
			},
		}

		updateClusterStatusWithBackupTimes(cluster, barmanBackups)
		Expect(cluster.Status.FirstRecoverabilityPoint).To(Equal(oldest.Format(time.RFC3339)))
		Expect(cluster.Status.FirstRecoverabilityByMethod).ToNot(BeNil())
		Expect(cluster.Status.FirstRecoverabilityByMethod[apiv1.BackupMethodBarmanObjectStore]).
			To(Equal(oldest))
	})

	It("will keep the oldest volume snapshot as FRP if it is older than barman backups", func() {
		cluster.Status = apiv1.ClusterStatus{
			FirstRecoverabilityPoint: now.Format(time.RFC3339),
			FirstRecoverabilityByMethod: map[apiv1.BackupMethod]metav1.Time{
				apiv1.BackupMethodBarmanObjectStore: now,
				apiv1.BackupMethodVolumeSnapshot:    superOld,
			},
		}

		updateClusterStatusWithBackupTimes(cluster, barmanBackups)
		Expect(cluster.Status.FirstRecoverabilityPoint).To(Equal(superOld.Format(time.RFC3339)))
		Expect(cluster.Status.FirstRecoverabilityByMethod).ToNot(BeNil())
		Expect(cluster.Status.FirstRecoverabilityByMethod[apiv1.BackupMethodBarmanObjectStore]).
			To(Equal(oldest))
		Expect(cluster.Status.FirstRecoverabilityByMethod[apiv1.BackupMethodVolumeSnapshot]).
			To(Equal(superOld))
	})
})
