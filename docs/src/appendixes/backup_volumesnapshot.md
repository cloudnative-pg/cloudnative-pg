---
id: backup_volumesnapshot
title: Appendix A - Backup on volume snapshots
---

# Appendix A - Backup on volume snapshots
<!-- SPDX-License-Identifier: CC-BY-4.0 -->

:::info[Important]
    Please refer to the official Kubernetes documentation for a list of all
    the supported [Container Storage Interface (CSI) drivers](https://kubernetes-csi.github.io/docs/drivers.html)
    that provide snapshotting capabilities.
:::

CloudNativePG is one of the first known cases of database operators that
directly leverages the Kubernetes native Volume Snapshot API for both
backup and recovery operations, in an entirely declarative way.

## About standard Volume Snapshots

Volume snapshotting was first introduced in
[Kubernetes 1.12 (2018) as alpha](https://kubernetes.io/blog/2018/10/09/introducing-volume-snapshot-alpha-for-kubernetes/),
promoted to [beta in 1.17 (2019)](https://kubernetes.io/blog/2019/12/09/kubernetes-1-17-feature-cis-volume-snapshot-beta/),
and [moved to GA in 1.20 (2020)](https://kubernetes.io/blog/2020/12/10/kubernetes-1.20-volume-snapshot-moves-to-ga/).
It’s now stable, widely available, and standard, providing 3 custom resource
definitions: `VolumeSnapshot`, `VolumeSnapshotContent` and
`VolumeSnapshotClass`.

This Kubernetes feature defines a generic interface for:

* the creation of a new volume snapshot, starting from a PVC
* the deletion of an existing snapshot
* the creation of a new volume from a snapshot

Kubernetes delegates the actual implementation to the underlying CSI drivers
(not all of them support volume snapshots). Normally, storage classes that
provide volume snapshotting support **incremental and differential block level
backup in a transparent way for the application**, which can delegate the
complexity and the independent management down the stack, including
cross-cluster availability of the snapshots.

## Requirements

For Volume Snapshots to work with a CloudNativePG cluster, you need to ensure
that each storage class used to dynamically provision the PostgreSQL volumes
(namely, `storage` and `walStorage` sections) support volume snapshots.

Given that instructions vary from storage class to storage class, please
refer to the documentation of the specific storage class and related CSI
drivers you have deployed in your Kubernetes system.

Normally, it is the [`VolumeSnapshotClass`](https://kubernetes.io/docs/concepts/storage/volume-snapshot-classes/)
that is responsible to ensure that snapshots can be taken from persistent
volumes of a given storage class, and managed as `VolumeSnapshot` and
`VolumeSnapshotContent` resources.

:::info[Important]
    It is your responsibility to verify with the third party vendor
    that volume snapshots are supported. CloudNativePG only interacts
    with the Kubernetes API on this matter, and we cannot support issues
    at the storage level for each specific CSI driver.
:::

## How to configure Volume Snapshot backups

CloudNativePG allows you to configure a given Postgres cluster for Volume
Snapshot backups through the `backup.volumeSnapshot` stanza.

:::info
    Please refer to [`VolumeSnapshotConfiguration`](../cloudnative-pg.v1.md#volumesnapshotconfiguration)
    in the API reference for a full list of options.
:::

A generic example with volume snapshots (assuming that PGDATA and WALs share
the same storage class) is the following:

``` yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: snapshot-cluster
spec:
  instances: 3

  storage:
    storageClass: @STORAGE_CLASS@
    size: 10Gi
  walStorage:
    storageClass: @STORAGE_CLASS@
    size: 10Gi

  backup:
    # Volume snapshot backups
    volumeSnapshot:
       className: @VOLUME_SNAPSHOT_CLASS_NAME@
       
  plugins:
  - name: barman-cloud.cloudnative-pg.io
    isWALArchiver: true
    parameters:
      barmanObjectName: @OBJECTSTORE_NAME@
```

As you can see, the `backup` section contains both the `volumeSnapshot` stanza
(controlling physical base backups on volume snapshots) and the
`plugins` one (controlling the [WAL archive](../wal_archiving.md)).

:::info
    Once you have defined the `plugin`, you can decide to use
    both volume snapshot and plugin backup strategies simultaneously
    to take physical backups.
:::

The `volumeSnapshot.className` option allows you to reference the default
`VolumeSnapshotClass` object used for all the storage volumes you have
defined in your PostgreSQL cluster.

:::info
    In case you are using a different storage class for `PGDATA` and
    WAL files, you can specify a separate `VolumeSnapshotClass` for
    that volume through the `walClassName` option (which defaults to
    the same value as `className`).
:::

Once a cluster is defined for volume snapshot backups, you need to define
a `ScheduledBackup` resource that requests such backups on a periodic basis.

## Hot and cold backups

:::warning
    As noted in the [backup document](../backup.md), a cold snapshot explicitly
    set to target the primary will result in the primary being fenced for
    the duration of the backup, making the cluster read-only during this
    period. For safety, in a cluster already containing fenced instances, a cold
    snapshot is rejected.
:::

By default, CloudNativePG requests an online/hot backup on volume snapshots, using the
[PostgreSQL defaults of the low-level API for base backups](https://www.postgresql.org/docs/current/continuous-archiving.html#BACKUP-LOWLEVEL-BASE-BACKUP):

- it doesn't request an immediate checkpoint when starting the backup procedure
- it waits for the WAL archiver to archive the last segment of the backup when
  terminating the backup procedure

:::info[Important]
    The default values are suitable for most production environments. Hot
    backups are consistent and can be used to perform snapshot recovery, as we
    ensure WAL retention from the start of the backup through a temporary
    replication slot. However, our recommendation is to rely on cold backups for
    that purpose.
:::

You can explicitly change the default behavior through the following options in
the `.spec.backup.volumeSnapshot` stanza of the `Cluster` resource:

- `online`: accepting `true` (default) or `false` as a value
- `onlineConfiguration.immediateCheckpoint`: whether you want to request an
  immediate checkpoint before you start the backup procedure or not;
  technically, it corresponds to the `fast` argument you pass to the
  `pg_backup_start`/`pg_start_backup()` function in PostgreSQL, accepting
  `true` (default) or `false`
- `onlineConfiguration.waitForArchive`: whether you want to wait for the
  archiver to process the last segment of the backup or not; technically, it
  corresponds to the `wait_for_archive` argument you pass to the
  `pg_backup_stop`/`pg_stop_backup()` function in PostgreSQL, accepting `true`
  (default) or `false`

If you want to change the default behavior of your Postgres cluster to take
cold backups by default, all you need to do is add the `online: false` option
to your manifest, as follows:

```yaml
  # ...
  backup:
    volumeSnapshot:
       online: false
       # ...
```

If you are instead requesting an immediate checkpoint as the default behavior,
you can add this section:

```yaml
  # ...
  backup:
    volumeSnapshot:
       online: true
       onlineConfiguration:
         immediateCheckpoint: true
       # ...
```

### Overriding the default behavior

You can change the default behavior defined in the cluster resource by setting
different values for `online` and, if needed, `onlineConfiguration` in the `Backup` or `ScheduledBackup` objects.

For example, in case you want to issue an on-demand cold backup, you can
create a `Backup` object with `.spec.online: false`:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Backup
metadata:
  name: snapshot-cluster-cold-backup-example
spec:
  cluster:
    name: snapshot-cluster
  method: volumeSnapshot
  online: false
```

Similarly, for the ScheduledBackup:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: ScheduledBackup
metadata:
  name: snapshot-cluster-cold-backup-example
spec:
  schedule: "0 0 0 * * *"
  backupOwnerReference: self
  cluster:
    name: snapshot-cluster
  method: volumeSnapshot
  online: false
```

## Persistence of volume snapshot objects

By default, `VolumeSnapshot` objects created by CloudNativePG are retained after
deleting the `Backup` object that originated them, or the `Cluster` they refer to.
Such behavior is controlled by the `.spec.backup.volumeSnapshot.snapshotOwnerReference`
option which accepts the following values:

- `none`: no ownership is set, meaning that `VolumeSnapshot` objects persist
   after the `Backup` and/or the `Cluster` resources are removed
- `backup`: the `VolumeSnapshot` object is owned by the `Backup` resource that
   originated it, and when the backup object is removed, the volume snapshot is
   also removed
- `cluster`: the `VolumeSnapshot` object is owned by the `Cluster` resource that
   is backed up, and when the Postgres cluster is removed, the volume snapshot is
   also removed

In case a `VolumeSnapshot` is deleted, the `deletionPolicy` specified in the
`VolumeSnapshotContent` is evaluated:

- if set to `Retain`, the `VolumeSnapshotContent` object is kept
- if set to `Delete`, the `VolumeSnapshotContent` object is removed as well

:::warning
    `VolumeSnapshotContent` objects do not keep all the information regarding the
    backup and the cluster they refer to (like the annotations and labels that
    are contained in the `VolumeSnapshot` object). Although possible, restoring
    from just this kind of object might not be straightforward. For this reason,
    our recommendation is to always backup the `VolumeSnapshot` definitions,
    even using a Kubernetes level data protection solution.
:::

The value in `VolumeSnapshotContent` is determined by the `deletionPolicy` set
in the corresponding `VolumeSnapshotClass` definition, which is
referenced in the `.spec.backup.volumeSnapshot.className` option.

Please refer to the [Kubernetes documentation on Volume Snapshot Classes](https://kubernetes.io/docs/concepts/storage/volume-snapshot-classes/)
for details on this standard behavior.

## Backup Volume Snapshot Deadlines

CloudNativePG supports backups using the volume snapshot method. In some
environments, volume snapshots may encounter temporary issues that can be
retried.

The `backup.cnpg.io/volumeSnapshotDeadline` annotation defines how long
CloudNativePG should continue retrying recoverable errors before marking the
backup as failed.

You can add the `backup.cnpg.io/volumeSnapshotDeadline` annotation to both
`Backup` and `ScheduledBackup` resources. For `ScheduledBackup` resources, this
annotation is automatically inherited by any `Backup` resources created from
the schedule.

If not specified, the default retry deadline is **10 minutes**.

### Error Handling

When a retryable error occurs during a volume snapshot operation:

1. CloudNativePG records the time of the first error.
2. The system retries the operation every **10 seconds**.
3. If the error persists beyond the specified deadline (or the default 10
   minutes), the backup is marked as **failed**.

### Retryable Errors

CloudNativePG treats the following types of errors as retryable:

- **Server timeout errors** (HTTP 408, 429, 500, 502, 503, 504)
- **Conflicts** (optimistic locking errors)
- **Internal errors**
- **Context deadline exceeded errors**
- **Timeout errors from the CSI snapshot controller**

### Examples

You can add the annotation to a `ScheduledBackup` resource as follows:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: ScheduledBackup
metadata:
  name: daily-backup-schedule
  annotations:
    backup.cnpg.io/volumeSnapshotDeadline: "20"
spec:
  schedule: "0 0 * * *"
  backupOwnerReference: self
  method: volumeSnapshot
  # other configuration...
```

When you define a `ScheduledBackup` with the annotation, any `Backup` resources
created from this schedule automatically inherit the specified timeout value.

In the following example, all backups created from the schedule will have a
30-minute timeout for retrying recoverable snapshot errors.

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: ScheduledBackup
metadata:
  name: weekly-backup
  annotations:
    backup.cnpg.io/volumeSnapshotDeadline: "30"
spec:
  schedule: "0 0 * * 0"  # Weekly backup on Sunday
  method: volumeSnapshot
  cluster:
    name: my-postgresql-cluster
```

Alternatively, you can add the annotation directly to a `Backup` Resource:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Backup
metadata:
  name: my-backup
  annotations:
    backup.cnpg.io/volumeSnapshotDeadline: "15"
spec:
  method: volumeSnapshot
  # other backup configuration...
```

## Example of Volume Snapshot Backup

The following example shows how to configure volume snapshot base backups on an
EKS cluster on AWS using the `ebs-sc` storage class and the `csi-aws-vsc`
volume snapshot class.

:::info[Important]
    If you are interested in testing the example, please read
    ["Volume Snapshots" for the Amazon Elastic Block Store (EBS) CSI driver](https://github.com/kubernetes-sigs/aws-ebs-csi-driver/tree/master/examples/kubernetes/snapshot) <!-- wokeignore:rule=master -->
    for detailed instructions on the installation process for the storage class and the snapshot class.
:::

The following manifest creates a `Cluster` that is ready to be used for volume
snapshots and that stores the WAL archive in a S3 bucket via IAM role for the
Service Account (IRSA, see [AWS S3](object_stores.md#aws-s3)):

``` yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: hendrix
spec:
  instances: 3

  storage:
    storageClass: ebs-sc
    size: 10Gi
  walStorage:
    storageClass: ebs-sc
    size: 10Gi

  backup:
    volumeSnapshot:
       className: csi-aws-vsc

  plugins:
  - name: barman-cloud.cloudnative-pg.io
    isWALArchiver: true
    parameters:
      barmanObjectName: @OBJECTSTORE_NAME@

  serviceAccountTemplate:
    metadata:
      annotations:
        eks.amazonaws.com/role-arn: "@ARN@"
---
apiVersion: postgresql.cnpg.io/v1
kind: ScheduledBackup
metadata:
  name: hendrix-vs-backup
spec:
  cluster:
    name: hendrix
  method: volumeSnapshot
  schedule: '0 0 0 * * *'
  backupOwnerReference: cluster
  immediate: true
```

The last resource defines daily volume snapshot backups at midnight, requesting
one immediately after the cluster is created.
