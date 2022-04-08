/*
Copyright 2019-2022 The CloudNativePG Contributors

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
	"sort"
	"time"
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
// use to file a PITR request for a certain time
func (catalog *Catalog) FindClosestBackupInfo(pit time.Time) *BarmanBackup {
	for i := len(catalog.List) - 1; i >= 0; i-- {
		if !catalog.List[i].BeginTime.IsZero() && catalog.List[i].BeginTime.Before(pit) {
			return &catalog.List[i]
		}
	}

	return nil
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
