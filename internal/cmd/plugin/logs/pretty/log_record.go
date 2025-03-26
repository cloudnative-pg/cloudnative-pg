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
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"strings"

	"github.com/logrusorgru/aurora/v4"
)

// colorizers is a list of functions that can be used to decorate
// pod names
var colorizers = []func(any) aurora.Value{
	aurora.Red,
	aurora.Green,
	aurora.Magenta,
	aurora.Cyan,
	aurora.Yellow,
}

// logRecord is the portion of the structure of a CNPG log
// that is handled by the beautifier
type logRecord struct {
	Level      LogLevel `json:"level"`
	Msg        string   `json:"msg"`
	Logger     string   `json:"logger"`
	TS         string   `json:"ts"`
	LoggingPod string   `json:"logging_pod"`
	Record     struct {
		ErrorSeverity string `json:"error_severity"`
		Message       string `json:"message"`
	} `json:"record,omitempty"`

	AdditionalFields map[string]any
}

func newLogRecordFromBytes(bytes []byte) (*logRecord, error) {
	var record logRecord

	if err := json.Unmarshal(bytes, &record); err != nil {
		return nil, fmt.Errorf("decoding log record: %w", err)
	}

	extraFields := make(map[string]any)
	if err := json.Unmarshal(bytes, &extraFields); err != nil {
		return nil, fmt.Errorf("decoding extra fields: %w", err)
	}

	delete(extraFields, "level")
	delete(extraFields, "pipe")
	delete(extraFields, "msg")
	delete(extraFields, "logger")
	delete(extraFields, "ts")
	delete(extraFields, "logging_pod")
	delete(extraFields, "record")
	delete(extraFields, "controllerGroup")
	delete(extraFields, "controllerKind")
	delete(extraFields, "Cluster")

	record.AdditionalFields = extraFields
	return &record, nil
}

// normalize converts the error_severity into one of the acceptable
// LogLevel values
func (record *logRecord) normalize() {
	message := record.Msg
	level := string(record.Level)

	if record.Msg == "record" {
		switch record.Record.ErrorSeverity {
		case "DEBUG1", "DEBUG2", "DEBUG3", "DEBUG4", "DEBUG5":
			level = "trace"

		case "INFO", "NOTICE", "LOG":
			level = "info"

		case "WARNING":
			level = "warning"

		case "ERROR", "FATAL", "PANIC":
			level = "error"

		default:
			level = "info"
		}

		message = record.Record.Message
	}

	record.Msg = message
	record.Level = LogLevel(level)
}

// print dumps the formatted record to the specified writer
func (record *logRecord) print(writer io.Writer, verbosity int) error {
	const jsonPrefix = "    "
	const jsonIndent = "  "
	const maxRowLen = 100

	message := record.Msg
	level := string(record.Level)

	if record.Msg == "record" {
		level = record.Record.ErrorSeverity
		message = record.Record.Message
	}

	additionalFields := ""
	if len(record.AdditionalFields) > 0 {
		v, _ := json.MarshalIndent(record.AdditionalFields, jsonPrefix, jsonIndent)
		additionalFields = string(v)
	}

	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(record.LoggingPod))
	colorIdx := int(hasher.Sum32()) % len(colorizers)

	ts := record.TS
	if verbosity == 0 && len(ts) > 23 {
		ts = record.TS[:23]
	}
	if verbosity > 0 {
		ts = fmt.Sprintf("%-30s", ts)
	}

	if verbosity == 0 {
		firstLine, suffix, _ := strings.Cut(message, "\n")
		if len(firstLine) > maxRowLen || len(suffix) > 0 {
			if len(firstLine) > maxRowLen {
				firstLine = firstLine[:maxRowLen]
			}
			firstLine += "..."
		}
		message = firstLine
	}

	_, err := fmt.Fprintln(
		writer,
		ts,
		fmt.Sprintf("%-8s", aurora.Blue(strings.ToUpper(level))),
		colorizers[colorIdx](record.LoggingPod),
		fmt.Sprintf("%-16s", aurora.Blue(record.Logger)),
		message)
	if len(additionalFields) > 0 && verbosity > 1 {
		_, err = fmt.Fprintln(
			writer,
			jsonPrefix+additionalFields,
		)
	}
	return err
}
