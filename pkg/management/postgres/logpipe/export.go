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

package logpipe

import (
	"encoding/json"
	"maps"
	"slices"

	"github.com/cloudnative-pg/machinery/pkg/log"
)

// jsonRecord is a log record that a process spawned by the instance
// manager, such as wal-archive, wrote in JSON
type jsonRecord struct {
	name          string
	level         string
	message       string
	keysAndValues []any
}

// jsonRecordAttributes maps the fields of a JSON record to the attributes
// that the OpenTelemetry export uses for the records of the instance manager
// itself, as defined by the semantic conventions
var jsonRecordAttributes = map[string]string{
	"error":      "exception.message",
	"stacktrace": "exception.stacktrace",
}

// parseJSONRecord decodes a record written with the default keys of the
// JSON encoder, as the spawned processes don't get the field remapping
// flags
func parseJSONRecord(line []byte) (jsonRecord, error) {
	var fields map[string]any
	if err := json.Unmarshal(line, &fields); err != nil {
		return jsonRecord{}, err
	}

	var record jsonRecord
	record.name, _ = fields["logger"].(string)
	record.level, _ = fields["level"].(string)
	record.message, _ = fields["msg"].(string)
	for _, key := range []string{"logger", "level", "msg", "ts"} {
		delete(fields, key)
	}
	for key, attribute := range jsonRecordAttributes {
		if value, ok := fields[key]; ok {
			delete(fields, key)
			fields[attribute] = value
		}
	}
	for _, key := range slices.Sorted(maps.Keys(fields)) {
		record.keysAndValues = append(record.keysAndValues, key, fields[key])
	}

	return record, nil
}

// exportJSONRecord sends a JSON record to the OpenTelemetry export only,
// as printing it already wrote it on the log destination
func exportJSONRecord(line []byte) {
	logger := log.OpenTelemetryLogger()
	record, err := parseJSONRecord(line)
	if err != nil {
		logger.Info(string(line))
		return
	}

	if record.name != "" {
		logger = logger.WithName(record.name)
	}
	switch record.level {
	case log.ErrorLevelString:
		logger.Error(nil, record.message, record.keysAndValues...)
	case log.DebugLevelString:
		logger.V(int(-log.DebugLevel)).Info(record.message, record.keysAndValues...)
	case log.TraceLevelString:
		logger.V(int(-log.TraceLevel)).Info(record.message, record.keysAndValues...)
	default:
		logger.Info(record.message, record.keysAndValues...)
	}
}
