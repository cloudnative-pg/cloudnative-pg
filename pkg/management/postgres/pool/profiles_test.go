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

package pool

import (
	"github.com/jackc/pgx/v5"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Connection profile defaults", func() {
	parseConfig := func() *pgx.ConnConfig {
		cfg, err := pgx.ParseConfig("host=/tmp")
		Expect(err).ToNot(HaveOccurred())
		return cfg
	}

	DescribeTable("pin search_path = pg_catalog in the startup packet",
		func(profile ConnectionProfile) {
			cfg := parseConfig()
			profile.Enrich(cfg)

			// CWE-426: every operator-issued connection must carry a
			// fixed search_path so that no tenant-controlled ALTER
			// DATABASE / ALTER ROLE setting can influence operator
			// queries.
			Expect(cfg.RuntimeParams).To(HaveKeyWithValue("search_path", "pg_catalog"))

			// Verify the pre-existing defaults are still present.
			Expect(cfg.RuntimeParams).To(HaveKeyWithValue("client_encoding", "UTF8"))
			Expect(cfg.RuntimeParams).To(HaveKeyWithValue("datestyle", "ISO"))
		},
		Entry("ConnectionProfilePostgresql", ConnectionProfilePostgresql),
		Entry("ConnectionProfilePostgresqlPhysicalReplication", ConnectionProfilePostgresqlPhysicalReplication),
		Entry("ConnectionProfilePgbouncer", ConnectionProfilePgbouncer),
	)

	It("preserves the synchronous_commit override on the postgresql profile", func() {
		cfg := parseConfig()
		ConnectionProfilePostgresql.Enrich(cfg)

		Expect(cfg.RuntimeParams).To(HaveKeyWithValue("synchronous_commit", "local"))
	})

	It("preserves the replication=1 override on the physical-replication profile", func() {
		cfg := parseConfig()
		ConnectionProfilePostgresqlPhysicalReplication.Enrich(cfg)

		Expect(cfg.RuntimeParams).To(HaveKeyWithValue("replication", "1"))
	})
})
