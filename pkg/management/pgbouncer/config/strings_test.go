/*
Copyright 2019-2022 The CloudNativePG Contributors

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
		params := buildPgBouncerParameters(validParams)
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
		params := stringifyPgBouncerParameters(buildPgBouncerParameters(validParams))
		Expect(params).NotTo(MatchRegexp("^pool_mode.*"))
		Expect(params).NotTo(MatchRegexp("^pid_file.*"))
	})
})
