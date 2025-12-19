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

package utils

import (
	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ValidateNamespacedConfiguration", func() {
	It("should pass when namespaced is disabled", func() {
		conf := &configuration.Data{
			Namespaced:        false,
			OperatorNamespace: "operator-ns",
			WatchNamespace:    "watch-ns",
		}
		Expect(ValidateNamespacedConfiguration(conf)).To(Succeed())
	})

	It("should pass when namespaced is enabled and namespaces match", func() {
		conf := &configuration.Data{
			Namespaced:        true,
			OperatorNamespace: "same-ns",
			WatchNamespace:    "same-ns",
		}
		Expect(ValidateNamespacedConfiguration(conf)).To(Succeed())
	})

	It("should fail when namespaced is enabled and namespaces differ", func() {
		conf := &configuration.Data{
			Namespaced:        true,
			OperatorNamespace: "operator-ns",
			WatchNamespace:    "watch-ns",
		}
		err := ValidateNamespacedConfiguration(conf)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(
			"operator namespace (operator-ns) and watch namespace (watch-ns) must be equal"))
	})

	It("should fail when namespaced is enabled and operator namespace is empty", func() {
		conf := &configuration.Data{
			Namespaced:        true,
			OperatorNamespace: "",
			WatchNamespace:    "watch-ns",
		}
		err := ValidateNamespacedConfiguration(conf)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("operator namespace cannot be empty"))
	})

	It("should fail when namespaced is enabled and watch namespace is empty", func() {
		conf := &configuration.Data{
			Namespaced:        true,
			OperatorNamespace: "operator-ns",
			WatchNamespace:    "",
		}
		err := ValidateNamespacedConfiguration(conf)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("watch namespace cannot be empty"))
	})

	It("should fail when namespaced is enabled and both namespaces are empty", func() {
		conf := &configuration.Data{
			Namespaced:        true,
			OperatorNamespace: "",
			WatchNamespace:    "",
		}
		err := ValidateNamespacedConfiguration(conf)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("operator namespace cannot be empty"))
	})
})
