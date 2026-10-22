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
	"path/filepath"

	"github.com/cloudnative-pg/machinery/pkg/fileutils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("RefreshReplicaConfiguration", func() {
	const podName = "cluster-example-1"

	It("prunes stale replication settings from postgresql.auto.conf on a primary", func() {
		pgData := GinkgoT().TempDir()

		autoConf := `primary_slot_name = '_cnpg_cluster_example_1'
primary_conninfo = 'host=cluster-example-rw application_name=cluster-example-1'
log_min_duration_statement = '1s'
`
		Expect(fileutils.WriteStringToFile(filepath.Join(pgData, "postgresql.auto.conf"), autoConf)).To(BeTrue())

		instance := NewInstance().WithPodName(podName).WithClusterName("cluster-example")
		instance.PgData = pgData

		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster-example"},
			Status: apiv1.ClusterStatus{
				TargetPrimary: podName,
			},
		}

		_, err := instance.RefreshReplicaConfiguration(context.Background(), cluster, nil)
		Expect(err).ToNot(HaveOccurred())

		content, err := fileutils.ReadFile(filepath.Join(pgData, "postgresql.auto.conf"))
		Expect(err).ToNot(HaveOccurred())
		Expect(string(content)).ToNot(ContainSubstring("primary_slot_name"))
		Expect(string(content)).ToNot(ContainSubstring("primary_conninfo"))
		Expect(string(content)).To(ContainSubstring("log_min_duration_statement"))
	})
})
