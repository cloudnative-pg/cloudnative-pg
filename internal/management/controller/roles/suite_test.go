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

package roles

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	expectedSelStmt = `SELECT rolname, rolsuper, rolinherit, rolcreaterole, rolcreatedb, 
		rolcanlogin, rolreplication, rolconnlimit, rolpassword, rolvaliduntil, rolbypassrls,
		pg_catalog.shobj_description(auth.oid, 'pg_authid') as comment, auth.xmin,
		mem.inroles
	FROM pg_catalog.pg_authid as auth
	LEFT JOIN (
		SELECT pg_catalog.array_agg(pg_catalog.pg_get_userbyid(roleid)) as inroles, member
		FROM pg_catalog.pg_auth_members GROUP BY member
	) mem ON member = oid
	WHERE rolname not like 'pg\_%'`

	expectedMembershipStmt = `SELECT mem.inroles 
	FROM pg_catalog.pg_authid as auth
	LEFT JOIN (
		SELECT pg_catalog.array_agg(pg_catalog.pg_get_userbyid(roleid)) as inroles, member
		FROM pg_catalog.pg_auth_members GROUP BY member
	) mem ON member = oid
	WHERE rolname = $1`

	wantedRoleCommentTpl = "COMMENT ON ROLE \"%s\" IS %s"
)

func TestReconciler(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Internal Management Controller Roles Reconciler Suite")
}
