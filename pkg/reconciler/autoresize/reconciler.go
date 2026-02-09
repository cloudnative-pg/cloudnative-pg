/*
Copyright © contributors to CloudNativePG, established as
CloudNativePG a Series of LF Projects, LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
*/

// Package autoresize implements automatic PVC resizing for CloudNativePG clusters.
// It monitors disk usage and triggers PVC expansion when configured thresholds
// are reached, respecting rate limits and WAL safety policies.
package autoresize

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	pvcresources "github.com/cloudnative-pg/cloudnative-pg/pkg/resources"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

var autoresizeLog = log.WithName("autoresize")

const (
	// RequeueDelay is the delay after a resize operation before the next check.
	RequeueDelay = 30 * time.Second

	// MonitoringInterval is the interval for routine disk usage monitoring
	// when auto-resize is enabled but no action was taken.
	MonitoringInterval = 30 * time.Second

	// DefaultMaxActionsPerDay is the default rate limit for resize operations.
	DefaultMaxActionsPerDay = 3

	// maxAutoResizeEventHistory is the maximum number of auto-resize events to retain in status.
	// This must be sufficient to cover the rate limit budget window (24 hours × maxActionsPerDay).
	maxAutoResizeEventHistory = 50
)

// InstanceDiskInfo holds the disk status info extracted from an instance's PostgresqlStatus.
type InstanceDiskInfo struct {
	// DiskStatus contains filesystem-level disk usage statistics for all volumes.
	DiskStatus *postgres.DiskStatus
	// WALHealthStatus contains WAL archive health and replication slot information.
	WALHealthStatus *postgres.WALHealthStatus
}

// Reconcile evaluates all PVCs in the cluster for auto-resize eligibility.
// It checks triggers, rate limits, WAL safety, and calculates new sizes for eligible PVCs.
// Returns a requeue result if any PVC was resized (to allow status persistence) or if errors occurred (to retry).
// The cluster status is mutated in-place with resize events but the caller must persist it.
func Reconcile(
	ctx context.Context,
	c client.Client,
	recorder record.EventRecorder,
	cluster *apiv1.Cluster,
	diskInfoByPod map[string]*InstanceDiskInfo,
	pvcs []corev1.PersistentVolumeClaim,
) (ctrl.Result, error) {
	if !IsAutoResizeEnabled(cluster) {
		return ctrl.Result{}, nil
	}

	contextLogger := log.FromContext(ctx).WithName("autoresize")
	var resizedAny bool
	var errs []error

	for i := range pvcs {
		pvc := &pvcs[i]
		resized, err := reconcilePVC(ctx, c, recorder, cluster, diskInfoByPod, pvc)
		if err != nil {
			contextLogger.Error(err, "failed to auto-resize PVC", "pvcName", pvc.Name)
			errs = append(errs, fmt.Errorf("PVC %s: %w", pvc.Name, err))
			continue
		}
		resizedAny = resizedAny || resized
	}

	if len(errs) > 0 {
		return ctrl.Result{RequeueAfter: RequeueDelay}, errors.Join(errs...)
	}

	if resizedAny {
		return ctrl.Result{RequeueAfter: RequeueDelay}, nil
	}

	return ctrl.Result{}, nil
}

// HasBudget calculates if there is remaining budget for auto-resize operations from status history.
func HasBudget(cluster *apiv1.Cluster, pvcName string, maxActions int) bool {
	if maxActions <= 0 {
		return false
	}
	cutoff := time.Now().Add(-24 * time.Hour)
	count := 0
	for _, event := range cluster.Status.AutoResizeEvents {
		if event.PVCName == pvcName && event.Timestamp.After(cutoff) {
			count++
		}
	}
	return count < maxActions
}

