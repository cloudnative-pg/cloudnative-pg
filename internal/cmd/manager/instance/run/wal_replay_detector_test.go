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

package run

import (
	"os"
	"path/filepath"
	"time"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/logpipe"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("newWALReplayErrorDetector", func() {
	var (
		instance *postgres.Instance
		detector *walReplayErrorDetector
		now      time.Time
	)

	BeforeEach(func() {
		pgData := GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(pgData, "standby.signal"), nil, 0o600)).To(Succeed())
		instance = &postgres.Instance{PgData: pgData}
		instance.SetCanCheckReadiness(true)
		now = time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC)
		detector = newWALReplayErrorDetector(instance).(*walReplayErrorDetector)
		detector.now = func() time.Time { return now }
	})

	writeMessage := func(message string) {
		detector.Write(&logpipe.LoggingRecord{Message: message})
	}

	It("should mark a replica unhealthy after three errors at the same LSN", func() {
		writeMessage("record with incorrect prev-link 0/59002E00 at 0/5A000028")
		writeMessage("contrecord is requested by 0/5A000028")
		writeMessage("record with incorrect prev-link 0/59002E00 at 0/5A000028")

		Expect(instance.CanCheckReadiness()).To(BeFalse())
		Expect(instance.StatusError()).To(ContainSubstring(walReplayStalledError))
		Expect(instance.StatusError()).To(ContainSubstring("same LSN"))
	})

	It("should reset the counter when the failure LSN changes", func() {
		writeMessage("record with incorrect prev-link 0/59002E00 at 0/5A000028")
		writeMessage("record with incorrect prev-link 0/59002E00 at 0/5A000030")
		writeMessage("record with incorrect prev-link 0/59002E00 at 0/5A000030")

		Expect(instance.CanCheckReadiness()).To(BeTrue())
		Expect(instance.StatusError()).To(BeEmpty())
	})

	It("should reset the counter when the repeated error is outside the window", func() {
		writeMessage("contrecord is requested by 0/5A000028")
		writeMessage("contrecord is requested by 0/5A000028")
		now = now.Add(walReplayErrorWindow + time.Second)
		writeMessage("contrecord is requested by 0/5A000028")

		Expect(instance.CanCheckReadiness()).To(BeTrue())
		Expect(instance.StatusError()).To(BeEmpty())
	})

	It("should not mark primaries unhealthy", func() {
		instance.PgData = GinkgoT().TempDir()

		writeMessage("record with incorrect prev-link 0/59002E00 at 0/5A000028")
		writeMessage("record with incorrect prev-link 0/59002E00 at 0/5A000028")
		writeMessage("record with incorrect prev-link 0/59002E00 at 0/5A000028")

		Expect(instance.CanCheckReadiness()).To(BeTrue())
		Expect(instance.StatusError()).To(BeEmpty())
	})

	It("should ignore generic PANIC messages", func() {
		writeMessage("PANIC: could not locate a valid checkpoint record")
		writeMessage("PANIC: could not locate a valid checkpoint record")
		writeMessage("PANIC: could not locate a valid checkpoint record")

		Expect(instance.CanCheckReadiness()).To(BeTrue())
		Expect(instance.StatusError()).To(BeEmpty())
	})
})
