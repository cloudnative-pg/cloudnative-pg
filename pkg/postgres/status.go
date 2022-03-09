/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package postgres

import (
	corev1 "k8s.io/api/core/v1"
)

// PostgresqlStatus defines a status for every instance in the cluster
type PostgresqlStatus struct {
	CurrentLsn                LSN        `json:"currentLsn,omitempty"`
	ReceivedLsn               LSN        `json:"receivedLsn,omitempty"`
	ReplayLsn                 LSN        `json:"replayLsn,omitempty"`
	SystemID                  string     `json:"systemID"`
	IsPrimary                 bool       `json:"isPrimary"`
	ReplayPaused              bool       `json:"replayPaused"`
	PendingRestart            bool       `json:"pendingRestart"`
	PendingRestartForDecrease bool       `json:"pendingRestartForDecrease"`
	IsWalReceiverActive       bool       `json:"isWalReceiverActive"`
	Node                      string     `json:"node"`
	Pod                       corev1.Pod `json:"pod"`
	IsPgRewindRunning         bool       `json:"isPgRewindRunning"`
	TotalInstanceSize         string     `json:"totalInstanceSize"`
	IsFencingOn               bool       `json:"isFencingOn"`

	// WAL Status
	// SELECT
	//		last_archived_wal,
	// 		last_archived_time,
	// 		last_failed_wal,
	// 		last_failed_time,
	// 		COALESCE(last_archived_time,'-infinity') > COALESCE(last_failed_time, '-infinity') AS is_archiving,
	// 		pg_walfile_name(pg_current_wal_lsn()) as current_wal
	// FROM pg_stat_archiver;
	LastArchivedWAL     string `json:"lastArchivedWAL,omitempty"`
	LastArchivedWALTime string `json:"lastArchivedWALTime,omitempty"`
	LastFailedWAL       string `json:"lastFailedWAL,omitempty"`
	LastFailedWALTime   string `json:"lastFailedWALTime,omitempty"`
	IsArchivingWAL      bool   `json:"isArchivingWAL,omitempty"`
	CurrentWAL          string `json:"currentWAL,omitempty"`

	// Is the number of '.ready' wal files contained in the wal archive folder
	ReadyWALFiles int `json:"readyWalFiles,omitempty"`

	// The current timeline ID
	// SELECT timeline_id FROM pg_control_checkpoint()
	TimeLineID int `json:"timeLineID,omitempty"`

	// This field is set when there is an error while extracting the
	// status of a Pod
	Error   error `json:"-"`
	IsReady bool  `json:"isReady"`

	// Status of the instance manager
	ExecutableHash             string `json:"executableHash"`
	IsInstanceManagerUpgrading bool   `json:"isInstanceManagerUpgrading"`
	InstanceManagerVersion     string `json:"instanceManagerVersion"`
	InstanceArch               string `json:"instanceArch"`

	// contains the PgStatReplication rows content.
	ReplicationInfo []PgStatReplication `json:"replicationInfo,omitempty"`
}

// PgStatReplication contains the replications of replicas as reported by the primary instance
type PgStatReplication struct {
	ApplicationName string `json:"applicationName,omitempty"`
	State           string `json:"state,omitempty"`
	SentLsn         LSN    `json:"receivedLsn,omitempty"`
	WriteLsn        LSN    `json:"writeLsn,omitempty"`
	FlushLsn        LSN    `json:"flushLsn,omitempty"`
	ReplayLsn       LSN    `json:"replayLsn,omitempty"`
	WriteLag        string `json:"writeLag,omitempty"`
	FlushLag        string `json:"flushLag,omitempty"`
	ReplayLag       string `json:"replayLag,omitempty"`
	SyncState       string `json:"syncState,omitempty"`
	SyncPriority    string `json:"syncPriority,omitempty"`
}

// PostgresqlStatusList is a list of PostgreSQL instances status, useful to
// be easily sorted
type PostgresqlStatusList struct {
	Items []PostgresqlStatus `json:"items"`
}

