# RFC: Automatic PVC Resizing with WAL-Aware Safety for CloudNativePG

| Field | Value |
|-------|-------|
| **Author** | Jeff Mealo |
| **Status** | Draft / Request for Comments |
| **Created** | 2026-02-06 |
| **Target Release** | TBD |
| **Companion Issue** | [Feature Request: Automatic PVC Resizing with WAL-Aware Safety Checks](https://github.com/cloudnative-pg/cloudnative-pg/issues/9928) |

---

## Summary

This RFC proposes adding native automatic PVC resizing to CloudNativePG. The feature monitors disk usage from within PostgreSQL pods using filesystem `statfs()` syscalls, exposes usage metrics via Prometheus, and automatically expands PVCs when configurable thresholds are exceeded. Central to the design is **WAL-aware safety logic** that prevents auto-resize from masking underlying issues like archive failures or replication lag. These conditions, left unaddressed, lead to data loss.

The configuration uses a **behavior-driven model** inspired by the Kubernetes HorizontalPodAutoscaler v2 scaling behaviors. Rather than treating resize as a static value, expansion is defined as a dynamic behavior constrained by clamping logic and rate-limited by cloud provider realities. This design evolved from an earlier, simpler proposal that proved insufficient to handle the full range of volume scales safely.¹

This document covers the full design in four sections: motivation and community context, architecture and data flow, API surface and implementation details, and the testing and rollout strategy.

---

## Motivation

### The Problem

PostgreSQL storage requirements are difficult to predict. Databases grow over time, and running out of disk space causes PostgreSQL crashes or read-only mode, WAL archiving failures, replication breakage, and service outages. Today, CNPG supports manual PVC resizing by updating `spec.storage.size`, but this requires monitoring disk usage externally, manual intervention to update the Cluster resource, and carries the risk of human error or delayed response.

The impact is real and recurring across the CNPG community. Issue #9927 reports clusters that enter unrecoverable states when disk fills. Issue #1808 describes a deadlock where the operator refuses to create the primary instance because disk is full, but can't expand the disk because there's no running instance. Issue #9301 found a circular dependency where "you can't increase storage because CNPG won't operate until you increase storage."

WAL volumes are especially dangerous. Issue #6152 reports a WAL PVC that won't grow even after manual intervention because WAL accumulates faster than it can be archived. Issue #8791 highlights documentation gaps for dealing with WAL disk exhaustion. Issue #7827 found replicas that continue reporting healthy status even after I/O errors from storage exhaustion.

### Why Not Use Existing External Solutions?

External solutions like [topolvm/pvc-autoresizer](https://github.com/topolvm/pvc-autoresizer) exist but have critical limitations for PostgreSQL workloads:

- **No PostgreSQL awareness**: cannot distinguish WAL growth from data growth
- **No archive health checks**: may mask archive failures by blindly growing storage
- **No replication slot awareness**: may mask stuck replication slots retaining WAL
- **Requires Prometheus**: adds an infrastructure dependency
- **Generic PVC annotations**: doesn't integrate with the Cluster CRD

Issue #7100 specifically requested per-PVC label and annotation support to enable TopoLVM integration, and issue #9385 directly requested native storage autoscaling support. PR #7064 (still open as a draft) proposed automatically switching clusters to read-only on high disk usage, a complementary but incomplete approach.

---

## Community Context: Related Issues

The following open and closed issues informed this design:

### Open Issues Directly Addressed

| Issue | Title | How This RFC Addresses It |
|-------|-------|--------------------------|
| [#9927](https://github.com/cloudnative-pg/cloudnative-pg/issues/9927) | Improve handling disk full scenario | Auto-resize prevents disk-full by expanding PVCs proactively |
| [#9885](https://github.com/cloudnative-pg/cloudnative-pg/issues/9885) | Operator stuck in reconciliation loop | Reduces storage-pressure-induced reconciliation failures |
| [#9786](https://github.com/cloudnative-pg/cloudnative-pg/issues/9786) | Invalid PATCH operation with storage and resource resize | Auto-resize handles PVC patching independently of pod spec changes |
| [#9447](https://github.com/cloudnative-pg/cloudnative-pg/issues/9447) | WAL disk space check fails due to node ephemeral storage | `statfs()` on the actual mount point gives accurate results |
| [#9385](https://github.com/cloudnative-pg/cloudnative-pg/issues/9385) | Storage Autoscaling Support | Direct implementation of this feature request |
| [#8791](https://github.com/cloudnative-pg/cloudnative-pg/issues/8791) | WAL disk running out and dealing with it | WAL-aware safety + metrics + alerts for WAL exhaustion |
| [#7997](https://github.com/cloudnative-pg/cloudnative-pg/issues/7997) | Pod creation stuck during PVC resize | Auto-resize expands PVCs before they reach critical thresholds |

### Closed Issues That Informed Design Decisions

| Issue | Title | Design Lesson |
|-------|-------|--------------|
| [#9301](https://github.com/cloudnative-pg/cloudnative-pg/issues/9301) | Can't increase storage because CNPG won't operate | Proactive resize avoids this circular dependency entirely |
| [#8369](https://github.com/cloudnative-pg/cloudnative-pg/issues/8369) | Increase storage above EBS size limit, unrecoverable state | `limit` field prevents exceeding CSI/platform limits |
| [#7827](https://github.com/cloudnative-pg/cloudnative-pg/issues/7827) | Replica shows healthy after I/O error from storage exhaustion | Disk metrics enable early detection before I/O errors |
| [#7505](https://github.com/cloudnative-pg/cloudnative-pg/issues/7505) | Primary node pod deleted during disk space increase | Online resize via CSI avoids pod disruption |
| [#7324](https://github.com/cloudnative-pg/cloudnative-pg/issues/7324) | PVC resize on Azure not properly detected | CSI failure detection via `statfs()` vs. PVC spec comparison |
| [#6152](https://github.com/cloudnative-pg/cloudnative-pg/issues/6152) | walStorage PVC will not grow | WAL safety checks block resize when archive lag is the root cause |
| [#5083](https://github.com/cloudnative-pg/cloudnative-pg/issues/5083) | Handling PVC volume shrink | Shrinking is a non-goal; K8s doesn't support it |
| [#4521](https://github.com/cloudnative-pg/cloudnative-pg/issues/4521) | Graceful handling of WAL disk space exhaustion | Complements existing fencing with proactive expansion |
| [#1808](https://github.com/cloudnative-pg/cloudnative-pg/issues/1808) | Out of disk space, refusing to create primary instance | Proactive resize prevents reaching this deadlock state |

### Related Pull Request

| PR | Title | Status | Relation |
|----|-------|--------|----------|
| [#7064](https://github.com/cloudnative-pg/cloudnative-pg/pull/7064) | Auto switch cluster to read-only on high disk usage | Open (Draft) | Complementary: read-only mode is a reactive measure; auto-resize is proactive |

---

## Goals and Non-Goals

### Goals

1. **Automatic PVC expansion** for data, WAL, and tablespace volumes
2. **Accurate disk metrics** via filesystem `statfs()` syscalls (not K8s PVC status or SQL)
3. **WAL-aware safety** that blocks resize when archive/replication is unhealthy
4. **Single-volume safety** with explicit risk acknowledgment
5. **Prometheus metrics** for disk usage, capacity, and free space
6. **PrometheusRule alerts** for disk pressure and blocked resizes
7. **Grafana dashboard updates** to visualize disk and WAL health
8. **CSI failure detection** by comparing actual vs. requested size
9. **Cloud provider rate-limit awareness** to prevent exhausting volume modification quotas

### Non-Goals

1. **PVC shrinking**: Kubernetes does not support this (see **Reclaiming Disk Space** below for documented alternatives)
2. **Automatic cleanup**: The operator will not delete data to free space
3. **Cross-cluster coordination**: Each cluster manages its own storage independently
4. **Non-CSI storage**: Requires a CSI driver with volume expansion support
5. **Barman plugin metrics overhaul**: Separate effort, though related
6. **Growth-rate prediction**: Predicting future disk usage based on historical growth trends is deferred as a potential future enhancement

### Reclaiming Disk Space (PVC Shrinking Alternatives)

Kubernetes does not support shrinking PVCs. Once a volume is expanded, it cannot be made smaller. This is a platform limitation, not a CNPG limitation. However, operators still need a path to reclaim over-provisioned storage. The following approaches are available today and should be documented alongside this feature:

1. **Restore from backup to a smaller cluster.** Create a new Cluster resource with a smaller `spec.storage.size` and restore from an existing backup. This is the simplest approach and preserves PITR capability. CNPG's recovery-from-backup workflow handles this natively.

2. **Logical export and re-import.** Use `pg_dump` / `pg_restore` (or `pg_dumpall`) to export data from the existing cluster and import into a new cluster with smaller storage. This works for any PostgreSQL version but does not preserve physical backup history.

3. **Logical replication.** Set up logical replication from the over-provisioned cluster to a new cluster with smaller storage, then switch over. This approach minimizes downtime but requires PostgreSQL 10+ and may not replicate all object types (sequences, large objects, DDL).

Documentation for these approaches should ship alongside the auto-resize feature so that operators have a clear path for both growth and reclamation.

---

## Background: Current CNPG Storage Architecture

### Three PVC Types

CNPG manages three types of PVCs per instance:

| PVC Type | Naming Pattern | Mount Path | Purpose |
|----------|----------------|------------|---------|
| Data (PGDATA) | `{cluster}-{n}` | `/var/lib/postgresql/data` | PostgreSQL data directory |
| WAL | `{cluster}-{n}-wal` | `/var/lib/postgresql/wal` | Separate WAL storage (optional) |
| Tablespace | `{cluster}-{n}-tbs-{name}` | `/var/lib/postgresql/tablespaces/{name}` | Custom tablespaces |

### Current Manual Resize Flow

```
User updates spec.storage.size
         ↓
Operator detects change (reconciler)
         ↓
Operator patches PVC spec.resources.requests.storage
         ↓
CSI driver expands volume
         ↓
Kubelet resizes filesystem (online or offline)
         ↓
PostgreSQL sees more space
```

This is implemented in `pkg/reconciler/persistentvolumeclaim/existing.go` via `reconcilePVCQuantity()`, which compares the requested size against the actual PVC spec and only allows increases. The `ShouldResizeInUseVolumes()` function on the Cluster type (defaulting to `true`) controls whether online resize is attempted.

### Existing Disk Space Infrastructure

CNPG already has a disk probe mechanism in `pkg/management/postgres/instance.go`:

```go
func (instance *Instance) CheckHasDiskSpaceForWAL(ctx context.Context) (bool, error) {
    // Uses statfs() via DiskProbe from machinery package
    walDirectory := path.Join(instance.PgData, pgWalDirectory)
    return fileutils.NewDiskProbe(walDirectory).HasStorageAvailable(ctx, walSegmentSize)
}
```

This infrastructure exists but is only used at startup and isn't exposed as metrics.

### Why `statfs()` Over SQL or K8s PVC Status

**SQL functions are insufficient:** `pg_database_size()` returns logical data size but has no knowledge of volume capacity. `pg_tablespace_size()` provides no free space info. `pg_stat_wal` provides WAL stats but no volume capacity. PostgreSQL simply does not know how much space is available on the underlying volume.

**K8s PVC status is insufficient:** The PVC `status.capacity` shows what Kubernetes *believes* the size is, but CSI expansion failures can cause the spec to show 150Gi while the actual volume remains 100Gi. PVC status also provides no usage information, only allocated capacity.

**`statfs()` from inside the container is the most accurate source available:** it reports actual capacity, actual used space, and actual available space from the mounted filesystem. Note that `statfs()` operates at the filesystem level, so its accuracy depends on the CSI driver providing an isolated filesystem per PVC (see the **Known Limitations** section below).

---

## Design Overview

### Architecture

```
+---------------------------------------------------------------------+
| Instance Pod                                                        |
|                                                                     |
|  +---------------------------------------------------------------+  |
|  | Instance Manager                                              |  |
|  |                                                               |  |
|  |  +--------------+   +--------------+   +------------------+   |  |
|  |  | DiskProbe    |   | WAL Health   |   | Metrics Exporter |   |  |
|  |  | (statfs)     |-->| Checker      |-->| :9187            |   |  |
|  |  |              |   |              |   |                  |   |  |
|  |  | - PGDATA     |   | - Archive OK |   | Exports:         |   |  |
|  |  | - WAL        |   | - Slots OK   |   | - capacity       |   |  |
|  |  | - Tablespace |   | - Pending #  |   | - used           |   |  |
|  |  +--------------+   +--------------+   | - available      |   |  |
|  |        |                  |            | - pct_used       |   |  |
|  |        v                  v            +--------+---------+   |  |
|  |  +-----------------------------------+          |             |  |
|  |  | Status Endpoint :8000/pg/status   |          |             |  |
|  |  | (includes disk + WAL health)      |----+     |             |  |
|  |  +-----------------------------------+    |     |             |  |
|  +-------------------------------------------|-----|-------------+  |
|                                              |     |                |
|  +----------+  +----------+  +----------+    |     v                |
|  | PGDATA   |  | WAL      |  | TBS      |    |  Prometheus          |
|  | PVC      |  | PVC      |  | PVCs     |    |  (scrapes)           |
|  +----------+  +----------+  +----------+    |                      |
+----------------------------------------------|----------------------+
                                               |
                                               v
+----------------------------------------------------------------------+
| Operator                                                             |
|                                                                      |
|  +----------------------------------------------------------------+  |
|  | Cluster Controller                                             |  |
|  |                                                                |  |
|  |  +--------------+   +--------------+   +-----------------+     |  |
|  |  | Fetch Disk   |   | Evaluate     |   | Resize PVC      |     |  |
|  |  | Status from  |-->| Policy &     |-->| if threshold    |     |  |
|  |  | Instances    |   | WAL Health   |   | exceeded        |     |  |
|  |  +--------------+   +--------------+   +-----------------+     |  |
|  |                            |                  |                |  |
|  |                            v                  v                |  |
|  |                     Block resize if     Patch PVC spec         |  |
|  |                     WAL unhealthy       + record event         |  |
|  +----------------------------------------------------------------+  |
+----------------------------------------------------------------------+
```

### Data Flow

1. **Instance Manager** periodically calls `statfs()` on each mount point (PGDATA, WAL, tablespaces)
2. **Metrics Exporter** exposes capacity/used/available as Prometheus metrics on `:9187`
3. **Status Endpoint** includes disk status and WAL health in the `/pg/status` response
4. **Operator** fetches disk status from all instances during reconciliation
5. **Operator** evaluates resize policy: trigger check, rate-limit budget check, expansion limit check, WAL health check
6. **Operator** patches PVC `spec.resources.requests.storage` if resize is needed and safe

---

## API Changes

### Design Evolution

An earlier draft of this RFC used a flat `AutoResizeConfiguration` struct with fields like `usageThreshold`, `increase`, `minIncrease`, `maxIncrease`, `maxSize`, and `cooldownPeriod`. During community review, two scaling problems were identified that this simpler model failed to address:

1. **The "Petabyte Problem"**: a 20% resize on a 10TB volume adds 2TB. This can trigger cloud provider timeouts or extended "Optimizing" states that lock the volume for hours.

2. **The "Thundering Herd"**: a 20% resize on a 1GB volume adds only 200MB. This wastes scarce API quotas (e.g., AWS EBS limits volumes to ~4 modifications per day) on trivial growth.

3. **The "Robot Trap"**: a time-based cooldown (e.g., 1 hour) doesn't account for cloud provider rate limits. An operator that burns all 4 daily EBS modification slots leaves no room for manual human intervention during an emergency.

These problems led to a redesign using a **behavior-driven configuration model**, inspired by the Kubernetes HorizontalPodAutoscaler v2 scaling behaviors. The configuration now separates concerns into three blocks: **triggers** (when to act), **expansion** (how to grow, with clamping), and **strategy** (rate limiting and safety).

### New `ResizeConfiguration` in `StorageConfiguration`

```go
// api/v1/cluster_types.go

type StorageConfiguration struct {
    // Existing fields (unchanged)
    StorageClass                  *string                           `json:"storageClass,omitempty"`
    Size                          string                            `json:"size,omitempty"`
    ResizeInUseVolumes            *bool                             `json:"resizeInUseVolumes,omitempty"`
    PersistentVolumeClaimTemplate *corev1.PersistentVolumeClaimSpec `json:"pvcTemplate,omitempty"`

    // NEW: Resize configuration
    // +optional
    Resize *ResizeConfiguration `json:"resize,omitempty"`
}
```

### `ResizeConfiguration`

```go
type ResizeConfiguration struct {
    // Enabled activates automatic PVC resizing for this volume type
    // +kubebuilder:default:=false
    Enabled bool `json:"enabled"`

    // Triggers defines the conditions that initiate a resize operation.
    // +optional
    Triggers *ResizeTriggers `json:"triggers,omitempty"`

    // Expansion defines how much to grow the PVC when resizing, including
    // clamping logic to bound the step size across different volume scales.
    // +optional
    Expansion *ExpansionPolicy `json:"expansion,omitempty"`

    // Strategy controls the operational behavior of resize operations,
    // including rate limiting and WAL safety checks.
    // +optional
    Strategy *ResizeStrategy `json:"strategy,omitempty"`
}
```

### `ResizeTriggers`

```go
type ResizeTriggers struct {
    // UsageThreshold is the disk usage percentage that triggers a resize (1-99).
    // Resize fires when used space exceeds this percentage of total capacity.
    // When both UsageThreshold and MinAvailable are set, resize triggers when
    // EITHER condition is met (whichever fires first).
    // +kubebuilder:validation:Minimum=1
    // +kubebuilder:validation:Maximum=99
    // +kubebuilder:default:=80
    UsageThreshold int `json:"usageThreshold,omitempty"`

    // MinAvailable triggers a resize when available space drops below this
    // absolute value, regardless of the percentage threshold.
    // Addresses the scaling problem where a percentage alone is too late for
    // small volumes (80% of 1Gi = only 200Mi free) or too aggressive for
    // large volumes (80% of 1Ti = 200Gi free).
    // +optional
    MinAvailable string `json:"minAvailable,omitempty"`
}
```

### `ExpansionPolicy`

```go
type ExpansionPolicy struct {
    // Step specifies how much to grow the PVC when resizing.
    // Accepts either a percentage (e.g., "20%") for exponential growth or an
    // absolute value (e.g., "10Gi") for linear growth.
    // Uses the Kubernetes IntOrString pattern (like maxUnavailable in Deployments).
    // +kubebuilder:default:="20%"
    Step intstr.IntOrString `json:"step,omitempty"`

    // MinStep sets a floor on the resize step when using percentage-based Step.
    // Prevents micro-resizes that waste scarce cloud provider API quotas
    // (e.g., 20% of 1Gi = 200Mi is too small to justify a modification slot).
    // Ignored when Step is an absolute value.
    // +kubebuilder:default:="2Gi"
    // +optional
    MinStep string `json:"minStep,omitempty"`

    // MaxStep sets a ceiling on the resize step when using percentage-based Step.
    // Prevents timeout-inducing massive resizes on large volumes
    // (e.g., 20% of 10Ti = 2Ti can trigger extended cloud provider "Optimizing"
    // states that lock the volume for hours).
    // Ignored when Step is an absolute value.
    // +kubebuilder:default:="500Gi"
    // +optional
    MaxStep string `json:"maxStep,omitempty"`

    // Limit is the maximum size the PVC can grow to.
    // Prevents runaway growth; resize stops when this limit is reached.
    // +optional
    Limit string `json:"limit,omitempty"`
}
```

### `ResizeStrategy`

```go
type ResizeStrategy struct {
    // Mode selects the resize strategy.
    // "Standard" uses reactive threshold-based triggers.
    // Future modes may include predictive scaling based on growth rate.
    // +kubebuilder:default:="Standard"
    // +kubebuilder:validation:Enum=Standard
    Mode ResizeMode `json:"mode,omitempty"`

    // MaxActionsPerDay limits the number of resize operations per 24-hour
    // rolling window. Cloud providers (e.g., AWS EBS) often limit volume
    // modifications to ~4 per day. Defaulting to 3 reserves one slot for
    // manual human intervention during emergencies.
    // Set to 0 to disable rate limiting (not recommended for production).
    // +kubebuilder:default:=3
    // +kubebuilder:validation:Minimum=0
    // +kubebuilder:validation:Maximum=10
    MaxActionsPerDay int `json:"maxActionsPerDay,omitempty"`

    // WALSafetyPolicy controls WAL-related safety checks before allowing resize.
    // Required for single-volume clusters; optional but recommended for all.
    // +optional
    WALSafetyPolicy *WALSafetyPolicy `json:"walSafetyPolicy,omitempty"`
}

type ResizeMode string

const (
    ResizeModeStandard ResizeMode = "Standard"
)
```

### `WALSafetyPolicy`

```go
type WALSafetyPolicy struct {
    // AcknowledgeWALRisk must be true for single-volume clusters (no separate walStorage).
    // Acknowledges that WAL growth from archive or replication failures may trigger
    // unnecessary resizes on the data volume.
    // +optional
    AcknowledgeWALRisk bool `json:"acknowledgeWALRisk,omitempty"`

    // RequireArchiveHealthy blocks resize if WAL archiving is failing.
    // Prevents masking archive failures by growing storage.
    // +kubebuilder:default:=true
    RequireArchiveHealthy *bool `json:"requireArchiveHealthy,omitempty"`

    // MaxPendingWALFiles blocks resize if more than this many files await archiving.
    // Set to 0 to disable this check.
    // +kubebuilder:default:=100
    MaxPendingWALFiles *int `json:"maxPendingWALFiles,omitempty"`

    // MaxSlotRetentionBytes blocks resize if inactive replication slots retain
    // more than this many bytes of WAL. Set to 0 to disable.
    // +optional
    MaxSlotRetentionBytes *int64 `json:"maxSlotRetentionBytes,omitempty"`

    // AlertOnResize generates a warning event when resize occurs for WAL-related reasons.
    // +kubebuilder:default:=true
    AlertOnResize *bool `json:"alertOnResize,omitempty"`
}
```

### Cluster Status Additions

```go
type ClusterStatus struct {
    // Existing fields...

    // DiskStatus reports disk usage for all instances
    // +optional
    DiskStatus *ClusterDiskStatus `json:"diskStatus,omitempty"`

    // AutoResizeEvents contains the history of auto-resize operations.
    // Used by the stateless rate limiter (HasBudget) for budget calculation.
    // Capped at maxAutoResizeEventHistory (50) entries.
    // +optional
    AutoResizeEvents []AutoResizeEvent `json:"autoResizeEvents,omitempty"`
}

type ClusterDiskStatus struct {
    // Instances contains per-instance disk status, keyed by instance name.
    // +optional
    Instances map[string]*InstanceDiskStatus `json:"instances,omitempty"`
}

type InstanceDiskStatus struct {
    // DataVolume contains disk stats for the PGDATA volume.
    // +optional
    DataVolume *VolumeDiskStatus `json:"dataVolume,omitempty"`

    // WALVolume contains disk stats for the WAL volume (if separate from PGDATA).
    // +optional
    WALVolume *VolumeDiskStatus `json:"walVolume,omitempty"`

    // Tablespaces contains disk stats for tablespace volumes.
    // +optional
    Tablespaces map[string]*VolumeDiskStatus `json:"tablespaces,omitempty"`

    // WALHealth contains the WAL archive health status.
    // +optional
    WALHealth *WALHealthInfo `json:"walHealth,omitempty"`

    // LastUpdated is the timestamp when this status was last updated.
    // +optional
    LastUpdated *metav1.Time `json:"lastUpdated,omitempty"`
}

type VolumeDiskStatus struct {
    TotalBytes     uint64 `json:"totalBytes,omitempty"`
    UsedBytes      uint64 `json:"usedBytes,omitempty"`
    AvailableBytes uint64 `json:"availableBytes,omitempty"`
    PercentUsed    int    `json:"percentUsed,omitempty"`  // 0-100, rounded
    InodesTotal    uint64 `json:"inodesTotal,omitempty"`
    InodesUsed     uint64 `json:"inodesUsed,omitempty"`
    InodesFree     uint64 `json:"inodesFree,omitempty"`
    AtLimit        bool   `json:"atLimit,omitempty"`
}

type WALHealthInfo struct {
    ArchiveHealthy bool             `json:"archiveHealthy,omitempty"`
    PendingWALFiles int             `json:"pendingWALFiles,omitempty"`
    InactiveSlotCount int           `json:"inactiveSlotCount,omitempty"`
    InactiveSlots []InactiveSlotInfo `json:"inactiveSlots,omitempty"`
}

type InactiveSlotInfo struct {
    SlotName       string `json:"slotName"`
    RetentionBytes int64  `json:"retentionBytes"`
}

type AutoResizeEvent struct {
    Timestamp  metav1.Time `json:"timestamp,omitempty"`
    InstanceName string    `json:"instanceName,omitempty"`
    PVCName    string      `json:"pvcName,omitempty"`
    VolumeType string      `json:"volumeType,omitempty"`
    Tablespace string      `json:"tablespace,omitempty"`
    PreviousSize string    `json:"previousSize,omitempty"`
    NewSize    string      `json:"newSize,omitempty"`
    Result     string      `json:"result,omitempty"`
}
```

### Example Configurations

**Large production cluster (percentage step with clamped ceiling):**

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: production-db
spec:
  instances: 3
  storage:
    size: 100Gi
    storageClass: fast-ssd
    resize:
      enabled: true
      triggers:
        usageThreshold: 85       # Resize when 85% used
      expansion:
        step: "20%"              # Exponential: adapts to volume size
        maxStep: "500Gi"         # Prevents timeout-inducing massive resizes
        limit: "2Ti"             # Hard cap
      strategy:
        maxActionsPerDay: 3      # Leaves 1 slot for manual intervention
  walStorage:
    size: 20Gi
    storageClass: fast-ssd
    resize:
      enabled: true
      triggers:
        usageThreshold: 70
        minAvailable: "5Gi"      # Don't wait for 70% on a 20Gi volume
      expansion:
        step: "10Gi"             # Fixed step for WAL (predictable growth)
        limit: "100Gi"
      strategy:
        maxActionsPerDay: 3
        walSafetyPolicy:
          requireArchiveHealthy: true
          maxPendingWALFiles: 50
          alertOnResize: true
```

**Small development cluster (absolute floor protects small volumes):**

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: dev-db
spec:
  instances: 1
  storage:
    size: 1Gi
    resize:
      enabled: true
      triggers:
        usageThreshold: 80
        minAvailable: "500Mi"    # At 1Gi, 80% = 200Mi free; this triggers sooner
      expansion:
        step: "20%"
        minStep: "1Gi"           # Ensure meaningful step even at small sizes
        limit: "20Gi"
      strategy:
        walSafetyPolicy:
          acknowledgeWALRisk: true   # REQUIRED for single-volume
          requireArchiveHealthy: true
          maxPendingWALFiles: 100
```

**Multi-tier with tablespaces (mixed strategies):**

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: analytics-db
spec:
  instances: 3
  storage:
    size: 100Gi
    resize:
      enabled: true
      triggers:
        usageThreshold: 85
      expansion:
        step: "20%"
        maxStep: "50Gi"
        limit: "500Gi"
      strategy:
        maxActionsPerDay: 3
  walStorage:
    size: 20Gi
    resize:
      enabled: true
      triggers:
        usageThreshold: 70
        minAvailable: "5Gi"
      expansion:
        step: "5Gi"
        limit: "50Gi"
      strategy:
        maxActionsPerDay: 3
        walSafetyPolicy:
          requireArchiveHealthy: true
  tablespaces:
    - name: large_objects
      storage:
        size: 500Gi
        storageClass: standard-hdd
        resize:
          enabled: true
          triggers:
            usageThreshold: 90
            minAvailable: "50Gi"
          expansion:
            step: "100Gi"        # Fixed step: predictable for budgeting
            limit: "2Ti"
          strategy:
            maxActionsPerDay: 2  # Conservative for large tablespace resizes
```

---

## Instance Manager Changes

### Disk Probe (`pkg/management/postgres/disk/probe.go`)

A new `disk.Probe` struct wraps `statfs()` calls for each CNPG volume mount point. It exposes `GetDataStats()`, `GetWALStats()`, and `GetTablespaceStats()` methods that return a `VolumeStats` struct containing `TotalBytes`, `UsedBytes`, `AvailableBytes`, `PercentUsed`, and inode statistics.

This builds on the existing `DiskProbe` from the `machinery` package but structures the output for Prometheus metric collection and status reporting.

### WAL Health Checker (`pkg/management/postgres/wal/health.go`)

A `HealthChecker` evaluates WAL health by:

1. **Counting pending archive files**: reading `.ready` files in `pg_wal/archive_status/`
2. **Querying `pg_stat_archiver`**: checking `last_archived_time`, `last_failed_time`, and `failed_count`
3. **Querying `pg_replication_slots`**: identifying inactive physical slots and calculating their WAL retention via `pg_wal_lsn_diff()`

The checker returns a `HealthStatus` struct that the operator uses to make resize decisions.

### Metrics Collection

New Prometheus gauges are registered on the existing `:9187` metrics endpoint. The metric collector runs on a configurable interval (default: 30 seconds) and updates all disk and WAL health metrics. See the **Metrics** section below for the complete list.

### Status Endpoint Extension

The existing `/pg/status` endpoint on `:8000` is extended to include disk status and WAL health in its JSON response. The operator fetches this during reconciliation to make resize decisions without needing to scrape Prometheus.

---

## Operator Changes

### Auto-Resize Reconciler

A new `autoresize.Reconciler` is called from the cluster controller's main reconciliation loop. On each invocation it:

1. Checks if any `resize` configuration is enabled (early exit if not)
2. Fetches disk status from all instances via the `/pg/status` endpoint
3. For each instance, evaluates the data volume, WAL volume (if separate), and each tablespace
4. For each volume: evaluates triggers → checks rate-limit budget → checks expansion limit → checks WAL safety (for WAL/single-volume) → calculates clamped new size → patches PVC → records event

The reconciler returns a `RequeueAfter` of 30 seconds to ensure continuous monitoring.

### Trigger Evaluation

The `triggers.usageThreshold` and `triggers.minAvailable` fields work together. Resize triggers when **either** condition is met:

- **Percentage trigger** (e.g., `usageThreshold: 85`): fires when used space exceeds 85% of total capacity
- **Absolute trigger** (e.g., `minAvailable: "10Gi"`): fires when free space drops below 10Gi

This dual-trigger design addresses the scaling problem where a single percentage is too aggressive for large volumes and too late for small ones. Consider a 1Gi volume: at 80% usage only 200Mi remains free, which may already be critical. Conversely, a 1Ti volume at 80% still has 200Gi free, far more headroom than most workloads need.

When only `usageThreshold` is set, behavior is purely percentage-based. When only `minAvailable` is set, behavior is purely absolute. When both are set, the more protective condition wins.

### Expansion Clamping Logic

The `expansion.step` field uses the Kubernetes `IntOrString` pattern to accept either a percentage or an absolute value:

- **Percentage** (e.g., `"20%"`): new size = current size x 1.20 (exponential, adapts to volume size)
- **Absolute** (e.g., `"10Gi"`): new size = current size + 10Gi (linear, predictable for budgeting)

When using percentage-based step, the `minStep` and `maxStep` fields clamp the calculated value:

```
raw_step = current_size * (step_percent / 100)
clamped_step = max(minStep, min(raw_step, maxStep))
new_size = min(current_size + clamped_step, limit)
```

This clamping addresses two specific problems:

- **The "Thundering Herd"** (`minStep` default: `2Gi`): 20% of 1Gi = 200Mi, which is too small to justify consuming a daily modification slot. The floor ensures each resize is meaningful.
- **The "Petabyte Problem"** (`maxStep` default: `500Gi`): 20% of 10Ti = 2Ti, which can trigger extended cloud provider "Optimizing" states that lock the volume for hours. The ceiling keeps individual resize operations within safe bounds.

When `step` is an absolute value, `minStep` and `maxStep` are ignored since the step is already fixed.

### Rate Limiting

The `strategy.maxActionsPerDay` field replaces the earlier `cooldownPeriod` concept with a budget-based model that maps directly to cloud provider realities.

Cloud providers impose daily modification limits on volumes. AWS EBS, for example, historically limits volumes to approximately 4 modifications per 24 hours. The default `maxActionsPerDay: 3` reserves one modification slot for manual human intervention during emergencies. If the operator consumes all available slots autonomously, an administrator cannot resize a volume manually when they need to.

The reconciler uses a stateless `HasBudget()` function that derives the remaining
budget from `cluster.Status.AutoResizeEvents` — a persisted list of resize events
stored in the cluster status. This is a 24-hour rolling window. When the budget is
exhausted, resize is blocked and a `AutoResizeRateLimited` Kubernetes warning event
is emitted. (The `resize_blocked{reason="rate_limit"}` metric is planned but not
yet populated — see Follow-Up PR #2.)

### Webhook Validation

The validating webhook enforces:

- **Single-volume clusters**: if `resize.enabled` is `true` and `walStorage` is not configured, `strategy.walSafetyPolicy.acknowledgeWALRisk` must be `true`
- **UsageThreshold range**: must be between 1 and 99
- **MinAvailable format**: must be a valid Kubernetes resource quantity
- **Step format**: must be a valid percentage or Kubernetes resource quantity (IntOrString)
- **MinStep/MaxStep format**: must be valid Kubernetes resource quantities; ignored when `step` is absolute
- **MinStep <= MaxStep**: if both are set, `minStep` must not exceed `maxStep`
- **Limit format**: must be a valid Kubernetes resource quantity
- **MaxActionsPerDay range**: must be between 0 and 10

---

## Metrics

### Complete Metrics List

**Instance Manager Metrics (`:9187`)** — exposed from each PostgreSQL pod:

| Metric Name | Type | Labels | Description | Status |
|-------------|------|--------|-------------|--------|
| `cnpg_disk_total_bytes` | Gauge | `volume_type`, `tablespace` | Total volume capacity from `statfs()` | ✅ Implemented |
| `cnpg_disk_used_bytes` | Gauge | `volume_type`, `tablespace` | Used space on volume | ✅ Implemented |
| `cnpg_disk_available_bytes` | Gauge | `volume_type`, `tablespace` | Available space on volume | ✅ Implemented |
| `cnpg_disk_percent_used` | Gauge | `volume_type`, `tablespace` | Percentage of volume used | ✅ Implemented |
| `cnpg_disk_inodes_total` | Gauge | `volume_type`, `tablespace` | Total inodes on volume | ✅ Implemented |
| `cnpg_disk_inodes_used` | Gauge | `volume_type`, `tablespace` | Used inodes | ✅ Implemented |
| `cnpg_disk_inodes_free` | Gauge | `volume_type`, `tablespace` | Free inodes | ✅ Implemented |
| `cnpg_disk_at_limit` | Gauge | `volume_type`, `tablespace` | 1 if volume has reached `expansion.limit` | ✅ Derived from status |
| `cnpg_disk_resizes_total` | Counter | `volume_type`, `tablespace`, `result` | Total auto-resize operations | ✅ Derived from status |
| `cnpg_disk_resize_budget_remaining` | Gauge | `volume_type` | Remaining resize operations in the 24h window | ✅ Derived from status |
| `cnpg_disk_resize_blocked` | Gauge | `volume_type`, `tablespace`, `reason` | 1 if auto-resize is blocked | ⚠️ Not yet populated (see Follow-Up PR #2) |

**Planned WAL Health Metrics** — not yet implemented, planned for Follow-Up PR #2:

| Metric Name | Type | Labels | Description | Status |
|-------------|------|--------|-------------|--------|
| `cnpg_wal_archive_healthy` | Gauge | | 1 if WAL archive is healthy | 🔮 Planned |
| `cnpg_wal_pending_archive_files` | Gauge | | Files awaiting archive | 🔮 Planned |
| `cnpg_wal_inactive_slots` | Gauge | | Count of inactive replication slots | 🔮 Planned |
| `cnpg_wal_slot_retention_bytes` | Gauge | `slot_name` | Bytes retained by each slot | 🔮 Planned |

**Note on metric location:** `resizes_total`, `resize_budget_remaining`, and `at_limit`
are currently derived in the instance manager from cluster status events. This is a
pragmatic approach for the initial implementation. Follow-Up PR #2 may move these
resize-action metrics to the operator's metrics endpoint, which is more idiomatic
(the operator makes resize decisions, not the instance manager).

### Label Values

- **`volume_type`**: `data`, `wal`, `tablespace`
- **`tablespace`**: empty for data/wal volumes; tablespace name for tablespace volumes
- **`reason`** (for `resize_blocked`): `archive_unhealthy`, `too_many_pending_wal`, `inactive_slots`, `rate_limit`, `at_limit`
- **`result`** (for `resizes_total`): `success`, `failed`, `blocked`

---

## Alerting

### Proposed PrometheusRule Alerts

**Critical alerts:**

- **`CNPGDiskCritical`**: Fires when any data or WAL volume exceeds 95% usage for 5 minutes
- **`CNPGWALGrowthArchiveFailure`**: Fires when WAL disk usage is growing (>1GB/hour) while archive is failing. This is the most dangerous condition

**Warning alerts:**

- **`CNPGDiskWarning`**: Volume usage exceeds 80% for 15 minutes
- **`CNPGAutoResizeBlocked`**: Auto-resize is blocked (any reason) for 5 minutes
- **`CNPGAtLimit`**: Volume has reached `expansion.limit` and usage exceeds 80%
- **`CNPGArchiveUnhealthy`**: WAL archiving is failing for 10 minutes
- **`CNPGInactiveReplicationSlots`**: Inactive slots detected for 30 minutes
- **`CNPGResizeBudgetExhausted`**: All daily resize slots consumed; no manual intervention slot available

**Info alerts:**

- **`CNPGAutoResizeOccurred`**: An auto-resize operation completed successfully in the last hour
- **`CNPGTablespaceHighUsage`**: Tablespace usage exceeds 85%

---

## Safety Mechanisms

### Decision Flow

```
Is resize enabled?
    │ No → Done
    │ Yes ↓
Is usage > usageThreshold OR available < minAvailable?
    │ No → Done
    │ Yes ↓
Is daily budget remaining (maxActionsPerDay)?
    │ No → Set metric resize_blocked{reason="rate_limit"} → Done
    │ Yes ↓
Is current size < expansion.limit?
    │ No → Set metric at_limit=1 → Event → Done
    │ Yes ↓
Is this a WAL volume or single-volume cluster?
    │ No → Calculate clamped step → Resize PVC ✓
    │ Yes ↓
Is archive healthy? (if requireArchiveHealthy)
    │ No → Block + Event + Metric → Done
    │ Yes ↓
Pending WAL files < maxPendingWALFiles?
    │ No → Block + Event + Metric → Done
    │ Yes ↓
Slot retention < maxSlotRetentionBytes?
    │ No → Block + Event + Metric → Done
    │ Yes ↓
Calculate clamped step → Resize PVC ✓ → Record Event → Update Metrics → Done
```

### Safety Check Summary

| Check | Applies To | Default | Purpose |
|-------|-----------|---------|---------|
| `acknowledgeWALRisk` | Single-volume data | Required | Explicit risk acknowledgment |
| `requireArchiveHealthy` | WAL + single-volume data | `true` | Block if archive failing |
| `maxPendingWALFiles` | WAL + single-volume data | `100` | Block if too many files pending |
| `maxSlotRetentionBytes` | WAL + single-volume data | Optional | Block if slots retaining too much |
| `expansion.limit` | All volumes | Optional | Prevent runaway growth |
| `maxActionsPerDay` | All volumes | `3` | Prevent exhausting cloud provider quotas |
| `expansion.maxStep` | All volumes (% step) | `500Gi` | Prevent timeout-inducing massive resizes |

### The Single-Volume Foot-Gun in Detail

When `walStorage` is not configured, WAL files live inside the PGDATA directory:

```
/var/lib/postgresql/data/pgdata/
├── base/           ← Database files
├── pg_wal/         ← WAL files (on the same volume!)
├── pg_xact/
└── ...
```

If WAL archiving fails, WAL files accumulate on the data volume. A naive auto-resizer would grow the volume, masking the archive failure until the expansion limit is reached and the archive backlog becomes unrecoverable, potentially compromising point-in-time recovery.

For this reason, single-volume clusters **must** set `strategy.walSafetyPolicy.acknowledgeWALRisk: true` in the webhook validation. The WAL health checks (`requireArchiveHealthy`, `maxPendingWALFiles`) are then especially important for these clusters.

---

## Dashboard Updates

New Grafana panels for the CNPG dashboard:

1. **Disk Usage by Volume Type**: time series of `cnpg_disk_percent_used` with color thresholds (green < 70%, yellow < 85%, orange < 95%, red >= 95%)
2. **Available Space**: stat panel showing `cnpg_disk_available_bytes` for data and WAL volumes
3. **WAL Archive Health**: stat panel with binary healthy/unhealthy mapping
4. **Auto-Resize Operations**: time series of `increase(cnpg_disk_resizes_total[1h])`
5. **Resize Blocked Status**: table showing instances with blocked resize and the reason
6. **Resize Budget**: gauge showing `cnpg_disk_resize_budget_remaining` per volume

---

## Testing Strategy

### Unit Tests

- `ResizeConfiguration` validation (trigger ranges, step format, limit format, clamping bounds)
- Expansion clamping logic (percentage and absolute step, minStep/maxStep, limit capping)
- WAL health evaluation logic (archive check, pending files check, slot check)
- Rate-limit enforcement (maxActionsPerDay budget tracking)
- Single-volume acknowledgment requirement

### Integration Tests

- Webhook validation for single-volume acknowledgment
- Metric collection accuracy
- Status endpoint response format

### E2E Tests

A comprehensive E2E testing plan has been developed separately. Key test scenarios include:

| Scenario | Description |
|----------|-------------|
| **Basic data resize** | Fill data volume to threshold; verify PVC expands |
| **Basic WAL resize** | Fill WAL volume to threshold; verify WAL PVC expands |
| **Archive health block** | Misconfigure backup credentials; fill WAL; verify resize blocked |
| **Inactive slot block** | Create unconsumed replication slot; generate WAL; verify resize blocked |
| **Single-volume no-ack rejection** | Attempt single-volume resize without `acknowledgeWALRisk`; verify webhook rejects |
| **Single-volume with ack** | Single-volume with acknowledgment; fill volume; verify resize |
| **Limit enforcement** | Trigger multiple resizes; verify size never exceeds `expansion.limit` |
| **Rate-limit enforcement** | Trigger rapid resizes; verify daily budget is respected |
| **MinStep clamping** | Small volume with percentage step; verify step is clamped up to minStep |
| **MaxStep clamping** | Large volume with percentage step; verify step is clamped down to maxStep |
| **Tablespace resize** | Fill tablespace volume; verify tablespace PVC expands |
| **Metrics accuracy** | Verify all `cnpg_disk_*` and `cnpg_wal_*` metrics are exposed and reasonable |

Tests use small initial PVC sizes (500Mi-1Gi) with `dd` to quickly fill to threshold. Storage classes that don't support `allowVolumeExpansion` are automatically skipped.

---

## Implementation Phases

### Phase 1: Metrics Foundation — ✅ IMPLEMENTED

**Goal:** Expose accurate disk metrics from the instance manager.

- ✅ Implement `disk.Probe` using `statfs()`
- ✅ Add disk metrics to the Prometheus exporter on `:9187` (capacity, used, available, percent, inodes)
- ✅ WAL health checker (`pkg/management/postgres/wal/health.go`) — queries `pg_stat_archiver`, counts `.ready` files, checks `pg_replication_slots`
- ✅ Instance status endpoint includes disk status
- ⬜ Grafana dashboard panels (deferred to Phase 4)
- ⬜ WAL health _metrics_ on `:9187` (archive_healthy, pending_files, inactive_slots — deferred to Follow-Up PR #2)
- ✅ Unit tests for metrics collection

**Deliverables:** New disk metrics on `:9187/metrics`, disk status in cluster status.

### Phase 2: Auto-Resize Core — ✅ IMPLEMENTED

**Goal:** Implement auto-resize for data and WAL volumes with behavior-driven configuration.

- ✅ `ResizeConfiguration` CRD (with `triggers`, `expansion`, `strategy` sub-structs)
- ✅ Auto-resize reconciler with clamping logic
- ✅ Stateless rate-limit budget tracking from `cluster.Status.AutoResizeEvents`
- ✅ `expansion.limit` enforcement
- ✅ Resize events and status updates (persisted via `Status().Patch()`)
- ✅ Webhook validation (including int step rejection, single-volume acknowledgment)
- ✅ E2E tests for basic resize, clamping, rate limiting, limit enforcement, minAvailable trigger

**Deliverables:** Working auto-resize for data and WAL volumes with clamped expansion and rate limiting.

### Phase 3: WAL Safety — ✅ IMPLEMENTED

**Goal:** Implement WAL-aware safety logic.

- ✅ `WALSafetyPolicy` in the `strategy` block
- ✅ WAL health evaluation in the reconciler (archive health, pending files, slot retention)
- ✅ Single-volume `acknowledgeWALRisk` enforcement (webhook rejection)
- ✅ Block resize when archive/replication unhealthy (with Kubernetes events)
- ✅ Fail-open with warning when WAL health data unavailable
- ✅ `AlertOnResize` wired to event recorder
- ✅ E2E tests for archive health blocking
- ⏳ E2E slot retention test pending (PIt — flaky status propagation, follow-up)

**Deliverables:** WAL-aware auto-resize, single-volume protection, archive health enforcement.

### Phase 4: Tablespaces and Polish — PARTIALLY IMPLEMENTED

**Goal:** Complete the feature with tablespace support and tooling.

- ✅ Tablespace auto-resize support (E2E tested)
- ⬜ PrometheusRule alert definitions (not yet implemented)
- ⬜ Grafana dashboard (not yet implemented)
- ⬜ Documentation ("Reclaiming Disk Space" guide — written in RFC, not yet in user docs)
- ⬜ `kubectl cnpg disk status` command (not yet implemented)
- ✅ Core E2E test coverage (12 active tests, 1 pending)

**Deliverables remaining:** PrometheusRule alerts, Grafana dashboard, kubectl command, user documentation.

---

## Migration and Compatibility

- **No breaking changes**: auto-resize is entirely opt-in via new fields
- **Existing clusters**: continue to work without any modification
- **Manual resize**: still fully supported alongside auto-resize
- **Upgrade path**: upgrade operator, then optionally enable resize on existing clusters
- **Feature gate** (optional for initial release): `CNPG_FEATURE_GATES=AutoResize=true`

---

## Alternatives Considered

### Alternative 1: Integrate with topolvm/pvc-autoresizer

**Rejected.** Lacks PostgreSQL/WAL awareness, requires Prometheus as a hard dependency, uses generic PVC annotations that don't fit the Cluster CRD model, and cannot block resize based on archive health.

### Alternative 2: Use kubelet metrics via Prometheus

**Rejected.** Adds Prometheus as a hard dependency, is less accurate than direct `statfs()`, cannot detect CSI resize failures, and provides no PostgreSQL-specific context.

### Alternative 3: SQL-based monitoring only

**Rejected.** PostgreSQL's `pg_database_size()` and `pg_tablespace_size()` provide no volume capacity or free space information. The database cannot answer the question "how full is my disk?"

### Alternative 4: Sidecar container for monitoring

**Rejected.** Adds unnecessary complexity when the instance manager already runs inside the pod with access to all mount points.

### Alternative 5: Simple boolean (`autoResize: true`) with hardcoded defaults

**Rejected.** Dangerous at scale. A 10TB volume growing by 20% adds 2TB, which is operationally risky on cloud providers with volume modification timeouts. Lacks protections against API quota exhaustion. No mechanism for human override during emergencies.

### Alternative 6: Separate "percent" and "absolute" fields (mutually exclusive)

**Rejected.** Creates invalid states (what happens when both are set?). The Kubernetes `IntOrString` pattern (used by `maxUnavailable` in Deployments) handles mixed units cleanly with a single field.

### Alternative 7: Flat configuration with `cooldownPeriod`

**Rejected after initial proposal.** A time-based cooldown (e.g., 1 hour) doesn't account for cloud provider rate limits. An operator that happens to resize 4 times in 4 hours (perfectly valid under a 1-hour cooldown) may exhaust the daily EBS modification quota, leaving no room for manual intervention. The budget-based `maxActionsPerDay` maps directly to the real constraint.

---

## Known Limitations

### Directory-Based Storage Provisioners

[`statfs()`](https://www.man7.org/linux/man-pages/man2/statfs.2.html) returns statistics for the **filesystem** backing a given mount point. Most production CSI drivers ([AWS EBS](https://github.com/kubernetes-sigs/aws-ebs-csi-driver), [GCE Persistent Disk](https://cloud.google.com/kubernetes-engine/docs/how-to/persistent-volumes/gce-pd-csi-driver), [Ceph RBD](https://docs.ceph.com/en/reef/rbd/rbd-kubernetes/), [TopoLVM](https://github.com/topolvm/topolvm)) create an isolated block device and filesystem per PVC, so `statfs()` accurately reflects per-PVC usage.

However, directory-based provisioners like [local-path-provisioner](https://github.com/rancher/local-path-provisioner) implement PVCs as directories on a shared host filesystem. In this configuration, `statfs()` returns the **host filesystem stats** for every PVC on the same node. This means:

- Multiple CNPG clusters sharing a node would all report the same usage percentage
- Auto-resize could trigger for a cluster that isn't the one consuming space
- Metrics would not reflect per-cluster usage accurately

**Mitigation:** Auto-resize requires a CSI driver that provides isolated filesystems per PVC. This should be validated in documentation and could be detected at runtime by comparing device IDs across mount points. Directory-based provisioners (commonly used in development/test environments) are not suitable for this feature.

### Cluster Spec Not Updated After Resize

Auto-resize patches PVCs directly but does **not** update `spec.storage.size` (or `spec.walStorage.size` / tablespace sizes) in the Cluster CR. After auto-resize grows a PVC from 10Gi to 15Gi, the Cluster CR still declares `size: 10Gi`.

**Consequences:**

- New replicas added via scale-up are created with PVCs at the original spec size (10Gi). They will be auto-resized on their next probe cycle, but there is a window where the new replica has less storage than existing instances.
- Volume snapshot restores are affected: a snapshot taken from a resized PVC (15Gi) may fail or produce unexpected results when restored into a PVC whose spec declares the original smaller size (10Gi). Behavior is CSI-driver-dependent — some drivers refuse the restore, others silently create the larger volume regardless of the spec size. This is particularly relevant for CNPG's snapshot-based replica creation and backup restore workflows, where the snapshot source volume may have been auto-resized beyond the declared spec.
- GitOps tools (ArgoCD, Flux) see no spec drift, which is desirable.
- The existing manual resize flow (`reconcilePVCQuantity`) uses `spec.storage.size` as a floor — PVCs are grown to match spec, never shrunk. If auto-resize wrote back to spec, this floor would permanently ratchet upward, preventing any future PVC shrink workflow (e.g., recreating replicas with a smaller spec to reclaim space).

**Design rationale:** The Cluster CR spec represents the user's declarative intent. The operator should not mutate it. Resize outcomes are recorded in `cluster.Status.AutoResizeEvents` for observability. This follows the Kubernetes convention where controllers write to `.status`, not `.spec`.

**Future consideration:** A configurable `updateSpecOnResize` option could allow users who don't use GitOps to opt into spec updates, ensuring new replicas start at the current size. However, this interacts with potential PVC shrink support and requires careful design. See Open Questions.

---

## Pre-Merge Implementation Issues

The following issues were identified during internal review. Items are split
into two categories based on PR scope:

- **This PR:** Must be resolved before submitting the initial feature PR.
- **Follow-up PR:** Will be addressed in focused follow-up PRs after the core
  feature merges. This keeps the initial PR (55 files, 11.5k+ lines) reviewable.

### Status Persistence Bug — RESOLVED

`autoresize.Reconcile()` returned early from the controller loop, skipping
status updates. **Fixed:** `cluster_controller.go` now calls
`Status().Patch(ctx, cluster, client.MergeFrom(origCluster))` after
`autoresize.Reconcile` returns, ensuring `AutoResizeEvents` are persisted.

### Non-Persistent Rate Limiting — RESOLVED

`GlobalBudgetTracker` was an in-memory map lost on restart. **Fixed:** Replaced
with stateless `HasBudget()` that derives budget from persisted
`cluster.Status.AutoResizeEvents`. `ratelimit.go` and `ratelimit_test.go`
deleted. `PVCName` field added to `AutoResizeEvent`.

### Resize Metrics — PARTIALLY RESOLVED

Four resize-action metrics were registered but never populated. **Partially fixed:**
`deriveDecisionMetrics()` in `disk.go` now populates `ResizesTotal`,
`ResizeBudgetRemain`, and `AtLimit` from cluster status history. Metrics are
derived in the instance manager from cluster status.

**Remaining gaps:**
- `cnpg_disk_resize_blocked` is registered but still not populated (the blocked
  metric requires operator-side tracking, not instance-manager-side). See
  Follow-Up PR #2.
- WAL health metrics (`cnpg_wal_archive_healthy`, `cnpg_wal_pending_archive_files`,
  `cnpg_wal_inactive_slots`, `cnpg_wal_slot_retention_bytes`) are not yet
  implemented as Prometheus metrics. WAL health _data_ is collected and used for
  resize decisions, but not yet exposed on the `:9187` metrics endpoint. See
  Follow-Up PR #2.

### Remaining Issues — This PR

#### Swallowed Errors in PVC Loop — RESOLVED

In `reconciler.go`, partial PVC failures are now aggregated with `errors.Join`
and returned to the caller with `RequeueAfter: RequeueDelay`.

#### Missing Event for "At Expansion Limit" — RESOLVED

Hitting the expansion limit now emits an `AutoResizeAtLimit` Kubernetes warning event.

#### Magic Number for Event History Cap — RESOLVED

Named constant `maxAutoResizeEventHistory = 50` is now used.

#### ShouldResize Swallows Parse Error — RESOLVED

Invalid `minAvailable` values now log a warning and fall through to percentage-only trigger.

#### Structured Error Wrapping — RESOLVED

`fmt.Errorf` calls in the autoresize package now use `%w` for error wrapping.

#### AlertOnResize Field — RESOLVED

`AlertOnResize` (`*bool`, default `true`) exists in `WALSafetyPolicy` and is
now wired to the event recorder. When `AlertOnResize` is true (default),
successful resize operations emit a `AutoResizeSuccess` Kubernetes event.

### Remaining Issues — Follow-Up PRs

#### Metric Ownership (Follow-Up PR #2)

The current implementation derives logical metrics in the instance manager from
cluster status. The CNPG-idiomatic approach separates concerns: Pod = raw disk
data, Operator = decision metrics. Move resize_blocked, resizes_total, and
budget_remaining to the operator's metrics endpoint.

#### resizeInUseVolumes Ignored (Follow-Up PR #3)

Auto-resize currently ignores `storageConfiguration.resizeInUseVolumes`. Needs
design discussion: does this flag control manual resize, auto-resize, or both?

#### Pointer Fields for Zero-Value Semantics (Follow-Up PR #3)

`UsageThreshold int` and `MaxActionsPerDay int` use bare values. Changing to
`*int` would allow distinguishing "not set" from "explicitly zero" but is an
API surface change that should be a separate PR.

### WAL Safety Fail-Open Warning — RESOLVED

When `walHealth` is nil, resize is allowed (fail-open) and now emits a
`AutoResizeWALHealthUnavailable` Kubernetes warning event.

**Design rationale: Fail-open is correct.** The primary threat is disk full →
PostgreSQL crashes → data unavailable. If WAL health data is missing (instance
manager busy, query timeout, etc.), blocking the resize is MORE dangerous than
allowing it — the disk may be about to fill up. Fail-closed would mean "if we
can't verify WAL health, let the database crash." That's wrong for a storage
safety feature.

### parseQuantityOrDefault Silent Fallback — RESOLVED

`parseQuantityOrDefault` now logs a warning when falling back to a default
value due to a parse error.

### IntOrString Zero-Value Ambiguity for `step: 0` — RESOLVED

`step: 0` is now rejected in webhook validation with a clear error message:
`"step must be a positive quantity or percentage, not 0"`. Additionally,
bare integer step values (e.g., `step: 20`) are rejected with:
`"integer step values are ambiguous; use a percentage string like '20%' or
an absolute quantity like '5Gi'"`.

---

## Configuration Conflicts & Validation Gaps

A comprehensive analysis of configurations that are accepted by the webhook and
CRD schema but lead to surprising, confusing, or silently broken runtime behavior.
These are organized by severity and recommended action.

### Silent No-Op Configurations (High Severity)

**`limit < currentSize`**: If `expansion.limit` is smaller than the current PVC
size, the auto-resizer becomes a permanent silent no-op. It triggers, calculates
a new size, caps it to the limit (which is already exceeded), and produces no
patch. The user believes they have auto-resize protection but will never get a
resize.

**Recommendation:** Webhook should warn (not reject, since PVC could be resized
externally): `"expansion.limit is less than spec.storage.size; auto-resize will
not trigger until the volume exceeds this limit"`.

**`maxActionsPerDay: 0` with `enabled: true`**: The rate limiter blocks every
resize attempt. Metrics show `resize_blocked{reason="rate_limit"}` but the
configuration is contradictory — resize is enabled but can never execute.

**Recommendation:** Webhook should warn: `"maxActionsPerDay: 0 effectively
disables auto-resize despite enabled: true"`.

### Zero-Value Ambiguities (Medium Severity)

**`usageThreshold: 0`**: Treated as "use default (80%)" rather than "never
trigger". A user writing `usageThreshold: 0` expecting to disable the threshold
trigger gets the default 80% instead. This follows the same pattern as the
`step: 0` issue documented in Pre-Merge Issues above.

**`step: 20` (integer, not string)**: An IntOrString integer value like `step: 20`
is parsed as `resource.ParseQuantity("20")` which yields **20 bytes**, not 20%.
To get 20%, the user must write `step: "20%"` (string). A user accustomed to
Kubernetes percentage patterns (like `maxUnavailable: 25`) may inadvertently
configure a 20-byte step, which would be rounded up by the CSI driver or
produce a no-op.

**Recommendation:** Either reject bare integer step values in the webhook
(`"step must be a percentage string like '20%' or an absolute quantity like
'5Gi'"`) or document this behavior prominently.

### Silently Ignored Fields (Medium Severity)

**`minStep`/`maxStep` with absolute `step`**: When `step` is an absolute quantity
(e.g., `"10Gi"`), the `minStep` and `maxStep` fields are silently ignored at
runtime. A user who sets all three fields expecting bounds is misconfigured.

**Recommendation:** Webhook should warn: `"minStep and maxStep are only applied
to percentage-based steps; they are ignored when step is an absolute quantity"`.

### Unbounded & Extreme Configurations (Medium Severity)

**No `limit` specified**: PVC grows without bound until cloud provider limits or
budget exhaustion. This is documented behavior but could surprise users.

**Recommendation:** Consider requiring an explicit `limit` or documenting this
prominently. A very high default (e.g., per-provider limit) could make the
decision conscious.

**`step > 100%`**: A step of `"200%"` triples the volume on each resize. Not
validated by the webhook.

**Recommendation:** Webhook should warn for `step > 100%`:
`"step exceeds 100%; each resize will more than double the volume size"`.

### Overshoot-Then-Cap Scenarios (Low Severity, Safe but Confusing)

**`minStep > limit`**: A small percentage step is clamped up to `minStep`, which
overshoots the limit; then the result is clamped back down to `limit`. The
interaction is safe but the user may not understand why `minStep` appears to
have no effect.

**`minStep > (limit - currentSize)`**: Similar to above. The minimum step
exceeds the remaining growth room, so limit always caps the result. `minStep`
is effectively ignored.

**Recommendation:** Document these interactions explicitly in the user guide.

### Trigger Edge Cases (Low Severity)

**`minAvailable > volumeSize`**: On a 1Gi volume with `minAvailable: "5Gi"`,
the trigger fires immediately on every probe (available space is always < 5Gi).
The volume resizes on every budget refresh until it exceeds 5Gi.

**Both triggers undefined (`triggers: {}`)**: `usageThreshold` defaults to 80,
`minAvailable` is disabled. Resize triggers at 80% usage. Users who expect
"no triggers = never resize" may be surprised.

**Recommendation:** Webhook should warn when `minAvailable > spec.storage.size`.

### WAL Safety Policy Conflicts (Medium Severity)

**`requireArchiveHealthy: true` without backup configured**: If the cluster has
no backup stanza, there is no WAL archiving. `pg_stat_archiver.last_archived_time`
is NULL. The health check may incorrectly report archiving as unhealthy, blocking
all resizes indefinitely.

**Recommendation:** Clarify semantics: if no backup is configured,
`requireArchiveHealthy` should be a no-op (with warning) or explicitly documented.

**`acknowledgeWALRisk: true` on a dual-volume cluster (with `walStorage`)**: The
flag is accepted but has no effect — it is only relevant for single-volume clusters
where data and WAL share a volume. Setting it on a dual-volume cluster is a
misleading no-op.

**Recommendation:** Webhook should warn: `"acknowledgeWALRisk has no effect when
walStorage is configured"`.

### Cross-Volume Independence (Documentation Gap)

Users can configure different policies for data vs. WAL storage, including
different `maxActionsPerDay` values. Each volume has independent rate limiting.
This is correct per the RFC (cloud provider limits apply per-volume), but may
surprise users who expect cluster-wide limits.

**Recommendation:** Document this clearly in the user guide.

### Budget Window Observability (UX Improvement)

The 24-hour rolling window for `maxActionsPerDay` is hard to observe. A user
seeing `budget_remaining: 0` has no easy way to know when the next slot opens
without manually calculating from `AutoResizeEvents` timestamps.

**Recommendation:** Add a `NextActionAt` timestamp field to the cluster status
(derived from oldest event in the 24h window + 24h).

### Event History Capping (Design Consideration)

`appendResizeEvent` caps history at 50 events. On clusters with frequent resizes
across multiple volumes/tablespaces, this history could rotate quickly, losing
audit data needed for the 24-hour budget window calculation (when the rate
limiter is made persistent per Phase 2).

**Recommendation:** Ensure the cap (50) is sufficient for worst-case budget
calculation: `maxActionsPerDay(10) × volumes × 2 days of history`. Consider
making the cap configurable or sizing it per-volume.

### GitOps Visibility (UX Consideration)

The `acknowledgeWALRisk` webhook rejection is only visible if the user checks
the admission response. GitOps tools (ArgoCD, Flux) may surface this as a
generic `"Invalid"` error, and the specific reason (missing WAL risk
acknowledgment) could be buried. This is a general Kubernetes UX issue, not
specific to this feature.

**Recommendation:** Ensure the webhook error message is descriptive enough to
appear in the ArgoCD/Flux sync status. Consider also emitting a Kubernetes
event on the cluster object for webhook rejections.

### Summary of Validation Recommendations

| Configuration | Current | Recommendation | Severity |
|---|---|---|---|
| `limit < currentSize` | Accepted | Warn | High |
| `maxActionsPerDay: 0` + `enabled: true` | Accepted | Warn | Medium |
| `usageThreshold: 0` | Accepted (default 80%) | Reject or document | Medium |
| `step: 0` | Accepted (default 20%) | Reject (see Pre-Merge Issues) | Medium |
| `step: 20` (integer) | Accepted (20 bytes) | Reject or document | Medium |
| `step > 100%` | Accepted | Warn | Medium |
| `minStep`/`maxStep` + absolute step | Accepted (ignored) | Warn | Medium |
| `minAvailable > volumeSize` | Accepted | Warn | Low |
| `minStep > limit` | Accepted (cap) | Document | Low |
| `acknowledgeWALRisk` on dual-volume | Accepted (no-op) | Warn | Low |
| `requireArchiveHealthy` without backup | Accepted (undefined) | Warn or no-op | Medium |

---

## Open Questions

1. **Should tablespace auto-resize be included in Phase 1?**
   *Recommendation: Defer to Phase 4 for a simpler initial release.*

2. **Should we support inode-based triggers?**
   *Recommendation: Yes. Many small files (e.g., `pg_wal/archive_status/`) can exhaust inodes. Add a `triggers.inodeThreshold` field.*

3. **How should we handle CSI drivers that don't support expansion?**
   *Recommendation: Pre-flight check in webhook validation + clear error message. Skip resize gracefully if `allowVolumeExpansion` is false.*

4. **Should we integrate with VPA (Vertical Pod Autoscaler)?**
   *Recommendation: Out of scope for initial release.*

5. **Should we add a "dry-run" mode for testing policies?**
   *Recommendation: Consider for a future enhancement. For now, metrics + events provide visibility.*

6. **Should the feature be gated behind a feature flag for the initial release?**
   *Recommendation: Yes. `CNPG_FEATURE_GATES=AutoResize=true` for the first release cycle, then enabled by default.*

7. **Should resize decisions consider growth rate / trajectory?**
   *A predictive approach could calculate time-to-full based on historical growth rate and trigger resize earlier when growth is accelerating. This would be more intelligent than static thresholds but adds complexity (requires tracking historical data points, choosing a time window, handling bursty vs. steady growth). Recommendation: Defer to a future enhancement. The `strategy.mode` field is designed to accommodate a future `"Predictive"` mode without breaking the API.*

8. **Should `maxActionsPerDay` be per-volume or per-cluster?**
   *Recommendation: Per-volume. Cloud provider rate limits typically apply per-volume (e.g., each EBS volume has its own modification cooldown), not per-cluster.*

9. **Should auto-resize optionally update `spec.storage.size` after a successful resize?**
   *Currently, auto-resize patches only the PVC. New replicas added after resize start at the original spec size and must be auto-resized. Volume snapshot restores from resized PVCs may also behave unexpectedly when the target PVC spec declares the original smaller size (behavior is CSI-driver-dependent). An `updateSpecOnResize` option would keep the spec in sync, ensuring new replicas and snapshot restores use the current size. However, this permanently ratchets the spec floor upward, which would block any future PVC shrink workflow (recreating replicas at a smaller size). It also causes GitOps drift. Recommendation: Defer. Document the current behavior clearly and revisit when PVC shrink support is designed. Users who need immediate parity can manually update `spec.storage.size` after observing resize events.*

---

## Test Coverage

### Unit Tests

The feature has comprehensive unit test coverage across four test suites:

- **Reconciler clamping** (`pkg/reconciler/autoresize/clamping_test.go`, `clamping_edge_cases_test.go`): percentage steps, absolute steps, minStep/maxStep clamping, limit enforcement, degenerate configurations, terabyte-scale and megabyte-scale volumes
- **Trigger evaluation** (`pkg/reconciler/autoresize/triggers_test.go`): usageThreshold, minAvailable, both-triggers OR logic, defaults, nil handling, edge cases
- **Stateless rate limiting** (`pkg/reconciler/autoresize/hasbudget_test.go`): budget from empty events, budget exhausted, events older than 24h ignored, per-PVC isolation, mixed old/new events, maxActions=0 boundary, negative maxActions
- **WAL safety** (`pkg/reconciler/autoresize/walsafety_test.go`): archive health, pending WAL files, slot retention, PVC role variants, nil inputs, fail-open when WAL health unavailable
- **Webhook validation** (`internal/webhook/v1/cluster_webhook_autoresize_test.go`, `cluster_webhook_autoresize_conflicts_test.go`): per-field validation, cross-field conflict detection (limit < size, step > limit, minStep > limit, absolute step with minStep/maxStep, multi-volume errors, WAL safety edge cases, int step rejection, step: 0 rejection)
- **Webhook warnings** (`internal/webhook/v1/cluster_webhook_autoresize_warnings_test.go`): maxActionsPerDay=0, minAvailable > size, limit <= size, minStep/maxStep with absolute step, acknowledgeWALRisk on dual-volume, requireArchiveHealthy without backup, valid config produces no warnings

### E2E Tests

12 active E2E tests + 1 pending on AKS (3-node cluster, Azure Disk CSI driver):

1. Basic data PVC resize (+ REQ-12 event verification, REQ-16 multi-instance)
2. WAL PVC resize (separate WAL volume)
3. Expansion limit enforcement (clamping + second-fill blocking verification)
4. Webhook rejects single-volume without `acknowledgeWALRisk`
5. Webhook accepts single-volume with `acknowledgeWALRisk`
6. Rate-limit enforcement (`maxActionsPerDay: 1`, PercentUsed preconditions)
7. MinStep clamping (5% of 2Gi clamped to 1Gi minStep)
8. MaxStep webhook validation
9. Prometheus metric exposure (`cnpg_disk_*` label and value assertions)
10. Tablespace PVC resize
11. WAL archive health blocks resize (ArchiveHealthy precondition check)
12. MinAvailable trigger (absolute bytes-remaining trigger)

Test #13 (inactive replication slot blocks resize) is marked `PIt()` (pending)
due to a flaky status propagation issue — the blocking logic is unit-tested and
the same WAL safety code path is proven by the archive health test (#11). This
will be stabilized in a follow-up.

Tests use `Eventually`/`Consistently` patterns throughout (no `time.Sleep`).

See [E2E Testing Requirements](pvc-autoresize-e2e-requirements.md) for the full
test inventory, gap analysis, and known issues.

---

## References

- [topolvm/pvc-autoresizer](https://github.com/topolvm/pvc-autoresizer)
- [Kubernetes Volume Expansion](https://kubernetes.io/docs/concepts/storage/persistent-volumes/#expanding-persistent-volumes-claims)
- [CNPG Storage Documentation](https://cloudnative-pg.io/documentation/current/storage/)
- [CloudNativePG Recipe 6: Postgres Vertical Scaling with Storage](https://www.gabrielebartolini.it/articles/2024/04/cloudnativepg-recipe-6-postgres-vertical-scaling-with-storage-part-1/)
- [AWS EBS Volume Modification Constraints](https://docs.aws.amazon.com/ebs/latest/userguide/modify-volume-requirements.html)

---

*This RFC was prepared alongside a detailed E2E testing design. I welcome feedback on the approach, API surface, safety mechanisms, and implementation phasing.*

*Companion documents:*

- *[E2E Testing Spec](pvc-autoresize-e2e-testing.md) — original test scenario designs*
- *[E2E Testing Requirements](pvc-autoresize-e2e-requirements.md) — gap analysis, AKS test results, and prioritized requirements list*

---

¹ **Design Evolution:** The initial version of this RFC used a flat `AutoResizeConfiguration` struct with `usageThreshold`, `increase`, `minIncrease`, `maxIncrease`, `maxSize`, and `cooldownPeriod`. Community feedback (particularly from @ardentperf) identified that straight percentages are problematic across different volume scales, and that time-based cooldowns don't map to cloud provider rate limits. The redesigned behavior-driven model with `triggers`, `expansion` (with clamping), and `strategy` (with budget-based rate limiting) addresses these concerns while remaining simple for the common case.
