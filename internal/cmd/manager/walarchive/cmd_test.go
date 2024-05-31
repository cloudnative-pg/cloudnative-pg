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

package walarchive

import (
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("barmanCloudWalArchiveOptions", func() {
	const namespace = "test"
	cluster := &apiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cluster", Namespace: namespace},
		Spec: apiv1.ClusterSpec{
			Backup: &apiv1.BackupConfiguration{
				BarmanObjectStore: &apiv1.BarmanObjectStoreConfiguration{
					DestinationPath: "s3://bucket-name/",
					Wal: &apiv1.WalBackupConfiguration{
						Compression: "gzip",
						Encryption:  "aes256",
					},
				},
			},
		},
	}

	It("should generate correct arguments", func() {
		extraOptions := []string{"--min-chunk-size=5MB", "--read-timeout=60", "-vv"}
		cluster.Spec.Backup.BarmanObjectStore.Wal.AdditionalCommandArgs = extraOptions
		options, err := barmanCloudWalArchiveOptions(cluster, "test-cluster")
		Expect(err).ToNot(HaveOccurred())
		Expect(strings.Join(options, " ")).
			To(
				Equal(
					"--gzip -e aes256 --min-chunk-size=5MB --read-timeout=60 -vv s3://bucket-name/ test-cluster",
				))
	})

	It("should not overwrite declared options if conflict", func() {
		extraOptions := []string{
			"--min-chunk-size=5MB",
			"--read-timeout=60",
			"-vv",
			"--immediate-checkpoint=false",
			"--gzip",
			"-e",
			"aes256",
		}
		cluster.Spec.Backup.BarmanObjectStore.Wal.AdditionalCommandArgs = extraOptions
		options, err := barmanCloudWalArchiveOptions(cluster, "test-cluster")
		Expect(err).ToNot(HaveOccurred())

		Expect(strings.Join(options, " ")).
			To(
				Equal(
					"--gzip -e aes256 --min-chunk-size=5MB --read-timeout=60 " +
						"-vv --immediate-checkpoint=false s3://bucket-name/ test-cluster",
				))
	})
})
