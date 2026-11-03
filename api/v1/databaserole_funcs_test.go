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

package v1

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("DatabaseRole password", func() {
	newRole := func() *DatabaseRole {
		return &DatabaseRole{
			ObjectMeta: metav1.ObjectMeta{Name: "role-dante", Namespace: "default"},
			Spec: DatabaseRoleSpec{
				RoleConfiguration: RoleConfiguration{Name: "dante"},
			},
		}
	}

	It("does not generate for a mode it does not recognize", func() {
		// A mode a future release adds is unrecognized by code from before that
		// release during a rolling upgrade: generation must stay off rather than
		// fall into it by not being listed here.
		role := newRole()
		role.Spec.Password = &PasswordConfiguration{Mode: PasswordMode("notYetInvented")}
		Expect(role.IsPasswordGenerationEnabled()).To(BeFalse())
	})

	It("has a revocation pending only when the specification and the status agree", func() {
		role := newRole()
		role.Status.Password = &GeneratedPasswordState{PendingRevocation: true}
		Expect(role.IsPasswordRevocationPending()).To(BeFalse())

		role.Spec.Password = &PasswordConfiguration{Mode: PasswordModeExternal}
		Expect(role.IsPasswordRevocationPending()).To(BeTrue())

		// The role moved on to another mode: it has a password to apply, not one
		// to revoke, even though the flag from the old revocation is still set.
		role.Spec.Password.Mode = PasswordModeGenerate
		Expect(role.IsPasswordRevocationPending()).To(BeFalse())

		role.Spec.Password.Mode = PasswordModeExternal
		role.Status.Password.PendingRevocation = false
		Expect(role.IsPasswordRevocationPending()).To(BeFalse())
	})

	It("caps the default renewal threshold at half the password lifetime", func() {
		// The operator-wide default (7 days) outlives a short-lived password:
		// left uncapped, the password would be due for rotation the moment it is
		// generated.
		role := newRole()
		role.Spec.Password = &PasswordConfiguration{
			Mode:     PasswordModeGenerate,
			Duration: &metav1.Duration{Duration: 4 * 24 * time.Hour},
		}
		Expect(role.GetPasswordRenewBefore()).To(Equal(2 * 24 * time.Hour))
	})
})
