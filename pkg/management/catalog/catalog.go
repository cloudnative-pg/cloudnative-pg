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
	"encoding/json"
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

// NewCatalogFromBarmanCloudBackupList parses the output of barman-cloud-backup-list
func NewCatalogFromBarmanCloudBackupList(rawJSON string) (*Catalog, error) {
	result := &Catalog{}
	err := json.Unmarshal([]byte(rawJSON), result)
	if err != nil {
		return nil, err
	}

	for idx := range result.List {
		if err := result.List[idx].deserializeBackupTimeStrings(); err != nil {
			return nil, err
		}
	}

	// Sort the list of backups in order of time
	sort.Sort(result)

	return result, nil
}

var currentTLIRegex = regexp.MustCompile("^(|latest)$")

// LatestBackupInfo gets the information about the latest successful backup
func (catalog *Catalog) LatestBackupInfo() *BarmanBackup {
	if catalog.Len() == 0 {
		return nil
	}

	// the code below assumes the catalog to be sorted, therefore, we enforce it first
	sort.Sort(catalog)

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

	// the code below assumes the catalog to be sorted, therefore, we enforce it first
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
	// Check that BackupID is not empty. In such case, always use the
	// backup ID provided by the user.
	if recoveryTarget.BackupID != "" {
		return catalog.findBackupFromID(recoveryTarget.BackupID)
	}

	// The user has not specified any backup ID. As a result we need
	// to automatically detect the backup from which to start the
	// recovery process.

	// Set the timeline
	targetTLI := recoveryTarget.TargetTLI

	// Sort the catalog, as that's what the code below expects
	sort.Sort(catalog)

	// The first step is to check any time based research
	if t := recoveryTarget.TargetTime; t != "" {
		return catalog.findClosestBackupFromTargetTime(t, targetTLI)
	}

	// The second step is to check any LSN based research
	if t := recoveryTarget.TargetLSN; t != "" {
		return catalog.findClosestBackupFromTargetLSN(t, targetTLI)
	}

	// The fallback is to use the latest available backup in chronological order
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
			!barmanBackup.EndTime.After(targetTime) {
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
	for _, barmanBackup := range catalog.List {
		if !barmanBackup.isBackupDone() {
			continue
		}
		if barmanBackup.ID == backupID {
			return &barmanBackup, nil
		}
	}
	return nil, fmt.Errorf("no backup found with ID %s", backupID)
}

// BarmanBackup represent a backup as created
// by Barman
type BarmanBackup struct {
	// The backup name, can be used as a way to identify a backup.
	// Populated only if the backup was executed with barman 3.3.0+.
	BackupName string `json:"backup_name,omitempty"`

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

type barmanBackupShow struct {
	Cloud BarmanBackup `json:"cloud,omitempty"`
}

// NewBackupFromBarmanCloudBackupShow parses the output of barman-cloud-backup-show
func NewBackupFromBarmanCloudBackupShow(rawJSON string) (*BarmanBackup, error) {
	result := &barmanBackupShow{}
	err := json.Unmarshal([]byte(rawJSON), result)
	if err != nil {
		return nil, err
	}

	if err := result.Cloud.deserializeBackupTimeStrings(); err != nil {
		return nil, err
	}

	return &result.Cloud, nil
}

func (b *BarmanBackup) deserializeBackupTimeStrings() error {
	// barmanTimeLayout is the format that is being used to parse
	// the backupInfo from barman-cloud-backup-list
	const (
		barmanTimeLayout = "Mon Jan 2 15:04:05 2006"
	)

	var err error
	if b.BeginTimeString != "" {
		b.BeginTime, err = time.Parse(barmanTimeLayout, b.BeginTimeString)
		if err != nil {
			return err
		}
	}

	if b.EndTimeString != "" {
		b.EndTime, err = time.Parse(barmanTimeLayout, b.EndTimeString)
		if err != nil {
			return err
		}
	}

	return nil
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
