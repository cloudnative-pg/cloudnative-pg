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
)

// AddFlags binds manager configuration flags to a given flagset
func (l *Flags) AddFlags(flags *pflag.FlagSet) {
	loggingFlagSet := &flag.FlagSet{}
	loggingFlagSet.StringVar(&logLevel, "log-level", "info",
		"the desired log level, one of error, info, debug and trace")
	loggingFlagSet.StringVar(&logDestination, "log-destination", "",
		"where the log stream will be written")
	l.zapOptions.BindFlags(loggingFlagSet)
	flags.AddGoFlagSet(loggingFlagSet)
}

// ConfigureLogging configure the logging honoring the flags
// passed from the user
func (l *Flags) ConfigureLogging() {
	logger := zap.New(zap.UseFlagOptions(&l.zapOptions), customLevel, customDestination)
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
