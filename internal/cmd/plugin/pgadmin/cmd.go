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

package pgadmin

import (
	"context"
	"fmt"
	"os"
	"slices"
	"text/template"

	"github.com/spf13/cobra"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
)

const pgadminExample = `
  # Dry-run command with default values"
  kubectl-cnpg pgadmin <cluster-name> --dry-run

  # Create a pgadmin job with default values.
  kubectl-cnpg pgadmin <cluster-name>

  # Dry-run command with given values and clusterName "cluster-example"
  kubectl-cnpg pgadmin cluster-example -n <namespace> --dry-run
`

var usageExampleTemplate = template.Must(template.New("pgadmin-example").Parse(`
{{ if eq .Mode "server" }}
To access this pgAdmin instance, use the following credentials:

username: {{ .PgadminUsername }}
password: {{ .PgadminPassword }}


To establish a connection to the database server, you'll need the password for
the '{{ .ApplicationDatabaseOwnerName }}' user. Retrieve it with the following
command:

kubectl get secret {{ .ApplicationDatabaseSecretName }} -o 'jsonpath={.data.password}' | base64 -d; echo ""
{{ end }}

Easily reach the new pgAdmin4 instance by forwarding your local 8080 port using:

kubectl rollout status deployment {{ .DeploymentName }}
kubectl port-forward deployment/{{ .DeploymentName }} 8080:80

Then, navigate to http://localhost:8080 in your browser.

To remove this pgAdmin deployment, execute:

kubectl cnpg pgadmin4 {{ .ClusterName }} --dry-run | kubectl delete -f -
`))

// NewCmd initializes the pgadmin command
func NewCmd() *cobra.Command {
	var dryRun bool
	var mode string
	var pgadminImage string

	pgadminCmd := &cobra.Command{
		Use:     "pgadmin4 [name]",
		Short:   "Creates a pgAdmin deployment",
		Args:    cobra.MinimumNArgs(1),
		Long:    `Creates a pgAdmin deployment configured to work with a CNPG Cluster.`,
		GroupID: plugin.GroupIDMiscellaneous,
		Example: pgadminExample,
		RunE: func(_ *cobra.Command, args []string) error {
			ctx := context.Background()
			clusterName := args[0]

			if !slices.Contains([]string{string(ModeDesktop), string(ModeServer)}, mode) {
				return fmt.Errorf("unknown mode: %s", mode)
			}

			// Get the Cluster object
			var cluster apiv1.Cluster
			if err := plugin.Client.Get(
				ctx,
				client.ObjectKey{Namespace: plugin.Namespace, Name: clusterName},
				&cluster); err != nil {
				return fmt.Errorf("could not get cluster: %v", err)
			}

			pgAdminCmd, err := newCommand(&cluster, Mode(mode), dryRun, pgadminImage)
			if err != nil {
				return err
			}

			if err := pgAdminCmd.execute(ctx); err != nil {
				return err
			}

			if !pgAdminCmd.dryRun {
				_ = usageExampleTemplate.Execute(os.Stdout, pgAdminCmd)
			}

			return nil
		},
	}
	pgadminCmd.Flags().BoolVar(
		&dryRun,
		"dry-run",
		false,
		"When true prints the deployment manifest instead of creating it",
	)

	pgadminCmd.Flags().StringVar(
		&mode,
		"mode",
		"server",
		"Chooses between 'server' and 'desktop' (insecure) mode.",
	)

	pgadminCmd.Flags().StringVar(
		&pgadminImage,
		"image",
		"dpage/pgadmin4:latest",
		"Specifes the pgadmin4 image to use, e.g. for internal registries.",
	)

	return pgadminCmd
}
