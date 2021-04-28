/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package postgres

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
)

// JoinInfo contains the information needed to bootstrap a new
// PostgreSQL replica
type JoinInfo struct {
	// The cluster name to join
	ClusterName string

	// The generated node name
	PodName string

	// Where the new instance must be written
	PgData string

	// The full hostname of the parent node
	ParentNode string
}

// Join create a new instance joined to an existing PostgreSQL cluster
func (info JoinInfo) Join() error {
	primaryConnInfo := buildPrimaryConnInfo(info.ParentNode, info.PodName) + " dbname=postgres connect_timeout=5"

	log.Log.Info("Waiting for primary to be available", "primaryConnInfo", primaryConnInfo)
	db, err := sql.Open("postgres", primaryConnInfo)
	if err != nil {
		return err
	}
	defer func() {
		_ = db.Close()
	}()

	err = waitForConnectionAvailable(db)
	if err != nil {
		return fmt.Errorf("primary server not available: %v", primaryConnInfo)
	}

	options := []string{
		"-D", info.PgData,
		"-v",
		"-w",
		"-d", primaryConnInfo,
	}
	cmd := exec.Command("pg_basebackup", options...) // #nosec
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	log.Log.Info("Starting pg_basebackup", "options", options)
	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("error in pg_basebackup, %w", err)
	}

	return UpdateReplicaConfiguration(info.PgData, info.ClusterName, info.PodName, false)
}
