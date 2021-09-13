/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package manager contains the common behaviors of the manager subcommand
package manager

import (
	"flag"

	"github.com/spf13/pflag"
	"go.uber.org/zap/zapcore"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	mlog "github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
)

// Flags contains the set of values necessary
// for configuring the manager
type Flags struct {
	zapOptions zap.Options
}

var logLevel string

// AddFlags binds manager configuration flags to a given flagset
func (l *Flags) AddFlags(flags *pflag.FlagSet) {
	loggingFlagSet := &flag.FlagSet{}
	loggingFlagSet.StringVar(&logLevel, "log-level", "info",
		"the desired log level, one of error, info, debug and trace")
	l.zapOptions.BindFlags(loggingFlagSet)
	flags.AddGoFlagSet(loggingFlagSet)
}

// ConfigureLogging configure the logging honoring the flags
// passed from the user
func (l *Flags) ConfigureLogging() {
	logger := zap.New(zap.UseFlagOptions(&l.zapOptions), customLevel)
	switch logLevel {
	case mlog.ErrorLevelString, mlog.InfoLevelString, mlog.DebugLevelString, mlog.TraceLevelString:
	default:
		logger.Info("Invalid log level, defaulting", "level", logLevel, "default", mlog.DefaultLevel)
	}
	ctrl.SetLogger(logger)
	klog.SetLogger(logger)
	mlog.SetLogger(logger)
}

func getLogLevel(l string) zapcore.Level {
	switch l {
	case mlog.ErrorLevelString:
		return mlog.ErrorLevel
	case mlog.InfoLevelString:
		return mlog.InfoLevel
	case mlog.DebugLevelString:
		return mlog.DebugLevel
	case mlog.TraceLevelString:
		return mlog.TraceLevel
	default:
		return mlog.DefaultLevel
	}
}

func getLogLevelString(l zapcore.Level) string {
	switch l {
	case mlog.ErrorLevel:
		return mlog.ErrorLevelString
	case mlog.InfoLevel:
		return mlog.InfoLevelString
	case mlog.DebugLevel:
		return mlog.DebugLevelString
	case mlog.TraceLevel:
		return mlog.TraceLevelString
	default:
		return mlog.DefaultLevelString
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
