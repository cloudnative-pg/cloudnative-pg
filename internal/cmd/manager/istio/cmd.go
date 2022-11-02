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

// Package istio implement the istio utilities
package istio

import (
	"fmt"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/manager/istio/quit"
	"github.com/spf13/cobra"
)

// NewCmd creates the new cobra command
func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "istio",
		Short: "Utilities to work with istio",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("missing subcommand")
		},
	}

	cmd.AddCommand(quit.NewCmd())

	return cmd
}
