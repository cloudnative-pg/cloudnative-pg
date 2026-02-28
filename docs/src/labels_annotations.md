---
id: labels_annotations
sidebar_position: 290
title: Labels and Annotations
---

# Labels and Annotations
<!-- SPDX-License-Identifier: CC-BY-4.0 -->

Resources in Kubernetes are organized in a flat structure, with no hierarchical
information or relationship between them. However, such resources and objects
can be linked together and put in relationship through *labels* and
*annotations*.

:::info
    For more information, see the Kubernetes documentation on
    [annotations](https://kubernetes.io/docs/concepts/overview/working-with-objects/annotations/) and
    [labels](https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/).
:::

In brief:

- An annotation is used to assign additional non-identifying information to
  resources with the goal of facilitating integration with external tools.
- A label is used to group objects and query them through the Kubernetes native
  selector capability.

You can select one or more labels or annotations to use
in your CloudNativePG deployments. Then you need to configure the operator
so that when you define these labels or annotations in a cluster's metadata,
they're inherited by all resources created by it (including pods).

:::note
    Label and annotation inheritance is the technique adopted by CloudNativePG
    instead of alternative approaches such as pod templates.
:::

## Predefined labels

CloudNativePG manages the following predefined labels:

`cnpg.io/backupDate`
: The date of the backup in ISO 8601 format (`YYYYMMDD`).
  This label is available only on `VolumeSnapshot` resources.

`cnpg.io/backupName`
: Backup identifier.
  This label is available only on `VolumeSnapshot` resources.

`cnpg.io/backupMonth`
: The year/month when a backup was taken.
  This label is available only on `VolumeSnapshot` resources.

`cnpg.io/backupTimeline`
: The timeline of the instance when a backup was taken.
  This label is available only on `VolumeSnapshot` resources.

`cnpg.io/backupYear`
: The year a backup was taken.
  This label is available only on `VolumeSnapshot` resources.

`cnpg.io/cluster`
: Name of the cluster.

`cnpg.io/immediateBackup`
: Applied to a `Backup` resource if the backup is the first one created from
  a `ScheduledBackup` object having `immediate` set to `true`.

`cnpg.io/instanceName`
: Name of the PostgreSQL instance (replaces the old and
  deprecated `postgresql` label).

`cnpg.io/jobRole`
: Role of the job (that is, `import`, `initdb`, `join`, ...)

`cnpg.io/majorVersion`
: Integer PostgreSQL major version of the backup's data directory (for example, `17`).
This label is available only on `VolumeSnapshot` resources.

`cnpg.io/onlineBackup`
: Whether the backup is online (hot) or taken when Postgres is down (cold).
  This label is available only on `VolumeSnapshot` resources.

`cnpg.io/podRole`
: Distinguishes pods dedicated to pooler deployment from those used for
  database instances.

`cnpg.io/poolerName`
: Name of the PgBouncer pooler.

`cnpg.io/pvcRole`
: Purpose of the PVC, such as `PG_DATA` or `PG_WAL`.

`cnpg.io/reload`
: Available on `ConfigMap` and `Secret` resources. When set to `true`,
  a change in the resource is automatically reloaded by the operator.

`cnpg.io/userType`
: Specifies the type of PostgreSQL user associated with the
  `Secret`, either `superuser` (Postgres superuser access) or `app`
  (application-level user in CloudNativePG terminology), and is limited to the
  default users created by CloudNativePG (typically `postgres` and `app`).

`role` - **deprecated**
:  Whether the instance running in a pod is a `primary` or a `replica`.
   This label is deprecated, you should use `cnpg.io/instanceRole` instead.

`cnpg.io/scheduled-backup`
:  When available, name of the `ScheduledBackup` resource that created a given
   `Backup` object.

`cnpg.io/instanceRole`
: Whether the instance running in a pod is a `primary` or a `replica`.

`app.kubernetes.io/managed-by`
: Name of the manager. It will always be `cloudnative-pg`.
  Available across all CloudNativePG managed resources.

`app.kubernetes.io/name`
: Name of the application. It will always be `postgresql`.
  Available on pods, jobs, deployments, services, persistentVolumeClaims, volumeSnapshots,
  podDisruptionBudgets, podMonitors.

`app.kubernetes.io/component`
: Name of the component (`database`, `pooler`, ...).
  Available on pods, jobs, deployments, services, persistentVolumeClaims, volumeSnapshots,
  podDisruptionBudgets, podMonitors.

`app.kubernetes.io/instance`
: Name of the owning `Cluster` resource.
  Available on pods, jobs, deployments, services, volumeSnapshots, podDisruptionBudgets, podMonitors.

`app.kubernetes.io/version`
: Major version of PostgreSQL.
  Available on pods, jobs, services, volumeSnapshots, podDisruptionBudgets, podMonitors.

## Predefined annotations

CloudNativePG manages the following predefined annotations:

`container.apparmor.security.beta.kubernetes.io/*`
:   Name of the AppArmor profile to apply to the named container.
    See [AppArmor](security.md#restricting-pod-access-using-apparmor)
    for details.

`cnpg.io/backupEndTime`
: The time a backup ended.
  This annotation is available only on `VolumeSnapshot` resources.

`cnpg.io/backupEndWAL`
: The WAL at the conclusion of a backup.
  This annotation is available only on `VolumeSnapshot` resources.

`cnpg.io/backupStartTime`
: The time a backup started.

`cnpg.io/backupStartWAL`
: The WAL at the start of a backup.
  This annotation is available only on `VolumeSnapshot` resources.

`cnpg.io/coredumpFilter`
:   Filter to control the coredump of Postgres processes, expressed with a
    bitmask. By default it's set to `0x31` to exclude shared memory
    segments from the dump. See [PostgreSQL core dumps](troubleshooting.md#postgresql-core-dumps)
    for more information.

`cnpg.io/clusterManifest`
:   Manifest of the `Cluster` owning this resource (such as a PVC). This label
    replaces the old, deprecated `cnpg.io/hibernateClusterManifest` label.

`cnpg.io/fencedInstances`
:   List of the instances that need to be fenced, expressed in JSON format.
    The whole cluster is fenced if the list contains the `*` element.

`cnpg.io/forceLegacyBackup`
:   Applied to a `Cluster` resource for testing purposes only, to
    simulate the behavior of `barman-cloud-backup` prior to version 3.4 (Jan 2023)
    when the `--name` option wasn't available.

`cnpg.io/hash`
:   The hash value of the resource.

`cnpg.io/hibernation`
:   Applied to a `Cluster` resource to control the [declarative hibernation feature](declarative_hibernation.md).
    Allowed values are `on` and `off`.

`cnpg.io/managedSecrets`
:   Pull secrets managed by the operator and automatically set in the
    `ServiceAccount` resources for each Postgres cluster.

`cnpg.io/nodeSerial`
:   On a pod resource, identifies the serial number of the instance within the
    Postgres cluster.

`cnpg.io/operatorVersion`
:   Version of the operator.

`cnpg.io/pgControldata`
:   Output of the `pg_controldata` command. This annotation replaces the old,
    deprecated `cnpg.io/hibernatePgControlData` annotation.

`cnpg.io/podEnvHash`
:   Deprecated, as the `cnpg.io/podSpec` annotation now also contains the pod environment.

`cnpg.io/podPatch`
:   Annotation can be applied on a `Cluster` resource.

    When set to JSON-patch formatted patch, the patch will be applied on the instance Pods.

    **⚠️ WARNING:** This feature may introduce discrepancies between the
    operator’s expectations and Kubernetes behavior. Use with caution and only as a
    last resort.

    **IMPORTANT**: adding or changing this annotation won't trigger a rolling deployment
    of the generated Pods. The latter can be triggered manually by the user with
    `kubectl cnpg restart`.

`cnpg.io/initdbJobPatch`
:   Annotation can be applied on a `Cluster` resource.

    When set to JSON-patch formatted patch, the patch will be applied on the Job resources
    created by the operator for the initdb operation.

    **⚠️ WARNING:** This feature may introduce discrepancies between the
    operator's expectations and Kubernetes behavior. Use with caution and only as a
    last resort.

    **Note**: This annotation is validated when the cluster is created or updated.
    Invalid JSON-patch will cause a validation error.

    Example of patching termination grace period on the initdb job pod template:

    ```yaml
    apiVersion: postgresql.cnpg.io/v1
    kind: Cluster
    metadata:
      name: cluster-example
      annotations:
        cnpg.io/initdbJobPatch: '[{"op": "add", "path": "/spec/template/spec/terminationGracePeriodSeconds", "value": 60}]'
    spec:
      # ...
    ```

`cnpg.io/importJobPatch`
:   Annotation can be applied on a `Cluster` resource.

    When set to JSON-patch formatted patch, the patch will be applied on the Job resources
    created by the operator for the import operation.

    **⚠️ WARNING:** This feature may introduce discrepancies between the
    operator's expectations and Kubernetes behavior. Use with caution and only as a
    last resort.

    **Note**: This annotation is validated when the cluster is created or updated.
    Invalid JSON-patch will cause a validation error.

`cnpg.io/pgbasebackupJobPatch`
:   Annotation can be applied on a `Cluster` resource.

    When set to JSON-patch formatted patch, the patch will be applied on the Job resources
    created by the operator for the pgbasebackup operation.

    **⚠️ WARNING:** This feature may introduce discrepancies between the
    operator's expectations and Kubernetes behavior. Use with caution and only as a
    last resort.

    **Note**: This annotation is validated when the cluster is created or updated.
    Invalid JSON-patch will cause a validation error.

`cnpg.io/fullRecoveryJobPatch`
:   Annotation can be applied on a `Cluster` resource.

    When set to JSON-patch formatted patch, the patch will be applied on the Job resources
    created by the operator for the full-recovery operation (restoring from a base backup).

    **⚠️ WARNING:** This feature may introduce discrepancies between the
    operator's expectations and Kubernetes behavior. Use with caution and only as a
    last resort.

    **Note**: This annotation is validated when the cluster is created or updated.
    Invalid JSON-patch will cause a validation error.

`cnpg.io/joinJobPatch`
:   Annotation can be applied on a `Cluster` resource.

    When set to JSON-patch formatted patch, the patch will be applied on the Job resources
    created by the operator when a new replica joins the cluster.

    **⚠️ WARNING:** This feature may introduce discrepancies between the
    operator's expectations and Kubernetes behavior. Use with caution and only as a
    last resort.

    **Note**: This annotation is validated when the cluster is created or updated.
    Invalid JSON-patch will cause a validation error.

`cnpg.io/snapshotRecoveryJobPatch`
:   Annotation can be applied on a `Cluster` resource.

    When set to JSON-patch formatted patch, the patch will be applied on the Job resources
    created by the operator for the snapshot-recovery operation.

    **⚠️ WARNING:** This feature may introduce discrepancies between the
    operator's expectations and Kubernetes behavior. Use with caution and only as a
    last resort.

    **Note**: This annotation is validated when the cluster is created or updated.
    Invalid JSON-patch will cause a validation error.

`cnpg.io/majorUpgradeJobPatch`
:   Annotation can be applied on a `Cluster` resource.

    When set to JSON-patch formatted patch, the patch will be applied on the Job resources
    created by the operator for the major upgrade operation.

    **⚠️ WARNING:** This feature may introduce discrepancies between the
    operator's expectations and Kubernetes behavior. Use with caution and only as a
    last resort.

    **Note**: This annotation is validated when the cluster is created or updated.
    Invalid JSON-patch will cause a validation error.

`cnpg.io/podSpec`
:   Snapshot of the `spec` of the pod generated by the operator. This annotation replaces
    the old, deprecated `cnpg.io/podEnvHash` annotation.

`cnpg.io/poolerSpecHash`
:   Hash of the pooler resource.

`cnpg.io/pvcStatus`
:   Current status of the PVC: `initializing`, `ready`, or `detached`.

`cnpg.io/reconcilePodSpec`
:   Annotation can be applied to a `Cluster` or `Pooler` to prevent restarts.

    When set to `disabled` on a `Cluster`, the operator prevents instances
    from restarting due to changes in the PodSpec. This includes changes to:

      - Topology or affinity
      - Scheduler
      - Volumes or containers

    When set to `disabled` on a `Pooler`, the operator restricts any modifications
    to the deployment specification, except for changes to `spec.instances`.

`cnpg.io/reconciliationLoop`
:   When set to `disabled` on a `Cluster`, the operator prevents the
    reconciliation loop from running.

`cnpg.io/reloadedAt`
:   Contains the latest cluster `reload` time. `reload` is triggered by the user through a plugin.

`cnpg.io/skipEmptyWalArchiveCheck`
:   When set to `enabled` on a `Cluster` resource, the operator disables the check
    that ensures that the WAL archive is empty before writing data. Use at your own
    risk.

`cnpg.io/skipWalArchiving`
:   When set to `enabled` on a `Cluster` resource, the operator disables WAL archiving.
    This will set `archive_mode` to `off` and require a restart of all PostgreSQL
    instances. Use at your own risk.

`cnpg.io/snapshotStartTime`
:   The time a snapshot started.

`cnpg.io/snapshotEndTime`
:   The time a snapshot was marked as ready to use.

`cnpg.io/validation`
:   When set to `disabled` on a CloudNativePG-managed custom resource, the
    validation webhook allows all changes without restriction.

    **⚠️ WARNING:** Disabling validation may permit unsafe or destructive
    operations. Use this setting with caution and at your own risk.

`cnpg.io/volumeSnapshotDeadline`
:   Applied to `Backup` and `ScheduledBackup` resources, allows you to control
    how long the operator should retry recoverable errors before considering the
    volume snapshot backup failed. In minutes, defaulting to 10.

`kubectl.kubernetes.io/restartedAt`
:   When available, the time of last requested restart of a Postgres cluster.

`alpha.cnpg.io/unrecoverable`
:   Experimental annotation applied to a `Pod` running a PostgreSQL instance.
    It instructs the operator to delete the `Pod` and all its associated PVCs.
    The instance will then be recreated according to the configured join
    strategy. This annotation can only be used on instances that are neither the
    current primary nor the designated target primary.

## Prerequisites

By default, no label or annotation defined in the cluster's metadata is
inherited by the associated resources.
To enable label/annotation inheritance, follow the
instructions provided in [Operator configuration](operator_conf.md).

The following continues from that example and limits it to the following:

- Annotations: `categories`
- Labels: `app`, `environment`, and `workload`

:::note
    Feel free to select the names that most suit your context for both
    annotations and labels. You can also use wildcards
    in naming and adopt strategies like using `mycompany/*` for all labels
    or setting annotations starting with `mycompany/` to be inherited.
:::

## Defining cluster's metadata

When defining the cluster, before any resource is deployed, you can
set the metadata as follows:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-example
  annotations:
    categories: database
  labels:
    environment: production
    workload: database
    app: sso
spec:
     # ... <snip>
```

Once the cluster is deployed, you can verify, for example, that the labels
were correctly set in the pods:

```shell
kubectl get pods --show-labels
```

## Current limitations

Currently, CloudNativePG doesn't automatically propagate labels or
annotations deletions. Therefore, when an annotation or label is removed from
a cluster that was previously propagated to the underlying pods, the operator
doesn't remove it on the associated resources.
