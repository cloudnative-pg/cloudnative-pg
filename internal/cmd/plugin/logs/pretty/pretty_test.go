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

package pretty

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("group", func() {
	// collectGroups feeds the given records into group() and returns the
	// emitted groups once the input channel is closed.
	collectGroups := func(bf *prettyCmd, records []logRecord) [][]logRecord {
		GinkgoHelper()

		logChannel := make(chan logRecord)
		groupChannel := make(chan []logRecord)

		go bf.group(context.Background(), logChannel, groupChannel)

		go func() {
			defer GinkgoRecover()
			for _, record := range records {
				logChannel <- record
			}
			close(logChannel)
		}()

		var groups [][]logRecord
		for group := range groupChannel {
			groups = append(groups, group)
		}
		return groups
	}

	makeRecords := func(messages ...string) []logRecord {
		records := make([]logRecord, len(messages))
		for i, msg := range messages {
			records[i] = logRecord{Msg: msg}
		}
		return records
	}

	It("emits a full group as soon as groupSize records are buffered", func() {
		bf := &prettyCmd{groupSize: 2}

		groups := collectGroups(bf, makeRecords("a", "b", "c", "d"))

		Expect(groups).To(HaveLen(2))
		Expect(groups[0]).To(HaveLen(2))
		Expect(groups[0][0].Msg).To(Equal("a"))
		Expect(groups[0][1].Msg).To(Equal("b"))
		Expect(groups[1][0].Msg).To(Equal("c"))
		Expect(groups[1][1].Msg).To(Equal("d"))
	})

	It("flushes the remaining partial buffer when the input is closed", func() {
		bf := &prettyCmd{groupSize: 10}

		groups := collectGroups(bf, makeRecords("a", "b", "c"))

		Expect(groups).To(HaveLen(1))
		Expect(groups[0]).To(HaveLen(3))
		Expect(groups[0][0].Msg).To(Equal("a"))
		Expect(groups[0][2].Msg).To(Equal("c"))
	})

	It("emits nothing and closes the output when no records are received", func() {
		bf := &prettyCmd{groupSize: 4}

		groups := collectGroups(bf, nil)

		Expect(groups).To(BeEmpty())
	})

	It("terminates and closes the output channel when the context is cancelled", func() {
		bf := &prettyCmd{groupSize: 4}

		ctx, cancel := context.WithCancel(context.Background())
		logChannel := make(chan logRecord)
		groupChannel := make(chan []logRecord)

		go bf.group(ctx, logChannel, groupChannel)
		cancel()

		Eventually(groupChannel).Should(BeClosed())
	})
})
