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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("LeasePosition", func() {
	It("round trips every field", func() {
		position := LeasePosition{
			LSN:      "0/7000028",
			Timeline: 3,
			State:    PgDataStateShutDown,
			Pod:      "cluster-example-1",
		}

		value, err := position.Marshal()
		Expect(err).ToNot(HaveOccurred())
		Expect(value).To(MatchJSON(`{"lsn":"0/7000028","timeline":3,"state":"shut down","pod":"cluster-example-1"}`))

		parsed, err := ParseLeasePosition(value)
		Expect(err).ToNot(HaveOccurred())
		Expect(*parsed).To(Equal(position))
	})

	It("omits the fields that are unknown", func() {
		value, err := LeasePosition{State: LeasePositionStateUnknown, Pod: "cluster-example-1"}.Marshal()
		Expect(err).ToNot(HaveOccurred())
		Expect(value).To(MatchJSON(`{"state":"unknown","pod":"cluster-example-1"}`))
	})

	DescribeTable("accepts every known state",
		func(state PgDataState) {
			parsed, err := ParseLeasePosition(`{"state":"` + string(state) + `"}`)
			Expect(err).ToNot(HaveOccurred())
			Expect(parsed.State).To(Equal(state))
		},
		Entry("shut down", PgDataStateShutDown),
		Entry("shut down in recovery", PgDataStateShutDownInRecovery),
		Entry("starting up", PgDataStateStartingUp),
		Entry("shutting down", PgDataStateShuttingDown),
		Entry("in crash recovery", PgDataStateInCrashRecovery),
		Entry("in archive recovery", PgDataStateInArchiveRecovery),
		Entry("in production", PgDataStateInProduction),
		Entry("unknown", LeasePositionStateUnknown),
		Entry("promoting", LeasePositionStatePromoting),
	)

	DescribeTable("rejects what it does not understand",
		func(value string) {
			parsed, err := ParseLeasePosition(value)
			Expect(err).To(HaveOccurred())
			Expect(parsed).To(BeNil())
		},
		Entry("an empty value", ""),
		Entry("something that is not JSON", "0/7000028"),
		Entry("a missing state", `{"lsn":"0/7000028"}`),
		Entry("a state of the future", `{"state":"hibernating"}`),
	)
})
