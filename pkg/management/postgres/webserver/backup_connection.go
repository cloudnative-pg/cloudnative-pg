/*
Copyright The CloudNativePG Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package webserver

import (
	"context"
	"database/sql"
	"errors"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
)

// BackupResultData is the result of executing pg_start_backup and pg_stop_backup
type BackupResultData struct {
	BeginLSN   string `json:"beginLSN,omitempty"`
	EndLSN     string `json:"endLSN,omitempty"`
	LabelFile  []byte `json:"labelFile,omitempty"`
	SpcmapFile []byte `json:"spcmapFile,omitempty"`
	BackupName string `json:"backupName,omitempty"`
}

type backupConnection struct {
	immediateCheckpoint  bool
	waitForArchive       bool
	conn                 *sql.Conn
	postgresMajorVersion uint64
	data                 BackupResultData
}

func newBackupConnection(
	ctx context.Context,
	instance *postgres.Instance,
	backupName string,
	immediateCheckpoint bool,
	waitForArchive bool,
) (*backupConnection, error) {
	superUserDB, err := instance.GetSuperUserDB()
	if err != nil {
		return nil, err
	}

	// the context is used only while obtaining the connection
	conn, err := superUserDB.Conn(ctx)
	if err != nil {
		return nil, err
	}

	vers, err := instance.GetPgVersion()
	if err != nil {
		return nil, err
	}
	return &backupConnection{
		immediateCheckpoint:  immediateCheckpoint,
		waitForArchive:       waitForArchive,
		conn:                 conn,
		postgresMajorVersion: vers.Major,
		data:                 BackupResultData{BackupName: backupName},
	}, nil
}

func (bc *backupConnection) startBackup(ctx context.Context) error {
	if bc == nil {
		return errors.New("backupConnection not initialized")
	}

	var row *sql.Row
	if bc.postgresMajorVersion < 15 {
		row = bc.conn.QueryRowContext(ctx, "SELECT pg_start_backup(label => $1, fast => $2);", bc.data.BackupName,
			bc.immediateCheckpoint)
	} else {
		row = bc.conn.QueryRowContext(ctx, "SELECT pg_backup_start(label => $1, fast => $2);", bc.data.BackupName,
			bc.immediateCheckpoint)
	}

	return row.Scan(&bc.data.BeginLSN)
}

func (bc *backupConnection) stopBackup(ctx context.Context) error {
	if bc == nil {
		return errors.New("backupConnection not initialized")
	}

	contextLogger := log.FromContext(ctx)

	var row *sql.Row
	if bc.postgresMajorVersion < 15 {
		row = bc.conn.QueryRowContext(ctx,
			"SELECT lsn, labelfile, spcmapfile FROM pg_stop_backup(wait_for_archive => $1);", bc.waitForArchive)
	} else {
		row = bc.conn.QueryRowContext(ctx,
			"SELECT lsn, labelfile, spcmapfile FROM pg_backup_stop(wait_for_archive => $1);", bc.waitForArchive)
	}

	if err := row.Scan(&bc.data.EndLSN, &bc.data.LabelFile, &bc.data.SpcmapFile); err != nil {
		return err
	}

	if err := bc.conn.Close(); err != nil {
		contextLogger.Error(err, "while closing backup connection")
	}

	return nil
}
