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

var _ = Describe("DatabaseRole password secret resolution", func() {
	newRole := func() *DatabaseRole {
		return &DatabaseRole{
			ObjectMeta: metav1.ObjectMeta{Name: "role-dante", Namespace: "default"},
			Spec: DatabaseRoleSpec{
				RoleConfiguration: RoleConfiguration{Name: "dante"},
			},
		}
	}

	It("has no password secret by default", func() {
		role := newRole()
		Expect(role.IsPasswordGenerationEnabled()).To(BeFalse())
		Expect(role.IsPasswordRotationEnabled()).To(BeFalse())
		Expect(role.GetPasswordSecretName()).To(BeEmpty())
	})

	It("uses the supplied secret when the password is not generated", func() {
		role := newRole()
		role.Spec.PasswordSecret = &LocalObjectReference{Name: "byo-secret"}
		Expect(role.GetPasswordSecretName()).To(Equal("byo-secret"))
	})

	It("names the generated secret after the DatabaseRole, not after the PostgreSQL role", func() {
		role := newRole()
		role.Spec.Password = &PasswordConfiguration{}
		Expect(role.IsPasswordGenerationEnabled()).To(BeTrue())
		Expect(role.GetPasswordSecretName()).To(Equal("role-dante-password"))
	})

	It("does not enable generation for a mode outside the known ones", func() {
		role := newRole()
		role.Spec.Password = &PasswordConfiguration{Mode: "bogus"}
		Expect(role.IsPasswordGenerationEnabled()).To(BeFalse())
	})

	It("honors an explicit secret name", func() {
		role := newRole()
		role.Spec.Password = &PasswordConfiguration{Secret: "dante-credentials"}
		Expect(role.GetPasswordSecretName()).To(Equal("dante-credentials"))
	})

	It("has no password secret when generation is turned off", func() {
		role := newRole()
		role.Spec.Password = &PasswordConfiguration{Mode: PasswordModeExternal, Secret: "dante-credentials"}
		Expect(role.IsPasswordGenerationEnabled()).To(BeFalse())
		Expect(role.GetPasswordSecretName()).To(BeEmpty())
		// The name is still needed, to clean up what was generated before.
		Expect(role.GetGeneratedPasswordSecretName()).To(Equal("dante-credentials"))
	})

	It("has no password secret when the password is explicitly cleared", func() {
		role := newRole()
		role.Spec.Password = &PasswordConfiguration{Mode: PasswordModeClear, Secret: "dante-credentials"}
		Expect(role.IsPasswordGenerationEnabled()).To(BeFalse())
		Expect(role.GetPasswordSecretName()).To(BeEmpty())
	})

	It("reads the password from the named Secret when mode is secret, generating nothing", func() {
		role := newRole()
		role.Spec.Password = &PasswordConfiguration{Mode: PasswordModeSecret, Secret: "byo-secret"}
		Expect(role.IsPasswordGenerationEnabled()).To(BeFalse())
		Expect(role.GetPasswordSecretName()).To(Equal("byo-secret"))
	})

	It("is explicitly cleared only when mode is clear", func() {
		role := newRole()
		Expect(role.IsPasswordExplicitlyCleared()).To(BeFalse())

		role.Spec.Password = &PasswordConfiguration{}
		Expect(role.IsPasswordExplicitlyCleared()).To(BeFalse())

		role.Spec.Password.Mode = PasswordModeExternal
		Expect(role.IsPasswordExplicitlyCleared()).To(BeFalse())

		role.Spec.Password.Mode = PasswordModeClear
		Expect(role.IsPasswordExplicitlyCleared()).To(BeTrue())
	})

	It("rotates only when a lifetime is requested", func() {
		role := newRole()
		role.Spec.Password = &PasswordConfiguration{}
		Expect(role.IsPasswordRotationEnabled()).To(BeFalse())

		role.Spec.Password.Duration = &metav1.Duration{Duration: 90 * 24 * time.Hour}
		Expect(role.IsPasswordRotationEnabled()).To(BeTrue())

		role.Spec.Password.Mode = PasswordModeExternal
		Expect(role.IsPasswordRotationEnabled()).To(BeFalse())
	})
})
