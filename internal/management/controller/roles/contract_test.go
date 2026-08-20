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
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/scheme"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ApplyPassword", func() {
	const (
		namespace  = "default"
		roleName   = "dante"
		secretName = "role-dante-password"
		password   = "0mA3nCe0f6THe1dIvIne"
	)

	// A role whose password the operator generates carries no `passwordSecret`,
	// which on its own means "leave the password alone".
	config := apiv1.RoleConfiguration{Name: roleName, Login: true}

	buildClient := func() *fake.ClientBuilder {
		return fake.NewClientBuilder().WithScheme(scheme.BuildWithAllKnownScheme()).WithObjects(
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: namespace},
				Type:       corev1.SecretTypeBasicAuth,
				Data: map[string][]byte{
					corev1.BasicAuthUsernameKey: []byte(roleName),
					corev1.BasicAuthPasswordKey: []byte(password),
				},
			},
		)
	}

	It("applies a password read from a Secret the role does not refer to", func() {
		dbRole := DatabaseRoleFromConfiguration(config, false)
		Expect(dbRole.ignorePassword).To(BeTrue())

		version, err := dbRole.ApplyPassword(
			context.Background(), buildClient().Build(), &config, secretName, namespace)
		Expect(err).ToNot(HaveOccurred())
		Expect(version).ToNot(BeEmpty())

		// The password reaching PostgreSQL is what matters: an ignored password
		// leaves the CREATE/ALTER ROLE statement without a PASSWORD clause, and
		// the role ends up with a NULL password while the reconciliation reports
		// success.
		var query strings.Builder
		Expect(appendPasswordOption(dbRole, &query)).To(Succeed())
		Expect(query.String()).To(ContainSubstring(" PASSWORD "))
		Expect(query.String()).ToNot(ContainSubstring("PASSWORD NULL"))
	})

	It("leaves the password alone when there is no Secret to read", func() {
		dbRole := DatabaseRoleFromConfiguration(config, false)

		version, err := dbRole.ApplyPassword(
			context.Background(), buildClient().Build(), &config, "", namespace)
		Expect(err).ToNot(HaveOccurred())
		Expect(version).To(BeEmpty())

		var query strings.Builder
		Expect(appendPasswordOption(dbRole, &query)).To(Succeed())
		Expect(query.String()).ToNot(ContainSubstring("PASSWORD"))
	})

	It("sets the password to NULL when disabled, whatever the role was built from", func() {
		// A `password.mode: clear` DatabaseRole reaches this point as a role
		// built from a configuration that does not disable the password —
		// so it starts out ignoring it — and only the configuration handed
		// to ApplyPassword asks for the password to be disabled. Honouring
		// that has to override the earlier "leave it alone", or the role
		// keeps whatever password it already had in PostgreSQL.
		dbRole := DatabaseRoleFromConfiguration(config, false)
		Expect(dbRole.ignorePassword).To(BeTrue())

		disabled := config
		disabled.DisablePassword = true

		version, err := dbRole.ApplyPassword(
			context.Background(), buildClient().Build(), &disabled, "", namespace)
		Expect(err).ToNot(HaveOccurred())
		Expect(version).To(BeEmpty())

		var query strings.Builder
		Expect(appendPasswordOption(dbRole, &query)).To(Succeed())
		Expect(query.String()).To(ContainSubstring("PASSWORD NULL"))
	})
})

