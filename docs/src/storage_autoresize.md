---
id: storage_autoresize
sidebar_position: 285
title: Automatic PVC Resizing
---

# Automatic PVC Resizing
<!-- SPDX-License-Identifier: CC-BY-4.0 -->

CloudNativePG supports automatic PVC resizing for data, WAL, and tablespace
volumes. This feature monitors disk usage from within PostgreSQL pods and
automatically expands PVCs when configurable thresholds are exceeded.

## Overview

Database storage requirements are difficult to predict. Databases grow over
time, and running out of disk space causes PostgreSQL crashes, WAL archiving
failures, replication breakage, and service outages. Automatic PVC resizing
addresses this by proactively expanding storage before critical thresholds
are reached.

The feature uses filesystem `statfs()` syscalls to accurately measure disk
usage, ensuring measurements reflect actual usage rather than Kubernetes
PVC status metadata. This is particularly important for volumes where the
filesystem size may differ from the PVC specification.

:::info[Prerequisites]
Automatic PVC resizing requires a storage class that supports volume expansion.
The storage class must have `allowVolumeExpansion: true` set. Most cloud
provider storage classes (AWS EBS, GCP PD, Azure Disk) support this feature.
:::

## Configuration

Auto-resize is configured per storage volume through the `resize` field in
the storage configuration. This can be set on:

- `spec.storage.resize` for data volumes
- `spec.walStorage.resize` for WAL volumes
- `spec.tablespaces[*].storage.resize` for tablespace volumes

### Minimal Configuration

The simplest configuration enables auto-resize with default settings:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: my-cluster
spec:
  instances: 3
  storage:
    size: 10Gi
    resize:
      enabled: true
      strategy:
        walSafetyPolicy:
          acknowledgeWALRisk: true
```

:::warning
For single-volume clusters (no separate WAL storage), you must set
`acknowledgeWALRisk: true`. This acknowledges that auto-resize without
WAL safety checks may mask archive failures. See [WAL Safety](#wal-safety)
for details.
:::

### Single-Volume Cluster with Safety Settings

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: production-cluster
spec:
  instances: 3
  storage:
    size: 100Gi
    resize:
      enabled: true
      triggers:
        usageThreshold: 80
        minAvailable: 10Gi
      expansion:
        step: "10%"
        minStep: 5Gi
        maxStep: 50Gi
        limit: 500Gi
      strategy:
        maxActionsPerDay: 5
        walSafetyPolicy:
          acknowledgeWALRisk: true
          requireArchiveHealthy: true
          maxPendingWALFiles: 100
```

### Multi-Volume Cluster (Data + WAL)

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: production-cluster
spec:
  instances: 3
  storage:
    size: 100Gi
    resize:
      enabled: true
      triggers:
        usageThreshold: 80
      expansion:
        step: "20%"
        limit: 500Gi
      strategy:
        maxActionsPerDay: 5
  walStorage:
    size: 20Gi
    resize:
      enabled: true
      triggers:
        usageThreshold: 75
      expansion:
        step: "25%"
        limit: 100Gi
      strategy:
        maxActionsPerDay: 5
        walSafetyPolicy:
          requireArchiveHealthy: true
          maxPendingWALFiles: 50
```

### Cluster with Tablespace Resize

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-with-tablespaces
spec:
  instances: 3
  storage:
    size: 50Gi
    resize:
      enabled: true
      strategy:
        walSafetyPolicy:
          acknowledgeWALRisk: true
  tablespaces:
    - name: archive_data
      storage:
        size: 100Gi
        resize:
          enabled: true
          triggers:
            usageThreshold: 85
          expansion:
            step: "15%"
            limit: 1Ti
          strategy:
            maxActionsPerDay: 3
```

## Trigger Behavior

Auto-resize triggers when **either** of these conditions is met:

| Trigger | Description | Default |
|---------|-------------|---------|
| `usageThreshold` | Percentage of disk used | 80% |
| `minAvailable` | Minimum free space required | Not set |

