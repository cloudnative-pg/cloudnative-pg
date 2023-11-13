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
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/utils"
	postgresUtils "github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
)

// BackupResultData is the result of executing pg_start_backup and pg_stop_backup
type BackupResultData struct {
	BeginLSN   postgresUtils.LSN     `json:"beginLSN,omitempty"`
	EndLSN     postgresUtils.LSN     `json:"endLSN,omitempty"`
	LabelFile  []byte                `json:"labelFile,omitempty"`
	SpcmapFile []byte                `json:"spcmapFile,omitempty"`
	BackupName string                `json:"backupName,omitempty"`
	Phase      BackupConnectionPhase `json:"phase,omitempty"`
}

// BackupConnectionPhase a connection phase of the backup
type BackupConnectionPhase string

// A backup phase
const (
	Starting  BackupConnectionPhase = "starting"
	Started   BackupConnectionPhase = "started"
	Closing   BackupConnectionPhase = "closing"
	Completed BackupConnectionPhase = "completed"
)

type backupError struct {
	err   error
	phase BackupConnectionPhase
}

func (b backupError) Error() string {
	return fmt.Sprintf("encountered an error while executing phase: %s: %s", b.phase, b.err.Error())
}

// MarshalJSON implements the json.Marshaler interface for backupError.
func (b backupError) MarshalJSON() ([]byte, error) {
	type Serialize struct {
		Error string `json:"error"`
	}
	// Create a wrapper struct for JSON serialization that includes the error message.
	return json.Marshal(&Serialize{
		Error: b.Error(),
	})
}

func newBackupError(phase BackupConnectionPhase, err error) *backupError {
	if err == nil {
		return nil
	}

	return &backupError{phase: phase, err: err}
}

// replicationSlotInvalidCharacters matches every character that is
// not valid in a replication slot name
var replicationSlotInvalidCharacters = regexp.MustCompile(`[^a-z0-9_]`)

type backupConnection struct {
	immediateCheckpoint  bool
	waitForArchive       bool
	conn                 *sql.Conn
	postgresMajorVersion uint64
	data                 BackupResultData
	err                  *backupError
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

	vers, err := utils.GetPgVersion(superUserDB)
	if err != nil {
		return nil, err
	}

	// the context is used only while obtaining the connection
	conn, err := superUserDB.Conn(ctx)
	if err != nil {
		return nil, err
	}

	return &backupConnection{
		immediateCheckpoint:  immediateCheckpoint,
		waitForArchive:       waitForArchive,
		conn:                 conn,
		postgresMajorVersion: vers.Major,
		data: BackupResultData{
			BackupName: backupName,
			Phase:      Starting,
		},
	}, nil
}

func (bc *backupConnection) startBackup(ctx context.Context) {
	contextLogger := log.FromContext(ctx).WithValues("step", "start")

	if bc == nil {
		return
	}

	defer func() {
		if bc.err == nil {
			return
		}
		contextLogger.Error(bc.err, "encountered error while starting backup")

		if err := bc.conn.Close(); err != nil {
			if !errors.Is(err, sql.ErrConnDone) {
				contextLogger.Error(err, "while closing backup connection")
			}
		}
	}()

	// TODO: refactor with the same logic of GetSlotNameFromInstanceName in the api package
	slotName := replicationSlotInvalidCharacters.ReplaceAllString(bc.data.BackupName, "_")
	if _, err := bc.conn.ExecContext(
		ctx,
		"SELECT pg_create_physical_replication_slot(slot_name => $1, immediately_reserve => true, temporary => true)",
		slotName,
	); err != nil {
		bc.err = newBackupError(bc.data.Phase, bc.err)
		return
	}

	var row *sql.Row
	if bc.postgresMajorVersion < 15 {
		row = bc.conn.QueryRowContext(ctx, "SELECT pg_start_backup($1, $2, false);", bc.data.BackupName,
			bc.immediateCheckpoint)
	} else {
		row = bc.conn.QueryRowContext(ctx, "SELECT pg_backup_start(label => $1, fast => $2);", bc.data.BackupName,
			bc.immediateCheckpoint)
	}

	if err := row.Scan(&bc.data.BeginLSN); err != nil {
		bc.err = newBackupError(bc.data.Phase, err)
	}
	bc.data.Phase = Started
}

func (bc *backupConnection) stopBackup(ctx context.Context) {
	contextLogger := log.FromContext(ctx).WithValues("step", "stop")

	if bc == nil {
		return
	}

	defer func() {
		if err := bc.conn.Close(); err != nil {
			if !errors.Is(err, sql.ErrConnDone) {
				contextLogger.Error(err, "while closing backup connection")
			}
		}

	}()

	if bc.err != nil {
		return
	}

	var row *sql.Row
	if bc.postgresMajorVersion < 15 {
		row = bc.conn.QueryRowContext(ctx,
			"SELECT lsn, labelfile, spcmapfile FROM pg_stop_backup(false, $1);", bc.waitForArchive)
	} else {
		row = bc.conn.QueryRowContext(ctx,
			"SELECT lsn, labelfile, spcmapfile FROM pg_backup_stop(wait_for_archive => $1);", bc.waitForArchive)
	}

	if err := row.Scan(&bc.data.EndLSN, &bc.data.LabelFile, &bc.data.SpcmapFile); err != nil {
		bc.err = newBackupError(bc.data.Phase, err)
		contextLogger.Error(err, "while stopping PostgreSQL physical backup")
		return
	}

	bc.data.Phase = Completed
}
