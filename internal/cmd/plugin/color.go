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

type colorValue string

const (
	// colorAlways is meant to output colorized output always
	colorAlways colorValue = "always"
	// colorAuto is meant to output colorized output only when the output is attached to a terminal
	colorAuto colorValue = "auto"
	// colorNever is meant to output colorized output never
	colorNever colorValue = "never"
)

// String implements pflag.Value interface
func (e colorValue) String() string {
	return string(e)
}

// Set implements pflag.Value interface
func (e *colorValue) Set(val string) error {
	colorVal := colorValue(val)
	if colorVal != colorAlways && colorVal != colorAuto && colorVal != colorNever {
		return fmt.Errorf("should be one of 'always', 'auto', or 'never'")
	}
	*e = colorVal
	return nil
}

// Type implements pflag.Value interface
func (e *colorValue) Type() string {
	return "string"
}

// ConfigureColor renews aurora.DefaultColorizer based on flags and TTY
func ConfigureColor(cmd *cobra.Command) {
	configureColor(cmd, term.IsTerminal(int(os.Stdout.Fd())))
}

func configureColor(cmd *cobra.Command, isTTY bool) {
	colorFlag := cmd.Flag("color")
	// skip if the command does not have the color flag
	if colorFlag == nil {
		return
	}

	var shouldColorize bool
	switch colorValue(colorFlag.Value.String()) {
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
