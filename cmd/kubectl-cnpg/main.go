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
kubectl-cnp is a plugin to manage your CloudNativePG clusters
*/
package main

import (
	"os"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/cli"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/cli/backup"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/cli/certificate"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/cli/destroy"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/cli/fence"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/cli/fio"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/cli/hibernate"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/cli/install"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/cli/logical/publication"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/cli/logical/subscription"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/cli/logs"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/cli/maintenance"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/cli/pgadmin"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/cli/pgbench"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/cli/promote"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/cli/psql"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/cli/reload"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/cli/report"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/cli/restart"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/cli/snapshot"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/cli/status"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/versions"

	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

func main() {
	logFlags := &log.Flags{}
	configFlags := genericclioptions.NewConfigFlags(true)

	rootCmd := &cobra.Command{
		Use:   "kubectl-cnpg",
		Short: "A plugin to manage your CloudNativePG clusters",
		Annotations: map[string]string{
			cobra.CommandDisplayNameAnnotation: "kubectl cnpg",
		},
		SilenceUsage: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			logFlags.ConfigureLogging()

			// If we're invoking the completion command we shouldn't try to create
			// a Kubernetes client and we just let the Cobra flow to continue
			if cmd.Name() == "completion" || cmd.Name() == "version" ||
				cmd.HasParent() && cmd.Parent().Name() == "completion" {
				return nil
			}

			cli.ConfigureColor(cmd)

			return cli.SetupKubernetesClient(configFlags)
		},
	}

	logFlags.AddFlags(rootCmd.PersistentFlags())
	configFlags.AddFlags(rootCmd.PersistentFlags())

	adminGroup := &cobra.Group{
		ID:    cli.GroupIDAdmin,
		Title: "Operator-level administration",
	}

	troubleshootingGroup := &cobra.Group{
		ID:    cli.GroupIDTroubleshooting,
		Title: "Troubleshooting",
	}

	pgClusterGroup := &cobra.Group{
		ID:    cli.GroupIDCluster,
		Title: "Cluster administration",
	}

	pgDatabaseGroup := &cobra.Group{
		ID:    cli.GroupIDDatabase,
		Title: "Database administration",
	}

	miscGroup := &cobra.Group{
		ID:    cli.GroupIDMiscellaneous,
		Title: "Miscellaneous",
	}

	rootCmd.AddGroup(adminGroup, troubleshootingGroup, pgClusterGroup, pgDatabaseGroup, miscGroup)

	subcommands := []*cobra.Command{
		backup.NewCmd(),
		certificate.NewCmd(),
		destroy.NewCmd(),
		fence.NewCmd(),
		fio.NewCmd(),
		hibernate.NewCmd(),
		install.NewCmd(),
		logs.NewCmd(),
		maintenance.NewCmd(),
		pgadmin.NewCmd(),
		pgbench.NewCmd(),
		promote.NewCmd(),
		psql.NewCmd(),
		publication.NewCmd(),
		reload.NewCmd(),
		report.NewCmd(),
		restart.NewCmd(),
		snapshot.NewCmd(),
		status.NewCmd(),
		subscription.NewCmd(),
		versions.NewCmd(),
	}

	for _, cmd := range subcommands {
		cli.AddColorControlFlag(cmd)
		rootCmd.AddCommand(cmd)
	}

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