When both are configured, resize triggers if usage exceeds the threshold
**or** available space drops below `minAvailable`.

Example: With `usageThreshold: 80` and `minAvailable: 10Gi` on a 100Gi volume,
resize triggers when either 80Gi is used (80% threshold) or less than 10Gi
is free.

## Expansion Clamping

The expansion policy controls how much the volume grows on each resize:

| Field | Description | Default |
|-------|-------------|---------|
| `step` | Amount to grow (percentage like `20%` or absolute like `10Gi`) | 20% |
| `minStep` | Minimum growth per resize | 2Gi |
| `maxStep` | Maximum growth per resize | 500Gi |
| `limit` | Maximum total volume size | Not set |

Notes:
- `step` must be a string quantity or percentage (e.g., `"10Gi"` or `"20%"`). Integer values are rejected.
- `minStep` and `maxStep` only apply to percentage-based steps.

### Clamping Logic

The expansion amount is calculated as:

1. Calculate raw step: `currentSize * step%` or absolute step value
2. Apply minStep: `max(rawStep, minStep)`
3. Apply maxStep: `min(result, maxStep)`
4. Apply limit: `min(currentSize + result, limit) - currentSize`

Example: For a 100Gi volume with `step: 50%`, `minStep: 5Gi`, `maxStep: 20Gi`:
- Raw step: 50Gi
- After maxStep: 20Gi (clamped to maximum)
- New size: 120Gi

## Rate Limiting

Cloud providers impose limits on volume modifications. For example, AWS EBS
allows only one modification every 6 hours. The `maxActionsPerDay` setting
prevents exhausting these quotas:

```yaml
strategy:
  maxActionsPerDay: 4  # One resize per 6 hours
```

When the daily budget is exhausted, resize requests are blocked until the
budget resets (rolling 24-hour window). The `cnpg_disk_resize_budget_remaining`
metric tracks the remaining budget.

Setting `maxActionsPerDay: 0` disables auto-resize for that volume.

## WAL Safety

WAL (Write-Ahead Log) safety policies prevent auto-resize from masking
underlying issues that cause WAL accumulation. Without these checks,
auto-resize might continuously grow storage while the real problem
(archive failure, stuck replication slot) goes unaddressed.

### Why WAL Safety Matters

PostgreSQL retains WAL files until they are:
1. Archived (if archiving is configured)
2. Consumed by all replication slots
3. No longer needed for recovery

If archiving fails or a replication slot becomes inactive, WAL accumulates
indefinitely. Auto-resizing the volume delays the inevitable disk exhaustion
while masking the root cause.

### acknowledgeWALRisk

For single-volume clusters (data and WAL on the same volume), the
`acknowledgeWALRisk` field must be set to `true`:

```yaml
strategy:
  walSafetyPolicy:
    acknowledgeWALRisk: true
```

This acknowledges that auto-resize on a single volume cannot distinguish
between data growth (safe to expand) and WAL growth from archive failure
(should be investigated).

### requireArchiveHealthy

When `requireArchiveHealthy: true`, auto-resize is blocked if WAL archiving
is failing:

```yaml
strategy:
  walSafetyPolicy:
    requireArchiveHealthy: true
    maxPendingWALFiles: 100
```

The `maxPendingWALFiles` threshold (default: 100) determines how many
un-archived WAL files are acceptable before considering archiving unhealthy.

### maxSlotRetentionBytes

Prevents resize when inactive replication slots are retaining excessive WAL:

```yaml
strategy:
  walSafetyPolicy:
    maxSlotRetentionBytes: 1073741824  # 1Gi
```

If any replication slot is retaining more than the specified bytes, resize
is blocked. This forces investigation of stuck or abandoned replication slots.

## Monitoring

### Key Metrics

