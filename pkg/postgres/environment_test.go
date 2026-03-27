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

var _ = Describe("IsReservedEnvironmentVariable", func() {
	DescribeTable("detects reserved variables",
		func(name string) {
			Expect(IsReservedEnvironmentVariable(name)).To(BeTrue())
		},
		Entry("PGDATA", "PGDATA"),
		Entry("PGHOST", "PGHOST"),
		Entry("CNPG_SECRET", "CNPG_SECRET"),
		Entry("POD_NAME", "POD_NAME"),
		Entry("NAMESPACE", "NAMESPACE"),
		Entry("CLUSTER_NAME", "CLUSTER_NAME"),
	)

	DescribeTable("allows non-reserved variables",
		func(name string) {
			Expect(IsReservedEnvironmentVariable(name)).To(BeFalse())
		},
		Entry("LC_ALL", "LC_ALL"),
		Entry("TZ", "TZ"),
		Entry("FOO", "FOO"),
	)
})
