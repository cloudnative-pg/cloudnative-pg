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
	"regexp"
	"strings"
	"time"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/logpipe"
)

var walReplayProgressRegex = regexp.MustCompile(`redo in progress, elapsed time: .* current LSN: [0-9A-F]+/[0-9A-F]+`)

type walReplayProgressDetector struct {
	instance *postgres.Instance
}

func newWALReplayProgressDetector(instance *postgres.Instance) logpipe.RecordWriter {
	return &walReplayProgressDetector{
		instance: instance,
	}
}

func (d *walReplayProgressDetector) Write(record logpipe.NamedRecord) {
	// Once startup completed, this detector is permanently disabled for this
	// process and should avoid doing any more per-log-message work.
	if d.instance.IsWALReplayProgressDetectionStopped() {
		return
	}

	logRecord, ok := record.(*logpipe.LoggingRecord)
	if !ok {
		return
	}

	if strings.Contains(logRecord.Message, "database system is ready to accept") {
		d.instance.StopWALReplayProgressDetection()
		return
	}

	if !walReplayProgressRegex.MatchString(logRecord.Message) {
		return
	}

	d.instance.SetWALReplayProgressLastObservedAt(time.Now())
}
