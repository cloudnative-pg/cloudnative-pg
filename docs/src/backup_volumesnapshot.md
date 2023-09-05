# Backup on volume snapshots

!!! Important
    The current implementation of volume snapshots in CloudNativePG
    supports [cold backup](backup.md#cold-and-hot-backups) only.
    Hot backup with direct support of
    [PostgreSQL's low level API for base backups](https://www.postgresql.org/docs/current/continuous-archiving.html#BACKUP-LOWLEVEL-BASE-BACKUP)
    will be added in version 1.22. Having said this, the current implementation
    is suitable for production HA environments as, by honoring the backup
    target settings, will work on the most aligned standby without impacting the
    primary.

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

!!! Important
    It is your responsibility to verify with the third party vendor
    that volume snapshots are supported. CloudNativePG only interacts
    with the Kubernetes API on this matter and we cannot support issues
    at the storage level for each specific CSI driver.

## How to configure Volume Snapshot backups

CloudNativePG allows you to configure a given Postgres cluster for Volume
Snapshot backups through the `backup.volumeSnapshot` stanza.

!!! Info
    Please refer to [`VolumeSnapshotConfiguration`](cloudnative-pg.v1.md#postgresql-cnpg-io-v1-VolumeSnapshotConfiguration)
    in the API reference for a full list of options.

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
    # WAL archive
    barmanObjectStore:
       # ...
```

As you can see, the `backup` section contains both the `volumeSnapshot` stanza
(controlling physical base backups on volume snapshots) and the
`barmanObjectStore` one (controlling the [WAL archive](wal_archiving.md)).

!!! Info
    Once you have defined the `barmanObjectStore`, you can decide to use
    both volume snapshot and object store backup strategies simultaneously
    to take physical backups.

Once a cluster is defined for volume snapshot backups, you need to define
a `ScheduledBackup` resource that requests such backups on a periodic basis.

## Example

The following example shows how to configure volume snapshot base backups on an
EKS cluster on AWS using the `ebs-sc` storage class and the `csi-aws-vsc`
volume snapshot class.

!!! Important
    If you are interested in testing the example, please read
    ["Volume Snapshots" for the Amazon Elastic Block Store (EBS) CSI driver](https://github.com/kubernetes-sigs/aws-ebs-csi-driver/tree/master/examples/kubernetes/snapshot) <!-- wokeignore:rule=master -->
    for detailed instructions on the installation process for the storage class and the snapshot class.


The following manifest creates a `Cluster` that is ready to be used for volume
snapshots and that stores the WAL archive in a S3 bucket via IAM role for the
Service Account (IRSA, see [AWS S3](appendixes/object_stores.md#aws-s3)):

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
    barmanObjectStore:
      destinationPath: s3://@BUCKET_NAME@/
      s3Credentials:
        inheritFromIAMRole: true
      wal:
        compression: gzip
        maxParallel: 2

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
