/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package walarchivequeue implement the wal-archive-queue command
package walarchivequeue

import (
	"fmt"
	"io/ioutil"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/specs"
)

// NewCmd creates the new cobra command
func NewCmd() *cobra.Command {
	cmd := cobra.Command{
		Use:   "wal-archive-queue",
		Short: "Lists all .ready wal files in " + specs.PgWalArchiveStatusPath,
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			return run()
		},
	}

	return &cmd
}

func run() error {
	files, err := ioutil.ReadDir(specs.PgWalArchiveStatusPath)
	if err != nil {
		return err
	}

	fmt.Printf("Ready wal files in \"%s\":\n", specs.PgWalArchiveStatusPath)
	for _, file := range files {
		fileName := file.Name()
		if filepath.Ext(fileName) != ".ready" {
			continue
		}
		fmt.Println(fileName)
	}
	return nil
}
