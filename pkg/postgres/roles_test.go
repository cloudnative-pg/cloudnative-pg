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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("bla bla", func() {
	It("recognize a role which is reserved for PostgreSQL", func() {
		Expect(IsRoleReserved("postgres")).To(BeTrue())
		Expect(IsRoleReserved("pg_write_all_data")).To(BeTrue())
		Expect(IsRoleReserved("pg_execute_server_program")).To(BeTrue())
	})

	It("recognize a role which is reserved for the operator", func() {
		Expect(IsRoleReserved("streaming_replica")).To(BeTrue())
		Expect(IsRoleReserved("cnpg_pooler_pgbouncer")).To(BeTrue())
		Expect(IsRoleReserved("cnpg_pooler_odyssey")).To(BeTrue())
	})

	It("other roles should be not reserved", func() {
		Expect(IsRoleReserved("app")).To(BeFalse())
	})
})
