/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

// Package versions builds the version subcommand for both manager and plugins
package versions

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/versions"
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
