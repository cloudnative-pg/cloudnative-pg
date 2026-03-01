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
	"errors"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"go.uber.org/zap/zapcore"
)

// ErrUnknownLogLevel is returned when an unknown string representation
// of a log level is used
var ErrUnknownLogLevel = errors.New("unknown log level")

// LogLevel represents a log level such as error, warning, info, debug, or trace.
type LogLevel string

// Less returns true when the received event is less than
// the passed one
func (l LogLevel) Less(o LogLevel) bool {
	return l.toInt() < o.toInt()
}

// String is the string representation of this level
func (l LogLevel) String() string {
	return string(l)
}

// Type is the data type to be used for this type
// when used as a flag
func (l LogLevel) Type() string {
	return "string"
}

// Set sets a log level given its string representation
func (l *LogLevel) Set(val string) error {
	switch val {
	case log.ErrorLevelString, log.WarningLevelString, log.InfoLevelString, log.DebugLevelString, log.TraceLevelString:
		*l = LogLevel(val)
		return nil

	default:
		return ErrUnknownLogLevel
	}
}

// toInt returns the corresponding zapcore level
func (l LogLevel) toInt() zapcore.Level {
	switch l {
	case log.ErrorLevelString:
		return log.ErrorLevel

	case log.WarningLevelString:
		return log.WarningLevel

	case log.InfoLevelString:
		return log.InfoLevel

	case log.DebugLevelString:
		return log.DebugLevel

	case log.TraceLevelString:
		return log.TraceLevel

	default:
		return log.ErrorLevel
	}
}