// Len implements sort.Interface extracting the length of the list
func (list *PostgresqlStatusList) Len() int {
	return len(list.Items)
}

// Swap swaps two elements, implements sort.Interface
func (list *PostgresqlStatusList) Swap(i, j int) {
	list.Items[i], list.Items[j] = list.Items[j], list.Items[i]
}

// Less compares two elements. Primary instances always go first, ordered by their Pod
// name (split brain?), and secondaries always go by their replication status with
// the more updated one coming as first
func (list *PostgresqlStatusList) Less(i, j int) bool {
	// Incomplete status records go to the bottom of
	// the list, since this is used to elect a new primary
	// when needed.
	switch {
	case list.Items[i].Error != nil && list.Items[j].Error == nil:
		return false
	case list.Items[i].Error == nil && list.Items[j].Error != nil:
		return true
	}

	// Non-ready Pods go to the bottom of the list
	// since we prefer ready Pods as new primary
	switch {
	case list.Items[i].IsReady && !list.Items[j].IsReady:
		return true
	case !list.Items[i].IsReady && list.Items[j].IsReady:
		return false
	}

	// Manage primary servers
	switch {
	case list.Items[i].IsPrimary && list.Items[j].IsPrimary:
		return list.Items[i].Pod.Name < list.Items[j].Pod.Name

	case list.Items[i].IsPrimary:
		return true

	case list.Items[j].IsPrimary:
		return false
	}

	// Compare received LSN (bigger LSN orders first)
	if list.Items[i].ReceivedLsn != list.Items[j].ReceivedLsn {
		return !list.Items[i].ReceivedLsn.Less(list.Items[j].ReceivedLsn)
	}

	// Compare replay LSN (bigger LSN orders first)
	if list.Items[i].ReplayLsn != list.Items[j].ReplayLsn {
		return !list.Items[i].ReplayLsn.Less(list.Items[j].ReplayLsn)
	}

	return list.Items[i].Pod.Name < list.Items[j].Pod.Name
}

// AreWalReceiversDown checks if every WAL receiver of the cluster is down
// ignoring the status of the primary, that does not matter during
// a switchover or a failover
func (list PostgresqlStatusList) AreWalReceiversDown(primaryName string) bool {
	for idx := range list.Items {
		if list.Items[idx].Pod.Name == primaryName {
			continue
		}
		if list.Items[idx].IsWalReceiverActive {
			return false
		}
	}

	return true
}

// IsPodReporting if a pod is ready
func (list PostgresqlStatusList) IsPodReporting(podname string) bool {
	for _, item := range list.Items {
		if item.Pod.Name == podname {
			return item.Error == nil
		}
	}

	return false
}

// IsComplete checks the PostgreSQL status list for Pods which
// contain errors. Returns true if everything is green and
// false otherwise
func (list PostgresqlStatusList) IsComplete() bool {
	for idx := range list.Items {
		if list.Items[idx].Error != nil {
			return false
		}
	}

	return true
}

// ArePodsUpgradingInstanceManager checks if there are pods on which we are upgrading the instance manager
func (list PostgresqlStatusList) ArePodsUpgradingInstanceManager() bool {
	for _, item := range list.Items {
		if item.IsInstanceManagerUpgrading {
			return true
		}
	}

	return false
}

// ArePodsWaitingForDecreasedSettings checks if a rollout due to hot standby
// sensible parameters being decreased is ongoing
func (list PostgresqlStatusList) ArePodsWaitingForDecreasedSettings() bool {
	for _, item := range list.Items {
		if item.PendingRestartForDecrease {
			return true
		}
	}

	return false
}

// ShouldSkipReconcile checks whether at least an instance is asking for the reconciliation loop to be skipped
func (list PostgresqlStatusList) ShouldSkipReconcile() bool {
	for _, item := range list.Items {
		if item.IsFencingOn {
			return true
		}
	}

	return false
}