func reconcilePVC(
	ctx context.Context,
	c client.Client,
	recorder record.EventRecorder,
	cluster *apiv1.Cluster,
	diskInfoByPod map[string]*InstanceDiskInfo,
	pvc *corev1.PersistentVolumeClaim,
) (bool, error) {
	contextLogger := log.FromContext(ctx).WithName("autoresize")

	pvcRole := pvc.Labels[utils.PvcRoleLabelName]
	resizeConfig := getResizeConfigForPVC(cluster, pvc)
	if resizeConfig == nil || !resizeConfig.Enabled {
		return false, nil
	}

	podName := pvc.Labels[utils.InstanceNameLabelName]
	diskInfo, ok := diskInfoByPod[podName]
	if !ok || diskInfo == nil || diskInfo.DiskStatus == nil {
		return false, nil
	}

	volumeStats := getVolumeStatsForPVC(diskInfo.DiskStatus, pvcRole, pvc)
	if volumeStats == nil {
		return false, nil
	}

	triggers := resizeConfig.Triggers
	// Use default trigger if none provided
	if triggers == nil {
		triggers = &apiv1.ResizeTriggers{}
	}

	// Trigger Check
	//nolint:gosec // G115: disk sizes won't exceed int64 limits (9.2 EB)
	if !ShouldResize(volumeStats.PercentUsed, int64(volumeStats.AvailableBytes), triggers) {
		return false, nil
	}

	// Rate limiting - derived from persisted status
	maxActions := DefaultMaxActionsPerDay
	if resizeConfig.Strategy != nil && resizeConfig.Strategy.MaxActionsPerDay != nil {
		maxActions = *resizeConfig.Strategy.MaxActionsPerDay
	}

	if !HasBudget(cluster, pvc.Name, maxActions) {
		contextLogger.Info("auto-resize blocked: rate limit exceeded", "pvcName", pvc.Name)
		recorder.Eventf(cluster, corev1.EventTypeWarning, "AutoResizeBlocked",
			"Rate limit exceeded for volume %s", pvc.Name)
		return false, nil
	}

	expansion := resizeConfig.Expansion
	if expansion == nil {
		expansion = &apiv1.ExpansionPolicy{}
	}

	currentSize := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
	if expansion.Limit != "" {
		limit, err := resource.ParseQuantity(expansion.Limit)
		if err != nil {
			return false, fmt.Errorf("invalid expansion limit %q: %w", expansion.Limit, err)
		}
		if currentSize.Cmp(limit) >= 0 {
			contextLogger.Info("auto-resize blocked: at expansion limit", "pvcName", pvc.Name)
			recorder.Eventf(cluster, corev1.EventTypeWarning, "AutoResizeAtLimit",
				"Volume %s has reached expansion limit %s", pvc.Name, expansion.Limit)
			return false, nil
		}
	}

	isSingleVolume := !cluster.ShouldCreateWalArchiveVolume()
	var walSafety *apiv1.WALSafetyPolicy
	if resizeConfig.Strategy != nil {
		walSafety = resizeConfig.Strategy.WALSafetyPolicy
	}

	walSafetyResult := EvaluateWALSafety(pvcRole, isSingleVolume, walSafety, diskInfo.WALHealthStatus)
	if walSafetyResult.Allowed && walSafetyResult.BlockReason == WALSafetyBlockHealthUnavailable {
		recorder.Eventf(cluster, corev1.EventTypeWarning, "AutoResizeWALHealthUnavailable",
			"Auto-resize permitted without WAL health verification for PVC %s", pvc.Name)
	}
	if !walSafetyResult.Allowed {
		recorder.Event(cluster, corev1.EventTypeWarning, "AutoResizeBlocked", walSafetyResult.BlockMessage)
		return false, nil
	}

	newSize, err := CalculateNewSize(currentSize, expansion)
	if err != nil {
		return false, fmt.Errorf("failed to calculate new size: %w", err)
	}

	if newSize.Cmp(currentSize) <= 0 {
		return false, nil
	}

	contextLogger.Info("auto-resizing PVC", "pvcName", pvc.Name, "from", currentSize.String(), "to", newSize.String())

	oldPVC := pvc.DeepCopy()
	updatedPVC := pvcresources.NewPersistentVolumeClaimBuilderFromPVC(pvc).
		WithRequests(corev1.ResourceList{corev1.ResourceStorage: newSize}).
		Build()

	if err := c.Patch(ctx, updatedPVC, client.MergeFrom(oldPVC)); err != nil {
		return false, fmt.Errorf("failed to patch PVC %s: %w", pvc.Name, err)
	}

	// Record standard Kubernetes Event
	recorder.Eventf(cluster, corev1.EventTypeNormal, "AutoResizeSuccess",
		"Expanded volume %s from %s to %s", pvc.Name, currentSize.String(), newSize.String())

	// Mutation for the Cluster Status (caller must persist)
	event := apiv1.AutoResizeEvent{
		Timestamp:    metav1.Now(),
		InstanceName: podName,
		PVCName:      pvc.Name,
		VolumeType:   mapPVCRoleToVolumeType(pvcRole),
		PreviousSize: currentSize.String(),
		NewSize:      newSize.String(),
		Result:       apiv1.ResizeResultSuccess,
	}
	if pvcRole == string(utils.PVCRolePgTablespace) {
		event.Tablespace = pvc.Labels[utils.TablespaceNameLabelName]
	}
	appendResizeEvent(cluster, event)

	if shouldAlertOnResize(pvcRole, isSingleVolume, walSafety) {
		recorder.Eventf(cluster, corev1.EventTypeWarning, "AutoResizeWALRisk",
			"Auto-resize occurred on WAL-related volume %s", pvc.Name)
	}

	return true, nil
}

