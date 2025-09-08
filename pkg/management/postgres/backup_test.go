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

package postgres

import (
	"context"
	"os"
	"strings"

	barmanBackup "github.com/cloudnative-pg/barman-cloud/pkg/backup"
	"github.com/cloudnative-pg/machinery/pkg/log"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/scheme"

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
		backupCommand = BackupCommand{
			Cluster: cluster,
			Backup:  backup,
			Client: fake.NewClientBuilder().
				WithScheme(scheme.BuildWithAllKnownScheme()).
				WithObjects(cluster, backup).
				WithStatusSubresource(cluster, backup).
				Build(),
			Recorder: &record.FakeRecorder{},
			Env:      os.Environ(),
			Log:      log.FromContext(context.Background()),
			Instance: &Instance{},
		}
	})

	It("should fail and update cluster and backup resource", func() {
		backupCommand.run(context.Background())
		Expect(cluster.Status.LastFailedBackup).ToNot(BeEmpty()) //nolint:staticcheck

		clusterCond := meta.FindStatusCondition(cluster.Status.Conditions, string(apiv1.ConditionBackup))
		Expect(clusterCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(clusterCond.Message).ToNot(BeEmpty())
		Expect(clusterCond.Reason).To(Equal(string(apiv1.ConditionReasonLastBackupFailed)))

		Expect(backup.Status.Error).To(Equal(clusterCond.Message))
	})
})

var _ = Describe("generate backup options", func() {
	const namespace = "test"

	var cluster *apiv1.Cluster

	BeforeEach(func() {
		cluster = &apiv1.Cluster{
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
	})

	It("should generate correct options", func() {
		extraOptions := []string{"--min-chunk-size=5MB", "--read-timeout=60", "-vv"}
		cluster.Spec.Backup.BarmanObjectStore.Data.AdditionalCommandArgs = extraOptions

		cmd := barmanBackup.NewBackupCommand(cluster.Spec.Backup.BarmanObjectStore)
		options, err := cmd.GetDataConfiguration([]string{})
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
		cmd := barmanBackup.NewBackupCommand(cluster.Spec.Backup.BarmanObjectStore)
		options, err := cmd.GetDataConfiguration([]string{})
		Expect(err).ToNot(HaveOccurred())

		Expect(strings.Join(options, " ")).
			To(
				Equal(
					"--gzip --encryption aes256 --immediate-checkpoint --jobs 2 " +
						"--min-chunk-size=5MB --read-timeout=60 -vv",
				))
	})
})
