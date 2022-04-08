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

package main

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/manager"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/manager/backup"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/manager/bootstrap"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/manager/controller"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/manager/instance"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/manager/pgbouncer"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/manager/show"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/manager/walarchive"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/manager/walrestore"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/versions"

	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

func main() {
	managerFlags := &manager.Flags{}

	cmd := &cobra.Command{
		Use:          "manager [cmd]",
		SilenceUsage: true,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			managerFlags.ConfigureLogging()
		},
	}

	managerFlags.AddFlags(cmd.PersistentFlags())

	cmd.AddCommand(backup.NewCmd())
	cmd.AddCommand(bootstrap.NewCmd())
	cmd.AddCommand(controller.NewCmd())
	cmd.AddCommand(instance.NewCmd())
	cmd.AddCommand(show.NewCmd())
	cmd.AddCommand(walarchive.NewCmd())
	cmd.AddCommand(walrestore.NewCmd())
	cmd.AddCommand(versions.NewCmd())
	cmd.AddCommand(pgbouncer.NewCmd())

	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
