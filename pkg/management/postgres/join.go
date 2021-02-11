/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package postgres

import (
	"fmt"
	"os"
	"os/exec"
	"path"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/fileutils"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/postgres"
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
	primaryConnInfo := buildPrimaryConnInfo(info.ParentNode, info.PodName)

	options := []string{
		"-D", info.PgData,
		"-v",
		"-R",
		"-w",
		"-d", primaryConnInfo,
	}
	cmd := exec.Command("pg_basebackup", options...) // #nosec
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	log.Log.Info("Starting pg_basebackup", "options", options)
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("error in pg_basebackup, %w", err)
	}

	// The generated recovery.conf / postgresql.auto.conf doesn't instruct
	// the instance to follow the timeline changes of the primary, so we
	// need to include another parameter in the configuration.
	major, err := postgres.GetMajorVersion(info.PgData)
	if err != nil {
		return err
	}

	targetFile := "postgresql.auto.conf"
	if major < 12 {
		targetFile = "recovery.conf"
	}
	targetFile = path.Join(info.PgData, targetFile)

	// Append the required configuration parameter
	err = fileutils.AppendStringToFile(targetFile, "recovery_target_timeline = 'latest'\n")
	if err != nil {
		return err
	}

	return nil
}
