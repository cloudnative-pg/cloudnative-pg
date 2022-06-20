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

// Package catalog is the implementation of a backup catalog
package catalog

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"time"

	v1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// Catalog is a list of backup infos belonging to the same server
type Catalog struct {
	// The list of backups
	List []BarmanBackup `json:"backups_list"`
}

var currentTLIRegex = regexp.MustCompile("^(|latest)$")

// LatestBackupInfo gets the information about the latest successful backup
func (catalog *Catalog) LatestBackupInfo() *BarmanBackup {
	if catalog.Len() == 0 {
		return nil
	}

	// Skip errored backups and return the latest valid one
	for i := len(catalog.List) - 1; i >= 0; i-- {
		if catalog.List[i].isBackupDone() {
			return &catalog.List[i]
		}
	}

	return nil
}

// FirstRecoverabilityPoint gets the start time of the first backup in
// the catalog
func (catalog *Catalog) FirstRecoverabilityPoint() *time.Time {
	if catalog.Len() == 0 {
		return nil
	}

	// the code below assumes the catalog to be sorted, therefore we enforce it first
	sort.Sort(catalog)

	// Skip errored backups and return the first valid one
	for i := 0; i < len(catalog.List); i++ {
		if !catalog.List[i].isBackupDone() {
			continue
		}

		return &catalog.List[i].EndTime
	}

	return nil
}

// FindBackupInfo finds the backup info that should be used to file
// a PITR request via target parameters specified within `RecoveryTarget`
func (catalog *Catalog) FindBackupInfo(recoveryTarget *v1.RecoveryTarget) (*BarmanBackup, error) {
	// the code below assumes the catalog to be sorted, therefore we enforce it first
	sort.Sort(catalog)
	targetTLI := recoveryTarget.TargetTLI

	if t := recoveryTarget.TargetTime; t != "" {
		return catalog.findClosestBackupFromTargetTime(t, targetTLI)
	}

	if t := recoveryTarget.TargetLSN; t != "" {
		return catalog.findClosestBackupFromTargetLSN(t, targetTLI)
	}

	// TargetName, TargetXID, and TargetImmediate recovery targets require
	// the BackupID field to be defined.
	if recoveryTarget.TargetName != "" ||
		recoveryTarget.TargetXID != "" ||
		recoveryTarget.TargetImmediate != nil {
		return catalog.findBackupFromID(recoveryTarget.BackupID)
	}

	// targetXID, targetName will be ignored in choosing the proper backup
	return catalog.findlatestBackupFromTimeline(targetTLI), nil
}

func (catalog *Catalog) findClosestBackupFromTargetLSN(
	targetLSNString string,
	targetTLI string,
) (*BarmanBackup, error) {
	targetLSN := postgres.LSN(targetLSNString)
	if _, err := targetLSN.Parse(); err != nil {
		return nil, fmt.Errorf("while parsing recovery target targetLSN: " + err.Error())
	}
	for i := len(catalog.List) - 1; i >= 0; i-- {
		barmanBackup := catalog.List[i]
		if !barmanBackup.isBackupDone() {
			continue
		}
		if (strconv.Itoa(barmanBackup.TimeLine) == targetTLI ||
			// if targetTLI is not an integer, it will be ignored actually
			currentTLIRegex.MatchString(targetTLI)) &&
			postgres.LSN(barmanBackup.BeginLSN).Less(targetLSN) {
			return &catalog.List[i], nil
		}
	}
	return nil, nil
}

func (catalog *Catalog) findClosestBackupFromTargetTime(
	targetTimeString string,
	targetTLI string,
) (*BarmanBackup, error) {
	targetTime, err := utils.ParseTargetTime(nil, targetTimeString)
	if err != nil {
		return nil, fmt.Errorf("while parsing recovery target targetTime: " + err.Error())
	}
	for i := len(catalog.List) - 1; i >= 0; i-- {
		barmanBackup := catalog.List[i]
		if !barmanBackup.isBackupDone() {
			continue
		}
		if (strconv.Itoa(barmanBackup.TimeLine) == targetTLI ||
			// if targetTLI is not an integer, it will be ignored actually
			currentTLIRegex.MatchString(targetTLI)) &&
			barmanBackup.EndTime.Before(targetTime) {
			return &catalog.List[i], nil
		}
	}
	return nil, nil
}

func (catalog *Catalog) findlatestBackupFromTimeline(targetTLI string) *BarmanBackup {
	for i := len(catalog.List) - 1; i >= 0; i-- {
		barmanBackup := catalog.List[i]
		if !barmanBackup.isBackupDone() {
			continue
		}
		if strconv.Itoa(barmanBackup.TimeLine) == targetTLI ||
			// if targetTLI is not an integer, it will be ignored actually
			currentTLIRegex.MatchString(targetTLI) {
			return &catalog.List[i]
		}
	}

	return nil
}

func (catalog *Catalog) findBackupFromID(backupID string) (*BarmanBackup, error) {
	if backupID == "" {
		return nil, fmt.Errorf("no backupID provided")
	}
	for i := len(catalog.List) - 1; i >= 0; i-- {
		barmanBackup := catalog.List[i]
		if !barmanBackup.isBackupDone() {
			continue
		}
		if barmanBackup.ID == backupID {
			return &catalog.List[i], nil
		}
	}
	return nil, nil
}

// BarmanBackup represent a backup as created
// by Barman
type BarmanBackup struct {
	// The backup label
	Label string `json:"backup_label"`

	// The moment where the backup started
	BeginTimeString string `json:"begin_time"`

	// The moment where the backup ended
	EndTimeString string `json:"end_time"`

	// The moment where the backup ended
	BeginTime time.Time

	// The moment where the backup ended
	EndTime time.Time

	// The WAL where the backup started
	BeginWal string `json:"begin_wal"`

	// The WAL where the backup ended
	EndWal string `json:"end_wal"`

	// The LSN where the backup started
	BeginLSN string `json:"begin_xlog"`

	// The LSN where the backup ended
	EndLSN string `json:"end_xlog"`

	// The systemID of the cluster
	SystemID string `json:"systemid"`

	// The ID of the backup
	ID string `json:"backup_id"`

	// The error output if present
	Error string `json:"error"`

	// The TimeLine
	TimeLine int `json:"timeline"`
}

func (b *BarmanBackup) isBackupDone() bool {
	return !b.BeginTime.IsZero() && !b.EndTime.IsZero()
}

// NewCatalog creates a new sorted backup catalog, given a list of backup infos
// belonging to the same server.
func NewCatalog(list []BarmanBackup) *Catalog {
	result := &Catalog{
		List: list,
	}
	sort.Sort(result)

	return result
}
