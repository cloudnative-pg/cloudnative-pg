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
		recordLogr = buildUnsampledLogger()
	})
	return recordLogr
}

// buildUnsampledLogger constructs a JSON logger that writes to stderr without
// any sampling. The format intentionally mirrors the default instance-manager
// logger (RFC3339 timestamps, JSON encoding) so log aggregators see a
// consistent schema across all pod log lines.
func buildUnsampledLogger() logr.Logger {
	cfg := zap.NewProductionConfig()
	cfg.Sampling = nil // disable sampling — audit records must never be dropped
	cfg.EncoderConfig.EncodeTime = zapcore.RFC3339NanoTimeEncoder

	zapLogger, err := cfg.Build()
	if err != nil {
		// Fallback: global logger may still sample, but is better than nothing
		return log.GetLogger().GetLogger()
	}

	l := zapr.NewLogger(zapLogger)
	if podName := os.Getenv("POD_NAME"); podName != "" {
		l = l.WithValues("logging_pod", podName)
	}
	return l
}

// Write writes the PostgreSQL log record to the instance manager logger
func (writer *LogRecordWriter) Write(record NamedRecord) {
	getRecordLogger().WithName(record.GetName()).Info(logRecordKey, logRecordKey, record)
}
