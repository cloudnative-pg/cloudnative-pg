/*
Copyright The CloudNativePG Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package envsubst

import (
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = DescribeTable("Envsubst test",
	func(text string, vars map[string]string, expectedString string, expectedErr error) {
		out, err := Envsubst(vars, []byte(text))
		if expectedErr == nil {
			Expect(err).ShouldNot(HaveOccurred())
			Expect(string(out)).To(Equal(expectedString))
		} else {
			Expect(errors.Is(err, expectedErr)).To(BeTrue())
		}
	},
	Entry("can perform substitution", "substituting ${foo} in bar",
		map[string]string{"foo": "baz"}, "substituting baz in bar", nil),
	Entry("can repeat substitution", "substituting ${foo} in ${foo}",
		map[string]string{"foo": "baz"}, "substituting baz in baz", nil),
	Entry("can do several substitutions", "substituting ${foo} in ${bar}",
		map[string]string{"foo": "baz", "bar": "quux"}, "substituting baz in quux", nil),
	Entry("errors out on missing var", "not substituting ${foobar} in bar",
		map[string]string{"foo": "foo"}, "not substituting ${foobar} in bar", ErrEnvVarNotFound),
	Entry("can do multi-line subst",
		`storage:\n
		storageClass: ${E2E_DEFAULT_STORAGE_CLASS}
		size: 1Gi`,
		map[string]string{"E2E_DEFAULT_STORAGE_CLASS": "standard"},
		`storage:\n
		storageClass: standard
		size: 1Gi`, nil),
)
