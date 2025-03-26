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

package pgpass

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("pgpass lines generation", func() {
	When("the connection string is empty", func() {
		It("correctly generates the content", func() {
			Expect(NewConnectionInfo(map[string]string{}, "password").BuildLine()).To(Equal("*:*:*:*:password"))
		})
	})

	When("the connection string have all parameters", func() {
		It("correctly generates the content", func() {
			Expect(NewConnectionInfo(map[string]string{
				"host":   "pgtest.com",
				"port":   "5432",
				"dbname": "postgres",
				"user":   "postgres",
			}, "password").BuildLine()).To(Equal("pgtest.com:5432:*:postgres:password"))
		})
	})

	When("the connection string contains chars that need escaping", func() {
		It("correctly generates the content", func() {
			Expect(NewConnectionInfo(map[string]string{
				"host":   "pgtest.com:\\",
				"port":   "5432:\\",
				"dbname": "postgres",
				"user":   "postgres:\\",
			}, "pass:wo\\rd").BuildLine()).To(Equal("pgtest.com\\:\\\\:5432\\:\\\\:*:postgres\\:\\\\:pass\\:wo\\\\rd"))
		})
	})
})
