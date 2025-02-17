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
	"fmt"
	"regexp"
	"sync"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/cloudnative-pg/machinery/pkg/types"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/pool"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/utils"
)

// BackupResultData is the result of executing pg_start_backup and pg_stop_backup
type BackupResultData struct {
	BeginLSN   types.LSN             `json:"beginLSN,omitempty"`
	EndLSN     types.LSN             `json:"endLSN,omitempty"`
	LabelFile  []byte                `json:"labelFile,omitempty"`
	SpcmapFile []byte                `json:"spcmapFile,omitempty"`
	BackupName string                `json:"backupName,omitempty"`
	Phase      BackupConnectionPhase `json:"phase,omitempty"`
	// TODO: evaluate
	Error string `json:"error,omitempty"`
}

// BackupConnectionPhase a connection phase of the backup
type BackupConnectionPhase string

// A backup phase
const (
	Starting  BackupConnectionPhase = "starting"
	Started   BackupConnectionPhase = "started"
	Closing   BackupConnectionPhase = "closing"
	Completed BackupConnectionPhase = "completed"
	Errored   BackupConnectionPhase = "errored"
)

// IsTerminatedPhase returns true if the phase is a terminated phase
func (b BackupConnectionPhase) IsTerminatedPhase() bool {
	return b == Completed || b == Errored
}

// replicationSlotInvalidCharacters matches every character that is
// not valid in a replication slot name
var replicationSlotInvalidCharacters = regexp.MustCompile(`[^a-z0-9_]`)

type backupConnection struct {
	sync                 sync.Mutex
	immediateCheckpoint  bool
	waitForArchive       bool
	conn                 *sql.Conn
	postgresMajorVersion uint64
	data                 BackupResultData
}

func (bc *backupConnection) setPhase(phase BackupConnectionPhase, backupName string) {
	bc.sync.Lock()
	defer bc.sync.Unlock()
	if backupName != bc.data.BackupName {
		return
	}
	bc.data.Phase = phase
}

func newBackupConnection(
	ctx context.Context,
	backupName string,
	immediateCheckpoint bool,
	waitForArchive bool,
) (*backupConnection, error) {
	const applicationName = "cnpg-instance-manager-backup"
	// we don't use the instance connections as a safeguard measure to avoid connection saturation due to possible
	// connection leaks see #6761.
	baseConnString := postgres.BuildPostgresUnixConnectionString(applicationName)
	dsn := pool.AddDBNameToConnectionString(baseConnString, "postgres")
	superUserDB, err := pool.NewConnection(dsn, pool.ConnectionProfilePostgresql, 1)
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

func (bc *backupConnection) close() error {
	if bc == nil {
		return nil
	}

	if bc.conn == nil {
		return nil
	}

	return bc.conn.Close()
}

func (bc *backupConnection) startBackup(ctx context.Context, mutex *sync.Mutex) {
	mutex.Lock()
	defer mutex.Unlock()

	contextLogger := log.FromContext(ctx).WithValues("step", "start")

	if bc == nil {
		return
	}

	defer func() {
		if bc.data.Error == "" {
			return
		}
		contextLogger.Error(errors.New(bc.data.Error), "encountered error while starting backup")

		if err := bc.close(); err != nil {
			if !errors.Is(err, sql.ErrConnDone) {
				contextLogger.Error(err, "while closing backup connection")
			}
		}
		bc.data.Phase = Errored
	}()

	// TODO: refactor with the same logic of GetSlotNameFromInstanceName in the api package
	slotName := replicationSlotInvalidCharacters.ReplaceAllString(bc.data.BackupName, "_")
	if _, err := bc.conn.ExecContext(
		ctx,
		"SELECT pg_create_physical_replication_slot(slot_name => $1, immediately_reserve => true, temporary => true)",
		slotName,
	); err != nil {
		bc.data.Error = fmt.Sprintf("while creating the replication slot: %s", err.Error())
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
		contextLogger.Error(err, "while starting PostgreSQL physical backup")
		bc.data.Error = fmt.Sprintf("while scanning backup start: %s", err.Error())
		return
	}
	bc.data.Phase = Started
}

func (bc *backupConnection) stopBackup(ctx context.Context, mutex *sync.Mutex) {
	mutex.Lock()
	defer mutex.Unlock()
	contextLogger := log.FromContext(ctx).WithValues("step", "stop")

	if bc == nil {
		return
	}

	defer func() {
		if err := bc.close(); err != nil {
			if !errors.Is(err, sql.ErrConnDone) {
				contextLogger.Error(err, "while closing backup connection")
			}
		}
		if bc.data.Error != "" {
			bc.data.Phase = Errored
		} else {
			bc.data.Phase = Completed
		}
	}()

	if bc.data.Error != "" {
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
		contextLogger.Error(err, "while stopping PostgreSQL physical backup")
		bc.data.Error = fmt.Sprintf("while scanning backup stop: %s", err.Error())
	}
}
