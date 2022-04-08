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

// Package versions builds the version subcommand for both manager and plugins
package versions

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/versions"
)

// NewCmd is a cobra command printing build information
func NewCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Prints version, commit sha and date of the build",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("Build: %+v\n", versions.Info)
		},
	}
}
