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

package plugin

import (
	"fmt"
	"os"

	"github.com/logrusorgru/aurora/v4"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// colorConfiguration represents how the output should be colorized.
// It is a `pflag.Value`, therefore implements String(), Set(), Type()
type colorConfiguration string

const (
	// colorAlways configures the output to always be colorized
	colorAlways colorConfiguration = "always"
	// colorAuto configures the output to be colorized only when attached to a terminal
	colorAuto colorConfiguration = "auto"
	// colorNever configures the output never to be colorized
	colorNever colorConfiguration = "never"
)

// String returns the string representation
func (e colorConfiguration) String() string {
	return string(e)
}

// Set sets the color configuration
func (e *colorConfiguration) Set(val string) error {
	colorVal := colorConfiguration(val)
	switch colorVal {
	case colorAlways, colorAuto, colorNever:
		*e = colorVal
		return nil
	default:
		return fmt.Errorf("should be one of 'always', 'auto', or 'never'")
	}
}

// Type returns the data type of the flag used for the color configuration
func (e *colorConfiguration) Type() string {
	return "string"
}

// ConfigureColor renews aurora.DefaultColorizer based on flags and TTY
func ConfigureColor(cmd *cobra.Command) {
	configureColor(cmd, term.IsTerminal(int(os.Stdout.Fd()))) //nolint:gosec // file descriptors always fit in int
}

func configureColor(cmd *cobra.Command, isTTY bool) {
	colorFlag := cmd.Flag("color")
	colorConfig := colorAuto // default config
	if colorFlag != nil {
		colorConfig = colorConfiguration(colorFlag.Value.String())
	}

	var shouldColorize bool
	switch colorConfig {
	case colorAlways:
		shouldColorize = true
	case colorNever:
		shouldColorize = false
	case colorAuto:
		shouldColorize = isTTY
	}

	aurora.DefaultColorizer = aurora.New(
		aurora.WithColors(shouldColorize),
		aurora.WithHyperlinks(true),
	)
}

// AddColorControlFlag adds color control flags to the command
func AddColorControlFlag(cmd *cobra.Command) {
	// By default, color is set to 'auto'
	colorValue := colorAuto
	cmd.Flags().Var(&colorValue, "color", "Control color output; options include 'always', 'auto', or 'never'")
	_ = cmd.RegisterFlagCompletionFunc("color",
		func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
			return []string{colorAlways.String(), colorAuto.String(), colorNever.String()},
				cobra.ShellCompDirectiveDefault | cobra.ShellCompDirectiveKeepOrder
		})
}
