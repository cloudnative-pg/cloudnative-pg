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
	"fmt"
	"regexp"
	"time"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/logpipe"
)

const (
	walReplayErrorThreshold = 3
	walReplayErrorWindow    = 10 * time.Minute
	walReplayStalledError   = "unrecoverable WAL replay error loop detected"
)

var (
	incorrectPrevLinkRegex = regexp.MustCompile(`record with incorrect prev-link [0-9A-F]+/[0-9A-F]+ at ([0-9A-F]+/[0-9A-F]+)`)
	contrecordRegex        = regexp.MustCompile(`contrecord is requested by ([0-9A-F]+/[0-9A-F]+)`)
)

type walReplayErrorDetector struct {
	instance *postgres.Instance
	next     logpipe.RecordWriter
	now      func() time.Time

	lsn       string
	count     int
	firstSeen time.Time
}

func newWALReplayErrorDetector(instance *postgres.Instance, next logpipe.RecordWriter) logpipe.RecordWriter {
	return &walReplayErrorDetector{
		instance: instance,
		next:     next,
		now:      time.Now,
	}
}

func (d *walReplayErrorDetector) Write(record logpipe.NamedRecord) {
	d.next.Write(record)

	logRecord, ok := record.(*logpipe.LoggingRecord)
	if !ok {
		return
	}

	lsn := walReplayFailureLSN(logRecord.Message)
	if lsn == "" {
		return
	}

	primary, err := d.instance.IsPrimary()
	if err != nil || primary {
		return
	}

	now := d.now()
	if d.lsn != lsn || now.Sub(d.firstSeen) > walReplayErrorWindow {
		d.lsn = lsn
		d.count = 1
		d.firstSeen = now
		return
	}

	d.count++
	if d.count != walReplayErrorThreshold {
		return
	}

	d.instance.SetCanCheckReadiness(false)
	d.instance.SetStatusError(fmt.Sprintf(
		"%s after %d repeated errors at the same LSN",
		walReplayStalledError,
		d.count,
	))
}

func walReplayFailureLSN(message string) string {
	if match := incorrectPrevLinkRegex.FindStringSubmatch(message); len(match) == 2 {
		return match[1]
	}
	if match := contrecordRegex.FindStringSubmatch(message); len(match) == 2 {
		return match[1]
	}
	return ""
}
