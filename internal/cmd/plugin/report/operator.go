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

package report

import (
	"github.com/spf13/cobra"

	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/plugin"
)

func operatorCmd() *cobra.Command {
	var (
		file, output  string
		stopRedaction bool
	)
	cmd := &cobra.Command{
		Use:   "operator -f <filename.zip>",
		Short: "Report operator deployment, pod, events",
		Long:  "Collects combined information on the operator in a Zip file",
		RunE: func(cmd *cobra.Command, args []string) error {
			return Operator(cmd.Context(), plugin.OutputFormat(output),
				file, stopRedaction)
		},
	}

	cmd.AddCommand()

	cmd.Flags().StringVarP(&file, "file", "f", "report.zip",
		"Output file")
	cmd.Flags().StringVarP(&output, "output", "o", "yaml",
		"Output format. One of yaml|json")
	cmd.Flags().BoolVarP(&stopRedaction, "stopRedaction", "S", false,
		"Don't redact secrets")

	return cmd
}
