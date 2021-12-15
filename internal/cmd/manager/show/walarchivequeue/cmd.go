/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package walarchivequeue implement the wal-archive-queue command
package walarchivequeue

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/specs"
)

// NewCmd creates the new cobra command
func NewCmd() *cobra.Command {
	cmd := cobra.Command{
		Use:   "wal-archive-queue",
		Short: "Lists all .ready wal files in " + specs.PgWalArchiveStatusPath,
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			if err := run(); err != nil {
				log.Error(err, "Error while extracting the list of .ready files")
			}

			return nil
		},
	}

	return &cmd
}

func run() error {
	fileNames, err := postgres.GetReadyWALFiles()
	if err != nil {
		return err
	}
	for _, fileName := range fileNames {
		fmt.Println(fileName)
	}
	return nil
}
