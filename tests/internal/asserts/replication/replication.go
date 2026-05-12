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

// Package replication provides Ginkgo/Gomega assertions for streaming
// replication state: read-only / read-write service behaviour, replica
// promotion and lag, replication slot accounting.
//
// Only the write-target service assertions ship in step 5; the rest of
// the topic (AssertClusterStandbysAreStreaming, AssertReplicaModeCluster,
// AssertFastFailOver, replication-slot accounting, …) follows in step 6.
package replication

import (
	"fmt"
	"strings"

	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/environment"
	pgutils "github.com/cloudnative-pg/cloudnative-pg/tests/utils/postgres"

	. "github.com/onsi/ginkgo/v2" //nolint
	. "github.com/onsi/gomega"    //nolint
)

// AssertWritesToReplicaFails opens a connection to the named service and
// expects it to land on a replica (recovery=true) where DDL is rejected.
func AssertWritesToReplicaFails(
	env *environment.TestingEnvironment,
	namespace, service, appDBName, appDBUser, appDBPass string,
	connectionParams ...map[string]string,
) {
	GinkgoHelper()
	By(fmt.Sprintf("Verifying %v service doesn't allow writes", service), func() {
		Eventually(func(g Gomega) {
			forwardConn, conn, err := pgutils.ForwardPSQLServiceConnection(
				env.Ctx,
				env.Interface,
				env.RestClientConfig,
				namespace,
				service,
				appDBName,
				appDBUser,
				appDBPass,
				connectionParams...,
			)
			defer func() {
				_ = conn.Close()
				forwardConn.Close()
			}()
			g.Expect(err).ToNot(HaveOccurred())

			var rawValue string
			// Expect to be connected to a replica
			row := conn.QueryRow("SELECT pg_catalog.pg_is_in_recovery()")
			err = row.Scan(&rawValue)
			g.Expect(err).ToNot(HaveOccurred())
			isReplica := strings.TrimSpace(rawValue)
			g.Expect(isReplica).To(BeEquivalentTo("true"))

			// Expect to be in a read-only transaction
			_, err = conn.Exec("CREATE TABLE IF NOT EXISTS table1(var1 text)")
			g.Expect(err).To(HaveOccurred())
			g.Expect(err.Error()).Should(ContainSubstring("cannot execute CREATE TABLE in a read-only transaction"))
		}, environment.RetryTimeout).Should(Succeed())
	})
}

// AssertWritesToPrimarySucceeds opens a connection to the named service
// and expects it to land on the primary, where DDL succeeds.
func AssertWritesToPrimarySucceeds(
	env *environment.TestingEnvironment,
	namespace, service, appDBName, appDBUser, appDBPass string,
	connectionParams ...map[string]string,
) {
	GinkgoHelper()
	By(fmt.Sprintf("Verifying %v service correctly manages writes", service), func() {
		Eventually(func(g Gomega) {
			forwardConn, conn, err := pgutils.ForwardPSQLServiceConnection(
				env.Ctx,
				env.Interface,
				env.RestClientConfig,
				namespace,
				service,
				appDBName,
				appDBUser,
				appDBPass,
				connectionParams...,
			)
			defer func() {
				_ = conn.Close()
				forwardConn.Close()
			}()
			g.Expect(err).ToNot(HaveOccurred())

			var rawValue string
			// Expect to be connected to a primary
			row := conn.QueryRow("SELECT pg_catalog.pg_is_in_recovery()")
			err = row.Scan(&rawValue)
			g.Expect(err).ToNot(HaveOccurred())
			isReplica := strings.TrimSpace(rawValue)
			g.Expect(isReplica).To(BeEquivalentTo("false"))

			// Expect to be able to write
			_, err = conn.Exec("CREATE TABLE IF NOT EXISTS table1(var1 text)")
			g.Expect(err).ToNot(HaveOccurred())
		}, environment.RetryTimeout).Should(Succeed())
	})
}