func mapPVCRoleToVolumeType(role string) apiv1.ResizeVolumeType {
	switch role {
	case string(utils.PVCRolePgData):
		return apiv1.ResizeVolumeTypeData
	case string(utils.PVCRolePgWal):
		return apiv1.ResizeVolumeTypeWAL
	case string(utils.PVCRolePgTablespace):
		return apiv1.ResizeVolumeTypeTablespace
	default:
		return apiv1.ResizeVolumeType(role)
	}
}

// IsAutoResizeEnabled checks if auto-resize is enabled for any storage in the cluster.
func IsAutoResizeEnabled(cluster *apiv1.Cluster) bool {
	if cluster.Spec.StorageConfiguration.Resize != nil &&
		cluster.Spec.StorageConfiguration.Resize.Enabled {
		return true
	}

	if cluster.Spec.WalStorage != nil &&
		cluster.Spec.WalStorage.Resize != nil &&
		cluster.Spec.WalStorage.Resize.Enabled {
		return true
	}

	for _, tbs := range cluster.Spec.Tablespaces {
		if tbs.Storage.Resize != nil && tbs.Storage.Resize.Enabled {
			return true
		}
	}

	return false
}

// getResizeConfigForPVC returns the resize configuration for the given PVC.
func getResizeConfigForPVC(cluster *apiv1.Cluster, pvc *corev1.PersistentVolumeClaim) *apiv1.ResizeConfiguration {
	pvcRole := pvc.Labels[utils.PvcRoleLabelName]

	switch pvcRole {
	case string(utils.PVCRolePgData):
		return cluster.Spec.StorageConfiguration.Resize
	case string(utils.PVCRolePgWal):
		if cluster.Spec.WalStorage != nil {
			return cluster.Spec.WalStorage.Resize
		}
	case string(utils.PVCRolePgTablespace):
		tbsName := pvc.Labels[utils.TablespaceNameLabelName]
		for _, tbs := range cluster.Spec.Tablespaces {
			if tbs.Name == tbsName {
				return tbs.Storage.Resize
			}
		}
	}

	return nil
}

// getVolumeStatsForPVC returns the volume stats for the given PVC.
func getVolumeStatsForPVC(
	diskStatus *postgres.DiskStatus,
	pvcRole string,
	pvc *corev1.PersistentVolumeClaim,
) *postgres.VolumeStatus {
	switch pvcRole {
	case string(utils.PVCRolePgData):
		return diskStatus.DataVolume
	case string(utils.PVCRolePgWal):
		return diskStatus.WALVolume
	case string(utils.PVCRolePgTablespace):
		tbsName := pvc.Labels[utils.TablespaceNameLabelName]
		if diskStatus.Tablespaces != nil {
			return diskStatus.Tablespaces[tbsName]
		}
	}

	return nil
}

// appendResizeEvent appends a resize event to the cluster status.
// Events older than 25 hours are pruned (budget window is 24h + 1h buffer).
// Additionally, the history is capped at maxAutoResizeEventHistory entries.
func appendResizeEvent(cluster *apiv1.Cluster, event apiv1.AutoResizeEvent) {
	cutoff := time.Now().Add(-25 * time.Hour)
	retained := make([]apiv1.AutoResizeEvent, 0, len(cluster.Status.AutoResizeEvents)+1)
	for _, existing := range cluster.Status.AutoResizeEvents {
		if existing.Timestamp.IsZero() {
			continue
		}
		if existing.Timestamp.After(cutoff) {
			retained = append(retained, existing)
		}
	}
	retained = append(retained, event)

	// Apply hard cap to prevent unbounded growth
	if len(retained) > maxAutoResizeEventHistory {
		retained = retained[len(retained)-maxAutoResizeEventHistory:]
	}
	cluster.Status.AutoResizeEvents = retained
}

func shouldAlertOnResize(pvcRole string, isSingleVolume bool, walSafety *apiv1.WALSafetyPolicy) bool {
	if pvcRole != string(utils.PVCRolePgWal) &&
		(pvcRole != string(utils.PVCRolePgData) || !isSingleVolume) {
		return false
	}

	if walSafety == nil || walSafety.AlertOnResize == nil {
		return true
	}

	return *walSafety.AlertOnResize
}
