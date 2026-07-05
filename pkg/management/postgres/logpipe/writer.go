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
	"sync"

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
type LogRecordWriter struct{}

var (
	recordLogOnce sync.Once
	recordLogr    logr.Logger
)

// getRecordLogger returns a logr.Logger with sampling disabled.
// controller-runtime enables a zap sampler in production mode that silently
// drops repeated log entries sharing the same message. Every PostgreSQL audit
// record emits msg="record", so a burst of audit activity causes subsequent
// records — including security-relevant events — to be dropped. This logger
// bypasses that sampler so every record is forwarded unconditionally.
func getRecordLogger() logr.Logger {
	recordLogOnce.Do(func() {
		recordLogr = buildUnsampledLogger(zapcore.AddSync(os.Stderr))
	})
	return recordLogr
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
// --log-destination is intentionally not honored here: it's only ever passed
// to the short-lived wal-archive/wal-restore subcommands, never to the
// long-running `instance run` process that owns this writer, so stderr is
// always the correct destination in practice.
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

// applyFieldRemap applies the --log-field-level/--log-field-timestamp field-name
// remapping machinery/pkg/log applies to the global logger's encoder (flags in
// the same "--log-field-level=X"/"--log-field-timestamp=X" form GetFieldsRemapFlags
// returns), so the audit logger's JSON schema doesn't diverge from the rest of
// the pod's logs.
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

// Write writes the PostgreSQL log record to the instance manager logger
func (writer *LogRecordWriter) Write(record NamedRecord) {
	getRecordLogger().WithName(record.GetName()).Info(logRecordKey, logRecordKey, record)
}
