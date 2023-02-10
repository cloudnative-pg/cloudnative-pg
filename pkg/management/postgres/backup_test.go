package postgres

import (
	"context"
	"os"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("testing backup command", func() {
	var cluster *apiv1.Cluster
	var backup BackupCommand

	BeforeEach(func() {
		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test-cluster", Namespace: "test"},
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
		backup = BackupCommand{
			Cluster: cluster,
			Backup: &apiv1.Backup{
				TypeMeta: metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "test",
				},
				Spec: apiv1.BackupSpec{
					Cluster: apiv1.LocalObjectReference{
						Name: "test",
					},
				},
			},
			Client: fake.NewClientBuilder().
				WithScheme(scheme.BuildWithAllKnownScheme()).
				WithObjects(cluster).
				Build(),
			Recorder: &record.FakeRecorder{},
			Env:      os.Environ(),
			Log:      log.FromContext(context.Background()),
			Instance: &Instance{},
		}
	})

	It("should fail and update cluster last failed backup", func() {
		backup.run(context.Background())
		Expect(cluster.Status.LastFailedBackup).ToNot(BeEmpty())
	})
})
