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

package config

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PgBouncer configuration", func() {
	It("can build valid configurations", func() {
		validParams := map[string]string{
			"verbose":                  "10",
			ignoreStartupParametersKey: "test",
		}
		params, err := buildPgBouncerParameters(validParams)
		Expect(err).ToNot(HaveOccurred())
		Expect(params[ignoreStartupParametersKey]).To(ContainSubstring("test"))
		Expect(params[ignoreStartupParametersKey]).
			To(ContainSubstring(defaultPgBouncerParameters[ignoreStartupParametersKey]))
		Expect(params["verbose"]).To(BeEquivalentTo("10"))
		Expect(params["logstats"]).To(BeEquivalentTo(defaultPgBouncerParameters["logstats"]))
	})

	It("can escape values", func() {
		validParams := map[string]string{
			"verbose":             "10\npool_mode: test",
			"autodb_idle_timeout": "10\\npid_file: test",
		}
		built, err := buildPgBouncerParameters(validParams)
		Expect(err).ToNot(HaveOccurred())
		params := stringifyPgBouncerParameters(built)
		Expect(params).NotTo(MatchRegexp("^pool_mode.*"))
		Expect(params).NotTo(MatchRegexp("^pid_file.*"))
	})

	It("resolves parameter names that clean to the same name deterministically", func() {
		collidingParams := map[string]string{
			"max_client_conn":   "100",
			"max_client_conn\n": "200",
			"max_client_conn\r": "300",
		}

		expected, err := buildPgBouncerParameters(collidingParams)
		Expect(err).ToNot(HaveOccurred())
		// When names collide, the value of the last key in sorted order wins
		Expect(expected["max_client_conn"]).To(Equal("300"))

		for range 100 {
			params, err := buildPgBouncerParameters(collidingParams)
			Expect(err).ToNot(HaveOccurred())
			Expect(params).To(Equal(expected))
		}
	})

	It("does not tell apart a name from the same name surrounded by whitespace", func() {
		params, err := buildPgBouncerParameters(map[string]string{
			"listen_port ": "6543",
			" admin_users": "someone",
			"verbose\t":    "1",
		})
		Expect(err).ToNot(HaveOccurred())

		// The parameters we force cannot be shadowed ...
		Expect(params["listen_port"]).To(Equal(forcedPgBouncerParameters["listen_port"]))
		Expect(params["admin_users"]).To(Equal(forcedPgBouncerParameters["admin_users"]))
		// ... and the other ones are rendered with their canonical name
		Expect(params["verbose"]).To(Equal("1"))
	})

	DescribeTable("rejects the names that cannot be the key of a line",
		func(name string) {
			_, err := buildPgBouncerParameters(map[string]string{name: "1"})
			Expect(err).To(HaveOccurred())
		},
		Entry("empty", ""),
		Entry("with an equal sign", "listen_port=6543"),
		Entry("with an inner space", "listen_port 6543"),
		Entry("starting a section", "[databases]"),
		Entry("starting a comment", ";listen_port"),
	)
})
