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
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("tablespace name validation", func() {
	errResolved := fmt.Errorf("tablespace names beginning 'pg_' are reserved for Postgres")
	errIdentifierRuleCheck := fmt.Errorf("tablespace names must be valid Postgres identifiers: " +
		"alphanumeric characters, '_', '$', and must start with a letter or an underscore")
	errLengthCheck := fmt.Errorf("the maximum length of an identifier is 63 characters")

	It("validation tablespace name", func() {
		Expect(IsTablespaceNameValid("tablespace1")).To(BeTrue())
		Expect(IsTablespaceNameValid("tbs_123")).To(BeTrue())
		Expect(IsTablespaceNameValid("_tbs_123")).To(BeTrue())
		Expect(IsTablespaceNameValid("tbs_123$")).To(BeTrue())

		result, err := IsTablespaceNameValid("pg_write_all_data")
		Expect(result).To(BeFalse())
		Expect(err).To(BeEquivalentTo(errResolved))

		result, err = IsTablespaceNameValid("tbs+123")
		Expect(result).To(BeFalse())
		Expect(err).To(BeEquivalentTo(errIdentifierRuleCheck))

		result, err = IsTablespaceNameValid("1tbs1")
		Expect(result).To(BeFalse())
		Expect(err).To(BeEquivalentTo(errIdentifierRuleCheck))

		result, err = IsTablespaceNameValid("tbs_123^")
		Expect(result).To(BeFalse())
		Expect(err).To(BeEquivalentTo(errIdentifierRuleCheck))

		result, err = IsTablespaceNameValid("tablespace1tablespace2tablespace3tablespace4tablespace5_12345678")
		Expect(result).To(BeFalse())
		Expect(err).To(BeEquivalentTo(errLengthCheck))
	})
})
