/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
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
