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

// Package show implement the show command subfeatures
package show

import (
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/manager/show/walarchivequeue"
	"github.com/spf13/cobra"
)

// NewCmd creates the new cobra command
func NewCmd() *cobra.Command {
	cmd := cobra.Command{
		Use:           "show [cmd]",
		Short:         "Useful data printing subfeature",
		SilenceErrors: true,
	}

	cmd.AddCommand(walarchivequeue.NewCmd())

	return &cmd
}
