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
	"os"
	"path/filepath"

	"github.com/cloudnative-pg/machinery/pkg/fileutils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/constants"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Restoring a replica from a snapshot taken without the WAL storage", func() {
	It("writes the backup label and creates the WAL directory on the empty WAL volume",
		func(ctx SpecContext) {
			tempDir := GinkgoT().TempDir()
			pgData := filepath.Join(tempDir, "data", "pgdata")
			pgWal := filepath.Join(tempDir, "wal", "pg_wal")

			// The PGDATA snapshot holds the link to the WAL volume, that is empty
			Expect(fileutils.EnsureDirectoryExists(pgData)).To(Succeed())
			Expect(os.Symlink(pgWal, filepath.Join(pgData, pgWalDirectory))).To(Succeed())

			cluster := &apiv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "cluster-example"},
			}
			cli := fake.NewClientBuilder().
				WithScheme(scheme.BuildWithAllKnownScheme()).
				WithObjects(cluster).
				Build()

			info := InitInfo{
				ClusterName:       cluster.Name,
				Namespace:         cluster.Namespace,
				PgData:            pgData,
				PgWal:             pgWal,
				BackupLabelFile:   []byte("BACKUP FROM: primary\n"),
				TablespaceMapFile: []byte("16384 /var/lib/postgresql/tablespaces/tbs\n"),
			}
			Expect(info.RestoreSnapshot(ctx, cli, true)).To(Succeed())

			Expect(fileutils.ReadFile(filepath.Join(pgData, constants.BackupLabelFile))).
				To(BeEquivalentTo(info.BackupLabelFile))
			Expect(fileutils.ReadFile(filepath.Join(pgData, constants.TablespaceMapFile))).
				To(BeEquivalentTo(info.TablespaceMapFile))
			Expect(pgWal).To(BeADirectory())
			Expect(filepath.Join(pgData, "standby.signal")).To(BeARegularFile())
		})
})
