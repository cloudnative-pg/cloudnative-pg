/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package postgres

// PostgresqlStatus defines a status for every instance in the cluster
type PostgresqlStatus struct {
	PodName             string `json:"podName"`
	CurrentLsn          LSN    `json:"currentLsn,omitempty"`
	ReceivedLsn         LSN    `json:"receivedLsn,omitempty"`
	ReplayLsn           LSN    `json:"replayLsn,omitempty"`
	SystemID            string `json:"systemID"`
	IsPrimary           bool   `json:"isPrimary"`
	ReplayPaused        bool   `json:"replayPaused"`
	PendingRestart      bool   `json:"pendingRestart"`
	IsWalReceiverActive bool   `json:"isWalReceiverActive"`
	Node                string `json:"node"`

	// This field is set when there is an error while extracting the
	// status of a Pod
	ExecError error `json:"-"`
	IsReady   bool  `json:"isReady"`
}

// PostgresqlStatusList is a list of PostgreSQL instances status, useful to
// be easily sorted
type PostgresqlStatusList struct {
	Items []PostgresqlStatus `json:"items"`
}

// Len implements sort.Interface extracting the length of the list
func (list PostgresqlStatusList) Len() int {
	return len(list.Items)
}

// Swap swaps two elements, implements sort.Interface
func (list *PostgresqlStatusList) Swap(i, j int) {
	list.Items[i], list.Items[j] = list.Items[j], list.Items[i]
}

// Less compare two elements. Primary instances always go first, ordered by their Pod
// name (split brain?), and secondaries always go by their replication status with
// the more updated one coming as first
func (list PostgresqlStatusList) Less(i, j int) bool {
	// Incomplete status records go to the bottom of
	// the list, since this is used to elect a new primary
	// when needed.
	switch {
	case list.Items[i].ExecError != nil && list.Items[j].ExecError == nil:
		return false
	case list.Items[i].ExecError == nil && list.Items[j].ExecError != nil:
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
		return list.Items[i].PodName < list.Items[j].PodName

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

	return list.Items[i].PodName < list.Items[j].PodName
}

// AreWalReceiversDown check if every WAL receiver of the cluster is down
func (list PostgresqlStatusList) AreWalReceiversDown() bool {
	for idx := range list.Items {
		if list.Items[idx].IsWalReceiverActive {
			return false
		}
	}

	return true
}

// IsComplete check the PostgreSQL status list for Pods which
// contain errors. Returns true if everything is green and
// false otherwise
func (list PostgresqlStatusList) IsComplete() bool {
	for idx := range list.Items {
		if list.Items[idx].ExecError != nil {
			return false
		}
	}

	return true
}
