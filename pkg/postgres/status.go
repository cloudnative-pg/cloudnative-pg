/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package postgres

import corev1 "k8s.io/api/core/v1"

// PostgresqlStatus defines a status for every instance in the cluster
type PostgresqlStatus struct {
	CurrentLsn          LSN        `json:"currentLsn,omitempty"`
	ReceivedLsn         LSN        `json:"receivedLsn,omitempty"`
	ReplayLsn           LSN        `json:"replayLsn,omitempty"`
	SystemID            string     `json:"systemID"`
	IsPrimary           bool       `json:"isPrimary"`
	ReplayPaused        bool       `json:"replayPaused"`
	PendingRestart      bool       `json:"pendingRestart"`
	IsWalReceiverActive bool       `json:"isWalReceiverActive"`
	Node                string     `json:"node"`
	Pod                 corev1.Pod `json:"pod"`

	// This field is set when there is an error while extracting the
	// status of a Pod
	Error   error `json:"-"`
	IsReady bool  `json:"isReady"`
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

// Less compare two elements. Primary instances always go first, ordered by their Pod
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

// AreWalReceiversDown check if every WAL receiver of the cluster is down
func (list PostgresqlStatusList) AreWalReceiversDown() bool {
	for idx := range list.Items {
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

// IsComplete check the PostgreSQL status list for Pods which
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
