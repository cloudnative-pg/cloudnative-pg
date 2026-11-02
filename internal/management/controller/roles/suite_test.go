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
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

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

var scramMatcherWhitespace = regexp.MustCompile(`\s+`)

// scramAwareMatcher does an exact string match by default. When the
// expected SQL contains the literal "${SCRAM}" marker, that marker is
// replaced with a SCRAM-SHA-256 verifier regex (variable salt) and the
// actual SQL is matched against the resulting pattern. Used to assert
// on ALTER ROLE statements whose PASSWORD literal is a freshly generated
// SCRAM hash on every reconcile.
var scramAwareMatcher = sqlmock.QueryMatcherFunc(func(expectedSQL, actualSQL string) error {
	const marker = "${SCRAM}"
	if !strings.Contains(expectedSQL, marker) {
		return sqlmock.QueryMatcherEqual.Match(expectedSQL, actualSQL)
	}
	// Collapse whitespace before matching, to keep parity with how
	// QueryMatcherEqual normalises the inputs.
	expected := strings.TrimSpace(scramMatcherWhitespace.ReplaceAllString(expectedSQL, " "))
	actual := strings.TrimSpace(scramMatcherWhitespace.ReplaceAllString(actualSQL, " "))
	pattern := regexp.QuoteMeta(expected)
	pattern = strings.ReplaceAll(pattern, regexp.QuoteMeta(marker),
		`SCRAM-SHA-256\$\d+:[^$]+\$[^:]+:[^']+`)
	matched, err := regexp.MatchString("^"+pattern+"$", actual)
	if err != nil {
		return err
	}
	if !matched {
		return fmt.Errorf("query %q does not match SCRAM-aware expected %q", actualSQL, expectedSQL)
	}
	return nil
})

func TestReconciler(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Internal Management Controller Roles Reconciler Suite")
}
