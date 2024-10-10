/*
Copyright The CloudNativePG Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
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

// logRecord is the portion of the structure of a CNPG logging
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

// prints dumps the formatted record to the specified writer
func (record *logRecord) print(writer io.Writer) error {
	message := record.Msg
	level := string(record.Level)

	if record.Msg == "record" {
		level = record.Record.ErrorSeverity
		message = record.Record.Message
	}

	additionalFields := ""
	if len(record.AdditionalFields) > 0 {
		v, _ := json.Marshal(record.AdditionalFields)
		additionalFields = string(v)
	}

	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(record.LoggingPod))
	colorIdx := int(hasher.Sum32()) % len(colorizers)

	_, err := fmt.Fprintln(
		writer,
		record.TS,
		aurora.Blue(strings.ToUpper(level)),
		colorizers[colorIdx](record.LoggingPod),
		aurora.Blue(record.Logger),
		message,
		additionalFields,
	)
	return err
}
