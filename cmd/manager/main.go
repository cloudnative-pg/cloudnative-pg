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

/*
The manager command is the main entrypoint of CloudNativePG operator.
*/
package main

import (
	"os"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/spf13/cobra"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/manager/backup"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/manager/bootstrap"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/manager/controller"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/manager/debug"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/manager/instance"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/manager/pgbouncer"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/manager/show"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/manager/walarchive"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/manager/walrestore"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/versions"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management"

	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

func main() {
	cobra.EnableTraverseRunHooks = true

	err := newRootCmd().Execute()
	management.ShutdownLogExport()
	if err != nil {
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	logFlags := &log.Flags{}

	cmd := &cobra.Command{
		Use:          "manager [cmd]",
		SilenceUsage: true,
		PersistentPreRun: func(cmd *cobra.Command, _ []string) {
			logFlags.ConfigureLogging(loggingOptions(cmd)...)
		},
	}

	logFlags.AddFlags(cmd.PersistentFlags())

	cmd.AddCommand(backup.NewCmd())
	cmd.AddCommand(bootstrap.NewCmd())
	cmd.AddCommand(controller.NewCmd())
	cmd.AddCommand(instance.NewCmd())
	cmd.AddCommand(show.NewCmd())
	cmd.AddCommand(walarchive.NewCmd())
	cmd.AddCommand(walrestore.NewCmd())
	cmd.AddCommand(versions.NewCmd())
	cmd.AddCommand(pgbouncer.NewCmd())
	cmd.AddCommand(debug.NewCmd())

	return cmd
}

// loggingOptions returns the logging configuration for the subcommand being
// executed. The controller keeps the sampling that controller-runtime applies
// to every operator, since a reconciliation storm can log the same message
// well beyond the sampler threshold. Every other subcommand runs inside a
// Cluster's pods, where the process output is the pod's log stream and
// dropping records is never acceptable, most importantly for the instance
// manager forwarding the PostgreSQL log, whose records share a single message
// and would otherwise be collapsed by the sampler under a burst of activity.
//
// The controller, instance and pgbouncer subcommands, which run in the
// operator, Cluster and Pooler pods, also export their records via
// OpenTelemetry. wal-archive and wal-restore don't: the instance manager
// forwards what they write, and exporting from them would add the export to
// the latency of every WAL file.
func loggingOptions(cmd *cobra.Command) []log.ConfigureOption {
	switch topLevelCommand(cmd).Name() {
	case "controller":
		return []log.ConfigureOption{log.WithOpenTelemetry()}
	case "instance", "pgbouncer":
		return []log.ConfigureOption{log.WithDisabledSampling(), log.WithOpenTelemetry()}
	default:
		return []log.ConfigureOption{log.WithDisabledSampling()}
	}
}

// topLevelCommand walks up the command tree until it finds the direct child
// of the root command, i.e. the manager subcommand that was invoked
func topLevelCommand(cmd *cobra.Command) *cobra.Command {
	for cmd.HasParent() && cmd.Parent().HasParent() {
		cmd = cmd.Parent()
	}

	return cmd
}
