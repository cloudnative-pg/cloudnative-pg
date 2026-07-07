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
	"time"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/logpipe"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("newWALReplayProgressDetector", func() {
	var (
		instance *postgres.Instance
		detector *walReplayProgressDetector
	)

	BeforeEach(func() {
		instance = &postgres.Instance{}
		detector = newWALReplayProgressDetector(instance).(*walReplayProgressDetector)
	})

	writeMessage := func(message string) {
		detector.Write(&logpipe.LoggingRecord{Message: message})
	}

	It("records progress when it sees a redo progress message", func() {
		before := time.Now()
		writeMessage("redo in progress, elapsed time: 1.00 s, current LSN: 0/3000000")
		after := time.Now()

		lastObservedAt := instance.GetWALReplayProgressLastObservedAt()
		Expect(lastObservedAt).To(BeTemporally(">=", before))
		Expect(lastObservedAt).To(BeTemporally("<=", after))
	})

	It("refreshes progress when it sees another redo progress message", func() {
		writeMessage("redo in progress, elapsed time: 1.00 s, current LSN: 0/3000000")
		before := time.Now()
		writeMessage("redo in progress, elapsed time: 11.00 s, current LSN: 0/3000000")
		after := time.Now()

		lastObservedAt := instance.GetWALReplayProgressLastObservedAt()
		Expect(lastObservedAt).To(BeTemporally(">=", before))
		Expect(lastObservedAt).To(BeTemporally("<=", after))
	})

	It("ignores non-progress messages", func() {
		writeMessage("redo starts at 0/3000000")
		writeMessage("database system was interrupted; last known up at 2026-07-06 00:00:00 UTC")

		lastObservedAt := instance.GetWALReplayProgressLastObservedAt()
		Expect(lastObservedAt).To(BeTemporally("==", time.Unix(0, 0)))
	})

	It("stops detecting progress once the startup probe has succeeded", func() {
		instance.StopWALReplayProgressDetection()
		writeMessage("redo in progress, elapsed time: 1.00 s, current LSN: 0/3000000")

		Expect(instance.IsWALReplayProgressDetectionStopped()).To(BeTrue())
	})

	It("stops detecting progress once PostgreSQL accepts connections", func() {
		writeMessage("database system is ready to accept connections")
		writeMessage("redo in progress, elapsed time: 1.00 s, current LSN: 0/3000000")

		Expect(instance.IsWALReplayProgressDetectionStopped()).To(BeTrue())
	})
})
