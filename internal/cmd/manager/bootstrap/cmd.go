/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
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
