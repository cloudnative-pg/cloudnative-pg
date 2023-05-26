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

/*
kubectl-cnp is a plugin to manage your CloudNativePG clusters
*/
package main

import (
	"os"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin/backup"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin/certificate"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin/destroy"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin/fence"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin/fio"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin/hibernate"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin/install"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin/maintenance"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin/pgbench"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin/promote"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin/psql"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin/reload"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin/report"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin/restart"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin/snapshot"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin/status"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/versions"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"

	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

func main() {
	logFlags := &log.Flags{}
	configFlags := genericclioptions.NewConfigFlags(true)

	rootCmd := &cobra.Command{
		Use:          "kubectl-cnpg",
		Short:        "A plugin to manage your CloudNativePG clusters",
		SilenceUsage: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			logFlags.ConfigureLogging()

			// If we're invoking the completion command we shouldn't try to create
			// a Kubernetes client and we just let the Cobra flow to continue
			if cmd.Name() == "completion" || cmd.Name() == "version" ||
				cmd.HasParent() && cmd.Parent().Name() == "completion" {
				return nil
			}

			return plugin.SetupKubernetesClient(configFlags)
		},
	}

	logFlags.AddFlags(rootCmd.PersistentFlags())
	configFlags.AddFlags(rootCmd.PersistentFlags())

	rootCmd.AddCommand(certificate.NewCmd())
	rootCmd.AddCommand(destroy.NewCmd())
	rootCmd.AddCommand(fence.NewCmd())
	rootCmd.AddCommand(fio.NewCmd())
	rootCmd.AddCommand(hibernate.NewCmd())
	rootCmd.AddCommand(install.NewCmd())
	rootCmd.AddCommand(maintenance.NewCmd())
	rootCmd.AddCommand(pgbench.NewCmd())
	rootCmd.AddCommand(promote.NewCmd())
	rootCmd.AddCommand(reload.NewCmd())
	rootCmd.AddCommand(report.NewCmd())
	rootCmd.AddCommand(restart.NewCmd())
	rootCmd.AddCommand(status.NewCmd())
	rootCmd.AddCommand(versions.NewCmd())
	rootCmd.AddCommand(backup.NewCmd())
	rootCmd.AddCommand(psql.NewCmd())
	rootCmd.AddCommand(snapshot.NewCmd())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
