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
	"os"
	"strings"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	logRecordKey = "record"
)

// RecordWriter is the interface
type RecordWriter interface {
	Write(r NamedRecord)
}

// LogRecordWriter implements the `RecordWriter` interface writing to the
// instance manager logger
type LogRecordWriter struct {
	logger logr.Logger
}

// NewLogRecordWriter builds a LogRecordWriter that logs through logger,
// which should be built via buildUnsampledLogger.
func NewLogRecordWriter(logger logr.Logger) *LogRecordWriter {
	return &LogRecordWriter{logger: logger}
}

// buildUnsampledLogger constructs a JSON logger writing to out without any
// sampling. It otherwise mirrors the global instance-manager logger built by
// machinery/pkg/log.Flags.ConfigureLogging for Info-level output: same
// JSON/RFC3339Nano encoding, the same effective --log-level, and the same
// --log-field-level/--log-field-timestamp field-name remapping, so audit
// records only differ from the rest of the pod's log lines in that they're
// never sampler-dropped. LogRecordWriter only ever logs at Info, so the
// custom level-name encoding ConfigureLogging installs for Warning/Debug/
// Trace (see machinery/pkg/log.getLogLevelString) isn't replicated here.
//
// --log-destination isn't honored here: in practice it's only ever set for
// the wal-archive/wal-restore subcommands, never for `instance run` (which
// owns this writer), so stderr is correct today — though nothing enforces
// that stays true.
func buildUnsampledLogger(out zapcore.WriteSyncer) logr.Logger {
	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.EncodeTime = zapcore.RFC3339NanoTimeEncoder
	applyFieldRemap(&encoderConfig, log.GetFieldsRemapFlags())

	core := zapcore.NewCore(zapcore.NewJSONEncoder(encoderConfig), out, globalLoggerLevel())
	l := zapr.NewLogger(zap.New(core))
	if podName := os.Getenv("POD_NAME"); podName != "" {
		l = l.WithValues("logging_pod", podName)
	}
	return l
}

// globalLoggerLevel returns the effective level of the global instance-manager
// logger, so the unsampled audit logger honors the same --log-level flag
// instead of hardcoding one. Falls back to Info if the global logger isn't a
// zap logger, which shouldn't happen once ConfigureLogging has run.
func globalLoggerLevel() zapcore.Level {
	if u, ok := log.GetLogger().GetLogger().GetSink().(zapr.Underlier); ok {
		return u.GetUnderlying().Level()
	}
	return zapcore.InfoLevel
}

// applyFieldRemap sets enc.LevelKey/TimeKey from flags in the
// "--log-field-level=X"/"--log-field-timestamp=X" form GetFieldsRemapFlags
// returns.
func applyFieldRemap(enc *zapcore.EncoderConfig, flags []string) {
	for _, flag := range flags {
		switch {
		case strings.HasPrefix(flag, "--log-field-level="):
			enc.LevelKey = strings.TrimPrefix(flag, "--log-field-level=")
		case strings.HasPrefix(flag, "--log-field-timestamp="):
			enc.TimeKey = strings.TrimPrefix(flag, "--log-field-timestamp=")
		}
	}
}

// Write logs record through the writer's logger.
func (writer *LogRecordWriter) Write(record NamedRecord) {
	writer.logger.WithName(record.GetName()).Info(logRecordKey, logRecordKey, record)
}
