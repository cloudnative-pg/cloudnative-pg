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
	"strings"

	"github.com/spf13/cobra"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
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
	files, err := ioutil.ReadDir(specs.PgWalArchiveStatusPath)
	if err != nil {
		return err
	}

	for _, file := range files {
		fileNameWithExtension := file.Name()
		fileExtension := filepath.Ext(fileNameWithExtension)
		if fileExtension != ".ready" {
			continue
		}

		fileName := strings.TrimSuffix(fileNameWithExtension, fileExtension)
		fmt.Println(fileName)
	}
	return nil
}
