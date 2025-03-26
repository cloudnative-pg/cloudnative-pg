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

// Package walarchivequeue implement the wal-archive-queue command
package walarchivequeue

import (
	"fmt"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/spf13/cobra"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
)

// NewCmd creates the new cobra command
func NewCmd() *cobra.Command {
	cmd := cobra.Command{
		Use:   "wal-archive-queue",
		Short: "Lists all .ready wal files in " + specs.PgWalArchiveStatusPath,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			contextLogger := log.FromContext(ctx)

			if err := run(); err != nil {
				contextLogger.Error(err, "Error while extracting the list of .ready files")
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
