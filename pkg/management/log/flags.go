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

package log

import (
	"flag"
	"fmt"
	"os"

	"github.com/spf13/pflag"
	"go.uber.org/zap/zapcore"
	"k8s.io/klog/v2"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// Flags contains the set of values necessary
// for configuring the manager
type Flags struct {
	zapOptions zap.Options
}

var (
	logLevel       string
	logDestination string

	logfieldsRemap struct {
		TimeKey  string
		LevelKey string
	}
)

// NewFlags creates a new instance of Flags
func NewFlags(options zap.Options) Flags {
	return Flags{zapOptions: options}
}

// SetLogLevel sets manually the logLevel
func SetLogLevel(level string) {
	logLevel = level
}

// AddFlags binds manager configuration flags to a given flagset
func (l *Flags) AddFlags(flags *pflag.FlagSet) {
	loggingFlagSet := &flag.FlagSet{}
	loggingFlagSet.StringVar(&logLevel, "log-level", "info",
		"the desired log level, one of error, info, debug and trace")
	loggingFlagSet.StringVar(&logDestination, "log-destination", "",
		"where the log stream will be written")
	loggingFlagSet.StringVar(&logfieldsRemap.LevelKey, "log-field-level", "",
		"JSON log field to report severity in (default: level)")
	loggingFlagSet.StringVar(&logfieldsRemap.TimeKey, "log-field-timestamp", "",
		"JSON log field to report timestamp in (default: ts)")
	l.zapOptions.BindFlags(loggingFlagSet)
	flags.AddGoFlagSet(loggingFlagSet)
}

// GetFieldsRemapFlags returns the required flags to set the logging fields
func GetFieldsRemapFlags() (res []string) {
	if l := logfieldsRemap.LevelKey; l != "" {
		res = append(res, fmt.Sprintf("--log-field-level=%s", l))
	}
	if l := logfieldsRemap.TimeKey; l != "" {
		res = append(res, fmt.Sprintf("--log-field-timestamp=%s", l))
	}
	return res
}

// ConfigureLogging configure the logging honoring the flags
// passed from the user
// This is executed after args were already parsed.
func (l *Flags) ConfigureLogging() {
	logger := zap.New(zap.UseFlagOptions(&l.zapOptions), customLevel, customDestination, remapKeys)
	switch logLevel {
	case ErrorLevelString,
		WarningLevelString,
		InfoLevelString,
		DebugLevelString,
		TraceLevelString:
		break
	default:
		logger.Info("Invalid log level, defaulting", "level", logLevel, "default", DefaultLevel)
	}

	redirectStdLog(logger)
	controllerruntime.SetLogger(logger)
	klog.SetLogger(logger)
	SetLogger(logger)
}

func getLogLevel(l string) zapcore.Level {
	switch l {
	case ErrorLevelString:
		return ErrorLevel
	case WarningLevelString:
		return WarningLevel
	case InfoLevelString:
		return InfoLevel
	case DebugLevelString:
		return DebugLevel
	case TraceLevelString:
		return TraceLevel
	default:
		return DefaultLevel
	}
}

func getLogLevelString(l zapcore.Level) string {
	switch l {
	case ErrorLevel:
		return ErrorLevelString
	case WarningLevel:
		return WarningLevelString
	case InfoLevel:
		return InfoLevelString
	case DebugLevel:
		return DebugLevelString
	case TraceLevel:
		return TraceLevelString
	default:
		return DefaultLevelString
	}
}

func remapKeys(in *zap.Options) {
	in.EncoderConfigOptions = append(in.EncoderConfigOptions, func(c *zapcore.EncoderConfig) {
		if logfieldsRemap.TimeKey != "" {
			c.TimeKey = logfieldsRemap.TimeKey
		}
		if logfieldsRemap.LevelKey != "" {
			c.LevelKey = logfieldsRemap.LevelKey
		}
	})
}

func customLevel(in *zap.Options) {
	in.Level = getLogLevel(logLevel)
	in.EncoderConfigOptions = append(in.EncoderConfigOptions, func(c *zapcore.EncoderConfig) {
		c.EncodeLevel = func(l zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
			enc.AppendString(getLogLevelString(l))
		}
	})
}

func customDestination(in *zap.Options) {
	if logDestination == "" {
		return
	}

	logStream, err := os.OpenFile(logDestination, os.O_RDWR|os.O_CREATE, 0o666) //#nosec
	if err != nil {
		panic(fmt.Sprintf("Cannot open log destination %v: %v", logDestination, err))
	}

	in.DestWriter = logStream
}
