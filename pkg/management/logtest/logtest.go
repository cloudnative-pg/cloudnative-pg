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

// Package logtest contains the testing utils for the logging subsystem of the instance manager
package logtest

import (
	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/go-logr/logr"
)

// LogLevel is the type representing a set of log levels
type LogLevel string

const (
	// LogLevelError is the error log level
	LogLevelError = LogLevel("ERROR")

	// LogLevelWarning is the error log level
	LogLevelWarning = LogLevel("WARNING")

	// LogLevelDebug is the debug log level
	LogLevelDebug = LogLevel("DEBUG")

	// LogLevelTrace is the error log level
	LogLevelTrace = LogLevel("TRACE")

	// LogLevelInfo is the error log level
	LogLevelInfo = LogLevel("INFO")
)

// LogRecord represents a log message
type LogRecord struct {
	LoggerName string
	Level      LogLevel
	Message    string
	Error      error
	Attributes map[string]any
}

// NewRecord create a new log record
func NewRecord(name string, level LogLevel, msg string, err error, keysAndValues ...any) *LogRecord {
	result := &LogRecord{
		LoggerName: name,
		Level:      level,
		Message:    msg,
		Error:      err,
		Attributes: make(map[string]any),
	}
	result.WithValues(keysAndValues...)
	return result
}

// WithValues reads a set of keys and values, using them as attributes
// of the log record
func (record *LogRecord) WithValues(keysAndValues ...any) {
	if len(keysAndValues)%2 != 0 {
		panic("key and values set is not even")
	}

	for idx := 0; idx < len(keysAndValues); idx += 2 {
		record.Attributes[keysAndValues[idx].(string)] = keysAndValues[idx+1]
	}
}

// SpyLogger is an implementation of the Logger interface that keeps track
// of the passed log entries
type SpyLogger struct {
	// The following attributes are referred to the current context

	Name       string
	Attributes map[string]any

	// The following attributes represent the event sink

	Records   []LogRecord
	EventSink *SpyLogger
}

// NewSpy creates a new logger interface which will collect every log message sent
func NewSpy() *SpyLogger {
	result := &SpyLogger{Name: ""}
	result.EventSink = result
	return result
}

// AddRecord adds a log record inside the spy
func (s *SpyLogger) AddRecord(record *LogRecord) {
	s.EventSink.Records = append(s.EventSink.Records, *record)
}

// GetLogger implements the log.Logger interface
func (s SpyLogger) GetLogger() logr.Logger {
	return logr.Logger{}
}

// Enabled implements the log.Logger interface
func (s *SpyLogger) Enabled() bool {
	return true
}

// Error implements the log.Logger interface
func (s *SpyLogger) Error(err error, msg string, keysAndValues ...any) {
	s.AddRecord(NewRecord(s.Name, LogLevelError, msg, err, keysAndValues...))
}

// Warning implements the log.Logger interface
func (s *SpyLogger) Warning(msg string, keysAndValues ...any) {
	s.AddRecord(NewRecord(s.Name, LogLevelWarning, msg, nil, keysAndValues...))
}

// Info implements the log.Logger interface
func (s *SpyLogger) Info(msg string, keysAndValues ...any) {
	s.AddRecord(NewRecord(s.Name, LogLevelInfo, msg, nil, keysAndValues...))
}

// Debug implements the log.Logger interface
func (s *SpyLogger) Debug(msg string, keysAndValues ...any) {
	s.AddRecord(NewRecord(s.Name, LogLevelDebug, msg, nil, keysAndValues...))
}

// Trace implements the log.Logger interface
func (s *SpyLogger) Trace(msg string, keysAndValues ...any) {
	s.AddRecord(NewRecord(s.Name, LogLevelTrace, msg, nil, keysAndValues...))
}

// WithValues implements the log.Logger interface
func (s *SpyLogger) WithValues(keysAndValues ...any) log.Logger {
	result := &SpyLogger{
		Name:      s.Name,
		EventSink: s,
	}

	result.Attributes = make(map[string]any)
	for key, value := range s.Attributes {
		result.Attributes[key] = value
	}
	for idx := 0; idx < len(keysAndValues); idx += 2 {
		result.Attributes[keysAndValues[idx].(string)] = keysAndValues[idx+1]
	}

	return result
}

// WithName implements the log.Logger interface
func (s SpyLogger) WithName(name string) log.Logger {
	return &SpyLogger{
		Name:      name,
		EventSink: &s,
	}
}

// WithCaller implements the log.Logger interface
func (s SpyLogger) WithCaller() log.Logger {
	return &s
}
