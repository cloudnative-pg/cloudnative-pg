package postgres

import (
	"context"
	"os"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	barmanCapabilities "github.com/cloudnative-pg/cloudnative-pg/pkg/management/barman/capabilities"
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
