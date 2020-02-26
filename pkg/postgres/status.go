/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/
package postgres

import corev1 "k8s.io/api/core/v1"

// PostgresqlStatus defines a status for every instance in the cluster
type PostgresqlStatus struct {
	PodName     string `json:"podName"`
	IsPrimary   bool   `json:"isPrimary"`
	ReceivedLsn LSN    `json:"receivedLsn,omitempty"`
	ReplayLsn   LSN    `json:"replayLsn,omitempty"`
}

// A list of PostgreSQL instances status, useful to be easily sorted
type PostgresqlStatusList struct {
	Items []PostgresqlStatus
}

// Len implements sort.Interface extracting the length of the list
func (list PostgresqlStatusList) Len() int {
	return len(list.Items)
}

// Swap swaps two elements, implements sort.Interface
func (list *PostgresqlStatusList) Swap(i, j int) {
	t := list.Items[i]
	list.Items[i] = list.Items[j]
	list.Items[j] = t
}

// Less compare two elements. Primary instances always go first, ordered by their Pod
// name (split brain?), and secondaries always go by their replication status with
// the more updated one coming as first
func (list PostgresqlStatusList) Less(i, j int) bool {
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

// Get a certain Pod from a Pod list
func ExtractPodFromList(pods []corev1.Pod, podName string) *corev1.Pod {
	for idx := range pods {
		if pods[idx].Name == podName {
			return &pods[idx]
		}
	}

	return nil
}
