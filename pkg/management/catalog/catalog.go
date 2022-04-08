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
	"sort"
	"strconv"
	"time"

	v1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/postgres"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"
)

// Catalog is a list of backup infos belonging to the same server
type Catalog struct {
	// The list of backups
	List []BarmanBackup `json:"backups_list"`
}

// LatestBackupInfo gets the information about the latest successful backup
func (catalog *Catalog) LatestBackupInfo() *BarmanBackup {
	if catalog.Len() == 0 {
		return nil
	}

	// Skip errored backups and return the latest valid one
	for i := len(catalog.List) - 1; i >= 0; i-- {
		if !catalog.List[i].BeginTime.IsZero() && !catalog.List[i].EndTime.IsZero() {
			return &catalog.List[i]
		}
	}

	return nil
}

// FirstRecoverabilityPoint gets the start time of the first backup in
// the catalog
func (catalog *Catalog) FirstRecoverabilityPoint() *time.Time {
	// the code below assumes the catalog to be sorted, therefore we enforce it first
	sort.Sort(catalog)
	if catalog.Len() == 0 {
		return nil
	}

	// Skip errored backups and return the first valid one
	for i := 0; i < len(catalog.List); i++ {
		if catalog.List[i].BeginTime.IsZero() || catalog.List[i].EndTime.IsZero() {
			continue
		}

		return &catalog.List[i].EndTime
	}

	return nil
}

// FindClosestBackupInfo finds the backup info that should
// use to file a PITR request via target parameters specified within `RecoveryTarget`
func (catalog *Catalog) FindClosestBackupInfo(recoveryTarget *v1.RecoveryTarget) (*BarmanBackup, error) {
	// the code below assumes the catalog to be sorted, therefore we enforce it first
	sort.Sort(catalog)
	targetTLI := recoveryTarget.TargetTLI

	if t := recoveryTarget.TargetTime; t != "" {
		backup, err := catalog.findClosestBackupFromTargetTime(t, targetTLI)
		if err != nil || backup != nil {
			return backup, err
		}
	}

	if t := recoveryTarget.TargetLSN; t != "" {
		backup, err := catalog.findClosestBackupFromTargetLSN(t, targetTLI)
		if err != nil || backup != nil {
			return backup, err
		}
	}

	if recoveryTarget.TargetName != "" || recoveryTarget.TargetXID != "" {
		return catalog.LatestBackupInfo(), nil
	}

	return nil, nil
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
		if (strconv.Itoa(barmanBackup.TimeLine) == targetTLI ||
			// if targetTLI is not an integer, it will be ignored actually
			targetTLI == "" || targetTLI == "latest" || targetTLI == "current") &&
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
		if (strconv.Itoa(barmanBackup.TimeLine) == targetTLI ||
			// if targetTLI is not an integer, it will be ignored actually
			targetTLI == "" || targetTLI == "latest" || targetTLI == "current") &&
			!barmanBackup.BeginTime.IsZero() && barmanBackup.BeginTime.Before(targetTime) {
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

// NewCatalog creates a new sorted backup catalog, given a list of backup infos
// belonging to the same server.
func NewCatalog(list []BarmanBackup) *Catalog {
	result := &Catalog{
		List: list,
	}
	sort.Sort(result)

	return result
}
