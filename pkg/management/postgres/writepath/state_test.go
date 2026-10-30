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

package writepath

import (
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("State", func() {
	const timeout = 30 * time.Second
	start := time.Date(2026, 10, 30, 10, 0, 0, 0, time.UTC)

	It("is healthy by default", func() {
		var s State
		Expect(s.Stalled()).To(BeFalse())
	})

	It("is not stalled when the heartbeat completes within the timeout", func() {
		var s State
		s.BeatStarted(start)
		Expect(s.Check(start.Add(timeout), timeout)).To(BeFalse())
		Expect(s.BeatFinished(nil, 3)).To(BeFalse())
		Expect(s.Stalled()).To(BeFalse())
	})

	It("is stalled when a heartbeat runs longer than the timeout, once", func() {
		var s State
		s.BeatStarted(start)
		Expect(s.Check(start.Add(timeout+time.Second), timeout)).To(BeTrue())
		Expect(s.Stalled()).To(BeTrue())
		Expect(s.Check(start.Add(timeout+2*time.Second), timeout)).To(BeFalse())
	})

	It("does not consider a failed heartbeat as a stall", func() {
		var s State
		s.BeatStarted(start)
		Expect(s.BeatFinished(errors.New("disk full"), 3)).To(BeFalse())
		Expect(s.Stalled()).To(BeFalse())
		Expect(s.Check(start.Add(time.Hour), timeout)).To(BeFalse())
	})

	It("does not clear the stall on errors, and counts only consecutive successes", func() {
		var s State
		s.BeatStarted(start)
		Expect(s.Check(start.Add(time.Minute), timeout)).To(BeTrue())

		Expect(s.BeatFinished(nil, 3)).To(BeFalse())
		s.BeatStarted(start)
		Expect(s.BeatFinished(nil, 3)).To(BeFalse())
		s.BeatStarted(start)
		Expect(s.BeatFinished(errors.New("boom"), 3)).To(BeFalse())
		Expect(s.Stalled()).To(BeTrue())

		for range 2 {
			s.BeatStarted(start)
			Expect(s.BeatFinished(nil, 3)).To(BeFalse())
		}
		s.BeatStarted(start)
		Expect(s.BeatFinished(nil, 3)).To(BeTrue())
		Expect(s.Stalled()).To(BeFalse())
	})

	It("never clears a latched stall", func() {
		var s State
		s.BeatStarted(start)
		Expect(s.Check(start.Add(time.Minute), timeout)).To(BeTrue())
		s.Latch()
		Expect(s.Latched()).To(BeTrue())

		for range 10 {
			s.BeatStarted(start)
			Expect(s.BeatFinished(nil, 1)).To(BeFalse())
		}
		s.Reset()
		Expect(s.Stalled()).To(BeTrue())
	})

	It("ignores the latch when the write path is healthy", func() {
		var s State
		s.Latch()
		Expect(s.Latched()).To(BeFalse())
	})

	It("forgets a non latched stall on reset", func() {
		var s State
		s.BeatStarted(start)
		Expect(s.Check(start.Add(time.Minute), timeout)).To(BeTrue())
		s.Reset()
		Expect(s.Stalled()).To(BeFalse())
	})
})