| Metric | Description |
|--------|-------------|
| `cnpg_disk_total_bytes` | Total volume size in bytes |
| `cnpg_disk_used_bytes` | Used space in bytes |
| `cnpg_disk_available_bytes` | Available space in bytes |
| `cnpg_disk_percent_used` | Usage percentage (0-100) |
| `cnpg_disk_at_limit` | 1 if volume is at expansion limit |
| `cnpg_disk_resize_blocked` | 1 if resize is blocked by WAL safety |
| `cnpg_disk_resize_budget_remaining` | Remaining daily resize budget |
| `cnpg_disk_resizes_total` | Counter of resize operations |
| `cnpg_wal_archive_healthy` | 1 if archiving is healthy |
| `cnpg_wal_pending_archive_files` | Number of un-archived WAL files |
| `cnpg_wal_inactive_slots` | Number of inactive replication slots |

### PrometheusRule Alerts

CloudNativePG provides sample PrometheusRule alerts in
`docs/src/samples/monitoring/prometheusrule.yaml`. Key disk-related alerts:

| Alert | Severity | Description |
|-------|----------|-------------|
| `CNPGDiskCritical` | critical | Disk usage above 95% |
| `CNPGDiskWarning` | warning | Disk usage above 80% |
| `CNPGAutoResizeBlocked` | warning | Resize blocked by WAL safety |
| `CNPGAtLimit` | warning | At expansion limit with high usage |
| `CNPGArchiveUnhealthy` | warning | WAL archiving failing |
| `CNPGResizeBudgetExhausted` | warning | Daily resize budget depleted |

## Troubleshooting

### Resize Not Triggering

1. **Check threshold**: Verify disk usage exceeds `usageThreshold` or
   available space is below `minAvailable`
2. **Check budget**: Verify `cnpg_disk_resize_budget_remaining > 0`
3. **Check WAL safety**: If `cnpg_disk_resize_blocked == 1`, investigate
   archive health or replication slot issues
4. **Check limit**: If `cnpg_disk_at_limit == 1`, the volume has reached
   its maximum configured size

### Resize Blocked

Check the cluster events for details:

```bash
kubectl describe cluster my-cluster
```

Look for events with reason containing "ResizeBlocked". Common causes:

- Archive is unhealthy (`requireArchiveHealthy: true`)
- Replication slot retaining too much WAL (`maxSlotRetentionBytes`)
- Daily resize budget exhausted

### CSI Driver Requirements

Verify your storage class supports volume expansion:

```bash
kubectl get storageclass <storage-class-name> -o yaml | grep allowVolumeExpansion
```

If `allowVolumeExpansion` is not `true`, you cannot use auto-resize with
that storage class.

## Reclaiming Disk Space

Auto-resize only grows volumes; it cannot shrink them. Kubernetes does not
support PVC shrinking. To reclaim space:

1. **Backup and restore**: Create a new cluster from backup with smaller
   initial storage
2. **Logical export**: Use `pg_dump`/`pg_restore` to migrate to a new cluster
3. **Logical replication**: Set up logical replication to a new cluster with
   smaller storage

## Known Limitations

- **No shrinking**: PVCs can only grow, not shrink
- **Cluster spec not updated**: After auto-resize, `spec.storage.size` (and
  `spec.walStorage.size`) retain their original values. The PVC is larger than
  what the spec declares. This means new replicas added after a resize start
  with the original spec size and are auto-resized on their next probe cycle.
  This is intentional: the operator does not modify the user's declarative spec,
  which preserves GitOps compatibility and avoids permanently ratcheting up the
  size floor (which would prevent future PVC shrink workflows).
  This also affects volume snapshot restores: a snapshot taken from a resized
  PVC (e.g. 15Gi) may fail or behave unexpectedly when restored into a new
  PVC whose spec declares the original smaller size (e.g. 10Gi), depending
  on the CSI driver
- **Directory-based provisioners**: Local-path-provisioner and similar
  directory-based storage do not support volume expansion
- **Offline resize**: Some CSI drivers require pod restart for resize;
  CloudNativePG handles this automatically but it may cause brief interruptions
- **Cloud provider limits**: Volume modification rate limits vary by provider
  (e.g., AWS EBS: one modification per 6 hours)
