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

// Package bootstrap implement the "controller bootstrap" command
package bootstrap

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/fileutils"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/versions"
)

// NewCmd create a new cobra command
func NewCmd() *cobra.Command {
	cmd := cobra.Command{
		Use:  "bootstrap [target]",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dest := args[0]

			log.Info("Installing the manager executable",
				"destination", dest,
				"version", versions.Version,
				"build", versions.Info)
			err := fileutils.CopyFile(cmd.Root().Name(), dest)
			if err != nil {
				panic(err)
			}

			log.Info("Setting 0755 permissions")
			err = os.Chmod(dest, 0o755) // #nosec
			if err != nil {
				panic(err)
			}

			log.Info("Bootstrap completed")

			return nil
		},
	}

	return &cmd
}
