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

package postgres

import (
	"context"
	"fmt"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/cloudnative-pg/machinery/pkg/types"
	corev1 "k8s.io/api/core/v1"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// PostgresqlStatus defines a status for every instance in the cluster
type PostgresqlStatus struct {
	CurrentLsn                types.LSN   `json:"currentLsn,omitempty"`
	ReceivedLsn               types.LSN   `json:"receivedLsn,omitempty"`
	ReplayLsn                 types.LSN   `json:"replayLsn,omitempty"`
	SystemID                  string      `json:"systemID"`
	IsPrimary                 bool        `json:"isPrimary"`
	ReplayPaused              bool        `json:"replayPaused"`
	PendingRestart            bool        `json:"pendingRestart"`
	PendingRestartForDecrease bool        `json:"pendingRestartForDecrease"`
	IsWalReceiverActive       bool        `json:"isWalReceiverActive"`
	IsPgRewindRunning         bool        `json:"isPgRewindRunning"`
	MightBeUnavailable        bool        `json:"mightBeUnavailable"`
	IsArchivingWAL            bool        `json:"isArchivingWAL,omitempty"`
	Node                      string      `json:"node"`
	Pod                       *corev1.Pod `json:"pod"`
	TotalInstanceSize         string      `json:"totalInstanceSize"`
	// populated when MightBeUnavailable reported a healthy status even if it found an error
	MightBeUnavailableMaskedError string `json:"mightBeUnavailableMaskedError,omitempty"`

	// Archiver status

	LastArchivedWAL     string `json:"lastArchivedWAL,omitempty"`
	LastArchivedWALTime string `json:"lastArchivedWALTime,omitempty"`
	LastFailedWAL       string `json:"lastFailedWAL,omitempty"`
	LastFailedWALTime   string `json:"lastFailedWALTime,omitempty"`

	// WAL Status

	CurrentWAL string `json:"currentWAL,omitempty"`

	// Is the number of '.ready' wal files contained in the wal archive folder
	ReadyWALFiles int `json:"readyWalFiles,omitempty"`

	// The current timeline ID
	// SELECT timeline_id FROM pg_control_checkpoint()
	TimeLineID int `json:"timeLineID,omitempty"`

	// This field is set when there is an error while extracting the
	// status of a Pod
	Error error `json:"-"`

	// contains the PgStatReplication rows content.
	ReplicationInfo PgStatReplicationList `json:"replicationInfo,omitempty"`
	// contains the PgReplicationSlot rows content.
	ReplicationSlotsInfo PgReplicationSlotList `json:"replicationSlotsInfo,omitempty"`
	// contains the PgStatBasebackup rows content.
	PgStatBasebackupsInfo []PgStatBasebackup `json:"pgStatBasebackupsInfo,omitempty"`

	// Status of the instance manager
	ExecutableHash             string `json:"executableHash"`
	InstanceManagerVersion     string `json:"instanceManagerVersion"`
	InstanceArch               string `json:"instanceArch"`
	IsInstanceManagerUpgrading bool   `json:"isInstanceManagerUpgrading"`

	// This field represents the Kubelet point-of-view of the readiness
	// status of this instance and may be slightly stale when the Kubelet has
	// not still invoked the readiness probe.
	//
	// If you want to check the latest detected status of PostgreSQL, you
	// need to call HasHTTPStatus().
	//
	// This field is never populated in the instance manager.
	IsPodReady bool `json:"isPodReady"`
}

// PgStatReplication contains the replications of replicas as reported by the primary instance
type PgStatReplication struct {
	ApplicationName string    `json:"applicationName,omitempty"`
	State           string    `json:"state,omitempty"`
	SentLsn         types.LSN `json:"receivedLsn,omitempty"`
	WriteLsn        types.LSN `json:"writeLsn,omitempty"`
	FlushLsn        types.LSN `json:"flushLsn,omitempty"`
	ReplayLsn       types.LSN `json:"replayLsn,omitempty"`
	WriteLag        string    `json:"writeLag,omitempty"`
	FlushLag        string    `json:"flushLag,omitempty"`
	ReplayLag       string    `json:"replayLag,omitempty"`
	SyncState       string    `json:"syncState,omitempty"`
	SyncPriority    string    `json:"syncPriority,omitempty"`
}

// PgStatBasebackup contains the information for progress of basebackup as reported by the primary instance
type PgStatBasebackup struct {
	Usename              string `json:"usename"`
	ApplicationName      string `json:"application_name"`
	BackendStart         string `json:"backend_start"`
	Phase                string `json:"phase"`
	BackupTotal          int64  `json:"backup_total"`
	BackupStreamed       int64  `json:"backup_streamed"`
	BackupTotalPretty    string `json:"backup_total_pretty"`
	BackupStreamedPretty string `json:"backup_streamed_pretty"`
	TablespacesTotal     int64  `json:"tablespaces_total"`
	TablespacesStreamed  int64  `json:"tablespaces_streamed"`
}

// AddPod store the Pod inside the status
func (status *PostgresqlStatus) AddPod(pod corev1.Pod) {
	status.Pod = &pod

	// IsPodReady is not populated by the instance manager, so we detect it from the
	// Pod status
	status.IsPodReady = utils.IsPodReady(pod)
	status.Node = pod.Spec.NodeName
}

// HasHTTPStatus checks if the instance manager is reporting this
// instance as ready.
//
// The result represents the state of PostgreSQL at the moment of the
// collection of the instance status and is more up-to-date than
// IsPodReady field, which is updated asynchronously.
func (status PostgresqlStatus) HasHTTPStatus() bool {
	// To load the status of this instance, we use the `/pg/status` endpoint
	// of the instance manager. PostgreSQL is ready and running if the
	// endpoint returns success, and the Error field will be nil.
	//
	// Otherwise, we didn't manage to collect the status of the PostgreSQL
	// instance, and we'll have an error inside the Error field.
	return status.Error == nil
}

// PgStatReplicationList is a list of PgStatReplication reported by the primary instance
type PgStatReplicationList []PgStatReplication

// PgReplicationSlot contains the replication slots status as reported by the primary instance
type PgReplicationSlot struct {
	SlotName    string `json:"slotName,omitempty"`
	Plugin      string `json:"plugin,omitempty"`
	SlotType    string `json:"slotType,omitempty"`
	Datoid      string `json:"datoid,omitempty"`
	Database    string `json:"database,omitempty"`
	Xmin        string `json:"xmin,omitempty"`
	CatalogXmin string `json:"catalogXmin,omitempty"`
	RestartLsn  string `json:"restartLsn,omitempty"`
	WalStatus   string `json:"walStatus,omitempty"`
	SafeWalSize *int   `json:"safeWalSize,omitempty"`
	Active      bool   `json:"active,omitempty"`
}

// PgReplicationSlotList is a list of PgReplicationSlot reported by the primary instance
type PgReplicationSlotList []PgReplicationSlot

// Len implements sort.Interface extracting the length of the list
func (list PgStatReplicationList) Len() int {
	return len(list)
}

// Swap implements sort.Interface to swap elements
func (list PgStatReplicationList) Swap(i, j int) {
	list[i], list[j] = list[j], list[i]
}

// Less implements sort.Interface to determine the sort order of the replication list
// Orders by: Sync State, Working State, Sent LSN, Write LSN, ApplicationName
func (list PgStatReplicationList) Less(i, j int) bool {
	// The current sync state
	switch {
	case list[i].SyncState < list[j].SyncState:
		return true
	case list[i].SyncState > list[j].SyncState:
		return false
	}

	// The actual working state
	switch {
	case list[i].State < list[j].State:
		return true
	case list[i].State > list[j].State:
		return false
	}

	// Compare sent LSN (bigger LSN orders first)
	if list[i].SentLsn != list[j].SentLsn {
		return !list[i].SentLsn.Less(list[j].SentLsn)
	}

	// Compare write LSN (bigger LSN orders first)
	if list[i].WriteLsn != list[j].WriteLsn {
		return !list[i].WriteLsn.Less(list[j].WriteLsn)
	}

	return list[i].ApplicationName < list[j].ApplicationName
}

// PostgresqlStatusList is a list of PostgreSQL status received from the Pods
// that can be sorted considering the replication status
type PostgresqlStatusList struct {
	Items []PostgresqlStatus `json:"items"`
}

// GetNames returns a list of names of Pods
func (list *PostgresqlStatusList) GetNames() []string {
	names := make([]string, len(list.Items))
	for idx, item := range list.Items {
		names[idx] = item.Pod.Name
	}

	return names
}

// LogStatus logs the current status of the instances
func (list *PostgresqlStatusList) LogStatus(ctx context.Context) {
	contextLogger := log.FromContext(ctx)

	total := len(list.Items)
	for idx, item := range list.Items {
		message := fmt.Sprintf("pod status (%d of %d)", idx+1, total)
		contextLogger.Info(message,
			"name", item.Pod.Name,
			"currentLsn", item.CurrentLsn,
			"receivedLsn", item.ReceivedLsn,
			"replayLsn", item.ReplayLsn,
			"isPrimary", item.IsPrimary,
			"isPodReady", item.IsPodReady,
			"pendingRestart", item.PendingRestart,
			"pendingRestartForDecrease", item.PendingRestartForDecrease,
			"statusCollectionError", item.Error)
	}

	contextLogger.Debug(
		`detailed pod status`,
		"data", list,
	)
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

// ReportingMightBeUnavailable checks whether the given instance might be unavailable
func (list PostgresqlStatusList) ReportingMightBeUnavailable(instance string) bool {
	for _, item := range list.Items {
		if item.Pod.Name == instance && item.MightBeUnavailable {
			return true
		}
	}

	return false
}

// AllReadyInstancesStatusUnreachable returns true if all the
// ready instances are unreachable from the operator via HTTP request.
func (list PostgresqlStatusList) AllReadyInstancesStatusUnreachable() bool {
	hasActiveAndReady := false
	for _, item := range list.Items {
		podIsActiveAndReady := utils.IsPodActive(*item.Pod) && utils.IsPodReady(*item.Pod)

		if !podIsActiveAndReady {
			continue
		}

		hasActiveAndReady = true
		if item.Error == nil {
			return false
		}
	}

	return hasActiveAndReady
}

// InstancesReportingStatus returns the number of instances that are Ready or MightBeUnavailable
func (list PostgresqlStatusList) InstancesReportingStatus() int {
	var n int
	for _, item := range list.Items {
		if utils.IsPodActive(*item.Pod) && utils.IsPodReady(*item.Pod) || item.MightBeUnavailable {
			n++
		}
	}

	return n
}
