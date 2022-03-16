/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package manager contains the common behaviors of the manager subcommand
package manager

import (
	"context"
	"flag"
	"fmt"
	"os"
	"reflect"

	"github.com/spf13/pflag"
	"go.uber.org/zap/zapcore"
	"k8s.io/klog/v2"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	mlog "github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
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
	case mlog.ErrorLevelString,
		mlog.WarningLevelString,
		mlog.InfoLevelString,
		mlog.DebugLevelString,
		mlog.TraceLevelString:
		break
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
	case mlog.WarningLevelString:
		return mlog.WarningLevel
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
	case mlog.WarningLevel:
		return mlog.WarningLevelString
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

// UpdateCondition will allow instance manager to update a particular condition
// if the condition was already updated in the correct state then this is a no-op
func UpdateCondition(ctx context.Context, c client.Client,
	cluster *apiv1.Cluster, condition *apiv1.ClusterCondition,
) error {
	if cluster == nil && condition == nil {
		// if cluster or condition is nil nothing to do here.
		return nil
	}

	oriCluster := cluster.DeepCopy()
	var exCondition *apiv1.ClusterCondition
	for i, c := range cluster.Status.Conditions {
		if c.Type == condition.Type {
			exCondition = &cluster.Status.Conditions[i]
			cluster.Status.Conditions[i] = *condition
			break
		}
	}
	// If existing condition is not found add
	if exCondition == nil {
		cluster.Status.Conditions = append(cluster.Status.Conditions, *condition)
	}

	if !reflect.DeepEqual(oriCluster.Status, cluster.Status) {
		// To avoid conflict using patch instead of update
		if err := c.Status().Patch(ctx, cluster, client.MergeFrom(oriCluster)); err != nil {
			return err
		}
	}

	return nil
}
