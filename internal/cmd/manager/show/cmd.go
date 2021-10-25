/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package show implement the show command subfeatures
package show

import (
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/manager/show/walarchivequeue"
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
