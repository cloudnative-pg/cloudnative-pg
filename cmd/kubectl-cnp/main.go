/*
Copyright 2019-2022 The CloudNativePG Contributors

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

	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/plugin/maintenance"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/plugin"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/plugin/certificate"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/plugin/fence"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/plugin/promote"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/plugin/reload"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/plugin/report"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/plugin/restart"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/plugin/status"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/versions"
)

func main() {
	configFlags := genericclioptions.NewConfigFlags(true)

	rootCmd := &cobra.Command{
		Use:          "kubectl-cnp",
		Short:        "A plugin to manage your Cloud Native PostgreSQL clusters",
		SilenceUsage: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return plugin.CreateKubernetesClient(configFlags)
		},
	}

	configFlags.AddFlags(rootCmd.PersistentFlags())

	rootCmd.AddCommand(status.NewCmd())
	rootCmd.AddCommand(promote.NewCmd())
	rootCmd.AddCommand(certificate.NewCmd())
	rootCmd.AddCommand(fence.NewCmd())
	rootCmd.AddCommand(restart.NewCmd())
	rootCmd.AddCommand(reload.NewCmd())
	rootCmd.AddCommand(versions.NewCmd())
	rootCmd.AddCommand(maintenance.NewCmd())
	rootCmd.AddCommand(report.NewCmd())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
