/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package postgres

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/pkg/errors"

	"github.com/2ndquadrant/cloud-native-postgresql/pkg/management/log"
)

// JoinInfo contains the information needed to bootstrap a new
// PostgreSQL replica
type JoinInfo struct {
	// The generated node name
	PodName string

	// Where the new instance must be written
	PgData string

	// The full hostname of the parent node
	ParentNode string
}

// Join create a new instance joined to an existing PostgreSQL cluster
func (info JoinInfo) Join() error {
	options := []string{
		"-D", info.PgData,
		"-v",
		"-R",
		"-d", fmt.Sprintf("host=%v dbname=%v", info.ParentNode, "postgres"),
	}
	cmd := exec.Command("pg_basebackup", options...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	log.Log.Info("Starting pg_basebackup", "options", options)
	err := cmd.Run()

	if err != nil {
		return errors.Wrap(err, "Error in pg_basebackup")
	}

	return nil
}
