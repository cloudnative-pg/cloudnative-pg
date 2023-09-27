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

package snapshot

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
)

// NewCmd implements the `snapshot` subcommand
func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "snapshot <cluster-name>",
		Short: "deprecated",
		Long:  "Replaced by `kubectl cnpg backup <cluster-name> -m volumeSnapshot`",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("This command was replaced by `kubectl cnpg backup <cluster-name> -m volumeSnapshot`")
			return errors.New("deprecated")
		},
	}

	return cmd
}