var _ = Describe("DatabaseRole implementation test", func() {
	fixedTime := time.Date(2023, 4, 4, 0, 0, 0, 0, time.UTC)
	fixedTime2 := time.Date(2023, 4, 4, 1, 0, 0, 0, time.UTC)
	It("should return true when the objects are equal", func() {
		role := DatabaseRole{
			Name:    "abc",
			Inherit: true,
		}
		config := apiv1.RoleConfiguration{Name: "abc"}
		res := role.isEquivalentTo(config)
		Expect(res).To(BeTrue())
	})

	It("should return true when the objects are equal except inRoles", func() {
		role := DatabaseRole{
			Name:    "abc",
			Inherit: true,
			InRoles: []string{
				"role1", "Userrole2", "Testxxx",
			},
		}
		config := apiv1.RoleConfiguration{
			Name: "abc",
			InRoles: []string{
				"Userrole2", "role1", "TestroleABC",
			},
		}
		res := role.isEquivalentTo(config)
		Expect(res).To(BeTrue())
	})

	It("should return false when the objects aren't equal", func() {
		role := DatabaseRole{Name: "abc", Inherit: true}
		config := apiv1.RoleConfiguration{Name: "def"}
		res := role.isEquivalentTo(config)
		Expect(res).To(BeFalse())
	})

	It("should return true when the inRole are same but not same order", func() {
		role := DatabaseRole{
			Name:    "abc",
			Inherit: true,
			InRoles: []string{
				"role1", "Userrole2", "TestroleABC",
			},
		}
		config := apiv1.RoleConfiguration{
			Name: "abc",
			InRoles: []string{
				"Userrole2", "role1", "TestroleABC",
			},
		}
		res := role.isInSameRolesAs(config)
		Expect(res).To(BeTrue())
	})

	It("should return false when the in roles are not equal", func() {
		role := DatabaseRole{
			Name:    "abc",
			Inherit: true,
			InRoles: []string{
				"role1", "Userrole2", "TestroleABC",
			},
		}
		config := apiv1.RoleConfiguration{
			Name: "abc",
			InRoles: []string{
				"Userrole2", "role1x", "TestroleABC",
			},
		}
		res := role.isInSameRolesAs(config)
		Expect(res).To(BeFalse())
	})

	It("Detects that spec and db role have the same ValidUntil", func() {
		role := DatabaseRole{
			Name:       "abc",
			ValidUntil: pgtype.Timestamp{Valid: true, Time: fixedTime},
		}
		inSpec := apiv1.RoleConfiguration{
			Name:       "abc",
			ValidUntil: &metav1.Time{Time: fixedTime},
			PasswordSecret: &apiv1.LocalObjectReference{
				Name: "test",
			},
		}
		res := role.hasSameValidUntilAs(inSpec)
		Expect(res).To(BeTrue())
	})

	It("Detects both database and spec don't have a VALID UNTIL", func() {
		role := DatabaseRole{
			Name: "abc",
		}
		inSpec := apiv1.RoleConfiguration{
			Name: "abc",
			PasswordSecret: &apiv1.LocalObjectReference{
				Name: "test",
			},
		}
		res := role.hasSameValidUntilAs(inSpec)
		Expect(res).To(BeTrue())
	})

	It("Detects the VALID UNTIL has drifted", func() {
		role := DatabaseRole{
			Name:       "abc",
			ValidUntil: pgtype.Timestamp{Valid: true, Time: fixedTime},
		}
		inSpec := apiv1.RoleConfiguration{
			Name:       "abc",
			ValidUntil: &metav1.Time{Time: fixedTime2},
			PasswordSecret: &apiv1.LocalObjectReference{
				Name: "test",
			},
		}
		res := role.hasSameValidUntilAs(inSpec)
		Expect(res).To(BeFalse())
	})

	It("Detects difference in VALID UNTIL if db has it but spec does not", func() {
		role := DatabaseRole{
			Name:       "abc",
			ValidUntil: pgtype.Timestamp{Valid: true, Time: fixedTime},
		}
		inSpec := apiv1.RoleConfiguration{
			Name: "abc",
			PasswordSecret: &apiv1.LocalObjectReference{
				Name: "test",
			},
		}
		res := role.hasSameValidUntilAs(inSpec)
		Expect(res).To(BeFalse())
	})

	It("Detects difference in VALID UNTIL if spec has it but db does not", func() {
		role := DatabaseRole{
			Name: "abc",
		}
		inSpec := apiv1.RoleConfiguration{
			Name:       "abc",
			ValidUntil: &metav1.Time{Time: fixedTime2},
			PasswordSecret: &apiv1.LocalObjectReference{
				Name: "test",
			},
		}
		res := role.hasSameValidUntilAs(inSpec)
		Expect(res).To(BeFalse())
	})

	It("Detects that spec and db role have never-expiring passwords", func() {
		role := DatabaseRole{
			Name:       "abc",
			ValidUntil: pgtype.Timestamp{Valid: true, Time: time.Time{}, InfinityModifier: pgtype.Infinity},
		}
		inSpec := apiv1.RoleConfiguration{
			Name:       "abc",
			ValidUntil: nil,
			PasswordSecret: &apiv1.LocalObjectReference{
				Name: "test",
			},
		}
		res := role.hasSameValidUntilAs(inSpec)
		Expect(res).To(BeTrue())
	})

	It("translates a removed validUntil to infinity when the role already had one", func() {
		role := roleConfigurationAdapter{
			RoleConfiguration:        apiv1.RoleConfiguration{Name: "foo"},
			validUntilNullIsInfinity: true,
		}.toDatabaseRole()
		Expect(role.ValidUntil.Valid).To(BeTrue())
		Expect(role.ValidUntil.InfinityModifier).To(Equal(pgtype.Infinity))
	})

	It("leaves a removed validUntil unset when the role had none", func() {
		role := roleConfigurationAdapter{
			RoleConfiguration: apiv1.RoleConfiguration{Name: "foo"},
		}.toDatabaseRole()
		Expect(role.ValidUntil.Valid).To(BeFalse())
	})

	It("should return Correct Role to grant/revoke", func() {
		rolesInDB := []string{"role1", "DBRole1", "DBRoleABC"}
		rolesInSpec := []string{"role1", "role2", "roleabc"}
		rolesToRevoke := getRolesToRevoke(rolesInDB, rolesInSpec)
		rolesToGrant := getRolesToGrant(rolesInDB, rolesInSpec)
		Expect(rolesToRevoke).To(BeEquivalentTo([]string{"DBRole1", "DBRoleABC"}))
		Expect(rolesToGrant).To(BeEquivalentTo([]string{"role2", "roleabc"}))
	})
})
