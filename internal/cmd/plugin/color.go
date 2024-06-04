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

package plugin

import (
	"os"

	"github.com/logrusorgru/aurora/v4"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

// ConfigureColor renews aurora.DefaultColorizer based on flags and TTY
func ConfigureColor(cmd *cobra.Command) error {
	return configureColor(cmd, isatty.IsTerminal(os.Stdout.Fd()))
}

func configureColor(cmd *cobra.Command, isTTY bool) error {
	colors, err := cmd.Flags().GetBool("colors")
	if err != nil {
		return err
	}
	noColors, err := cmd.Flags().GetBool("no-colors")
	if err != nil {
		return err
	}

	var shouldColorize bool
	switch {
	case colors:
		shouldColorize = true
	case noColors:
		shouldColorize = false
	default:
		shouldColorize = isTTY
	}

	aurora.DefaultColorizer = aurora.New(
		aurora.WithColors(shouldColorize),
		aurora.WithHyperlinks(true),
	)
	return nil
}

// AddColorControlFlags adds color control flags to the command
func AddColorControlFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("colors", false, "Force colorized output even if no terminal is attached")
	cmd.Flags().Bool("no-colors", false, "Disable colorized output")
	cmd.MarkFlagsMutuallyExclusive("colors", "no-colors")
}
