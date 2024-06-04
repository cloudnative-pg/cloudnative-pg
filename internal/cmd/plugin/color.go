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
	"fmt"
	"os"

	"github.com/logrusorgru/aurora/v4"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// ConfigureColor renews aurora.DefaultColorizer based on flags and TTY
func ConfigureColor(cmd *cobra.Command) error {
	return configureColor(cmd, term.IsTerminal(int(os.Stdout.Fd())))
}

func configureColor(cmd *cobra.Command, isTTY bool) error {
	colorFlag, err := cmd.Flags().GetString("color")
	if err != nil {
		return err
	}

	var shouldColorize bool
	switch colorFlag {
	case "always":
		shouldColorize = true
	case "never":
		shouldColorize = false
	case "auto":
		shouldColorize = isTTY
	default:
		return fmt.Errorf("invalid value for --color: %s, must be one of 'always', 'auto', or 'never'", colorFlag)
	}

	aurora.DefaultColorizer = aurora.New(
		aurora.WithColors(shouldColorize),
		aurora.WithHyperlinks(true),
	)
	return nil
}

// AddColorControlFlag adds color control flags to the command
func AddColorControlFlag(cmd *cobra.Command) {
	cmd.Flags().String("color", "auto", "Control color output; options include 'always', 'auto', or 'never'")
	_ = cmd.RegisterFlagCompletionFunc("color",
		func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
			return []string{"always", "auto", "never"},
				cobra.ShellCompDirectiveDefault | cobra.ShellCompDirectiveKeepOrder
		})
}
