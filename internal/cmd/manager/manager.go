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

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	mlog "github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
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

// UpdateCondition will allow update a particular condition in cluster status.
func UpdateCondition(
	ctx context.Context,
	c client.Client,
	cluster *apiv1.Cluster,
	condition *metav1.Condition,
) error {
	if cluster == nil || condition == nil {
		return nil
	}
	existingCluster := cluster.DeepCopy()
	meta.SetStatusCondition(&cluster.Status.Conditions, *condition)

	if !reflect.DeepEqual(existingCluster.Status.Conditions, cluster.Status.Conditions) {
		// To avoid conflict using patch instead of update
		if err := c.Status().Patch(ctx, cluster, client.MergeFrom(existingCluster)); err != nil {
			return err
		}
	}

	return nil
}
