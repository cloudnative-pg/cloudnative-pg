# Backup

!!! Important
    With version 1.21, backup and recovery capabilities in CloudNativePG
    have sensibly changed due to the introduction of native support for
    [Kubernetes volume snapshots](backup_volumesnapshot.md).
    Up to that point, backup and recovery were available only for object
    stores. Read about backups and [recovery](recovery.md)
    if you have been a user of CloudNativePG 1.15 through 1.20.

PostgreSQL natively provides first-class backup and recovery capabilities based
on a file-system-level (physical) copy. These capabilities have been successfully used for
more than 15 years in mission-critical production databases, helping
organizations all over the world achieve their disaster-recovery goals with
Postgres.

!!! Note
    You can also back up databases in PostgreSQL using the
    pg_dump utility, which relies on logical backups instead of physical ones.
    However, logical backups aren't suitable for business-continuity use cases
    and as such aren't yet covered by CloudNativePG.
    If you want to use the pg_dump utility, see
    [Emergency backup](troubleshooting.md#emergency-backup) in Troubleshooting.

In CloudNativePG, the backup infrastructure for each PostgreSQL cluster is made
up of the following resources:

- **WAL archive** – A location containing the WAL files (transactional logs)
  that are continuously written by Postgres and archived for data durability.
- **Physical base backups** – A copy of all the files that PostgreSQL uses to
  store the data in the database (primarily the `PGDATA` and any tablespace).

The WAL archive currently can be stored only on object stores.

On the other hand, CloudNativePG supports two ways to store physical base backups:

- On [object stores](backup_barmanobjectstore.md) as tarballs, optionally
  compressed
- On [Kubernetes volume snapshots](backup_volumesnapshot.md), if supported by
  the underlying storage class

!!! Important
    Before choosing your backup strategy with CloudNativePG, 
    familiarize yourself with some basic concepts, like WAL archive and
    hot and cold backups.

!!! Important
    Please refer to the official Kubernetes documentation for a list of all
    the supported [Container Storage Interface (CSI) drivers](https://kubernetes-csi.github.io/docs/drivers.html)
    that provide snapshotting capabilities.

## WAL archive

The WAL archive in PostgreSQL is at the heart of continuous backup, and it's
fundamental for these reasons:

- **Hot backups** – The possibility to take physical base backups from any
  instance in the Postgres cluster (either primary or standby) without shutting
  down the server. They're also known as online backups.
- **Point-in-time recovery** (PITR) – The possibility to recover at any point in
  time from the first available base backup in your system.

!!! Warning
    WAL archive alone doesn't serve a purpose. Without a physical base backup, you can't
    restore a PostgreSQL cluster.

In general, the presence of a WAL archive enhances the resilience of a
PostgreSQL cluster, allowing each instance to fetch any required WAL file from
the archive if needed. (Normally the WAL archive has higher retention periods
than any Postgres instance that normally recycles those files.)

This use case can also be extended to [replica clusters](replica_cluster.md),
as they can rely on the WAL archive to synchronize across long
distances, extending disaster recovery goals across different regions.

When you [configure a WAL archive](wal_archiving.md), CloudNativePG provides
out-of-the-box an RPO &lt;= 5 minutes for disaster recovery, even across regions.

!!! Important
    Our recommendation is to always set up the WAL archive in production.
    There are known use cases, normally involving staging and development
    environments, where none of these benefits are needed and the WAL
    archive isn't necessary. RPO in this case can be any value, such as
    24 hours (daily backups) or infinite (no backup at all).

## Cold and hot backups

Hot backups require the presence of a WAL archive. They are the norm in any modern database management system.

*Cold backups*, also known as offline backups, are instead physical base backups
taken when the PostgreSQL instance (standby or primary) is shut down. They are
consistent per definition and they represent a snapshot of the database at the
time it was shut down.

As a result, PostgreSQL instances can be restarted from a cold backup. A WAL archive isn't needed, even though the instances can take advantage of it if it's 
available, with all the benefits on the recovery side highlighted in [WAL archive](#wal-archive).

In those situations with a higher RPO (for example, 1 hour or 24 hours), and
shorter retention periods, cold backups represent a viable option to consider
for your disaster recovery plans.

## Object stores or volume snapshots: which one to use?

In CloudNativePG, object-store-based backups:

- Always require the WAL archive
- Support hot backup only
- Don't support incremental copy
- Don't support differential copy

Volume snapshots instead:

- Don't require the WAL archive, although in production we always recommend it
- Support incremental copy, depending on the underlying storage classes
- Support differential copy, depending on the underlying storage classes
- Also support cold backup

The one to use depends on your specific requirements and environment,
including:

- Availability of a viable object store solution in your Kubernetes cluster.
- Availability of a trusted storage class that supports volume snapshots.
- Size of the database. With object stores, the larger your database, the
  longer backup and, most importantly, recovery procedures take (the latter
  impacts RTO). In presence of very large databases (VLDB), the general
  advice is to rely on volume snapshots as, because of copy-on-write, they
  provide faster recovery.
- Data mobility and the possibility to store or relay backup files on a
  secondary location in a different region or any subsequent one.
- Other factors, mostly based on the confidence and familiarity with the
  underlying storage solutions.

The table highlights some of the main differences between the two
available methods for storing physical base backups.

|                                   | Object store |   Volume snapshots   |
|-----------------------------------|:------------:|:--------------------:|
| **WAL archiving**                 |   Required   |    Recommended (1)   |
| **Cold backup**                   |      ✗       |           ✓          |
| **Hot backup**                    |      ✓       |           ✓          |
| **Incremental copy**              |      ✗       |         ✓  (2)       |
| **Differential copy**             |      ✗       |         ✓  (2)       |
| **Backup from a standby**         |      ✓       |           ✓          |
| **Snapshot recovery**             |    ✗ (3)     |           ✓          |
| **Point In Time Recovery (PITR)** |      ✓       | Requires WAL archive |
| **Underlying technology**         | Barman Cloud |   Kubernetes API     |


> 1. WAL archive must be on an object store.
> 2. If supported by the underlying storage classes of the PostgreSQL volumes.
> 3. Snapshot recovery can be emulated using the `bootstrap.recovery.recoveryTarget.targetImmediate` option.

## Scheduled backups

We recommend configuring scheduled backups as your backup strategy in
CloudNativePG. Scheduled backups are managed by the `ScheduledBackup` resource.

!!! Info
    See [`ScheduledBackupSpec`](cloudnative-pg.v1.md#postgresql-cnpg-io-v1-ScheduledBackupSpec)
    in the API reference for a full list of options.

The `schedule` field allows you to define a *six-term cron schedule* specification,
which includes seconds, as expressed in
the [Go `cron` package format](https://pkg.go.dev/github.com/robfig/cron#hdr-CRON_Expression_Format).

!!! Warning
    This format also accepts the `seconds` field, which is
    different from the `crontab` format in Unix/Linux systems.

This example shows a scheduled backup. It schedules a backup every day at midnight because the schedule
specifies zero for the second, minute, and hour. It specifies a wildcard, meaning all,
for day of the month, month, and day of the week.

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: ScheduledBackup
metadata:
  name: backup-example
spec:
  schedule: "0 0 0 * * *"
  backupOwnerReference: self
  cluster:
    name: pg-backup
```


In Kubernetes CronJobs, the equivalent expression is `0 0 * * *` because seconds
aren't included.

!!! Hint
    Backup frequency might impact your recovery time object (RTO) after a
    disaster, which requires a full or point-in-time recovery (PITR) operation.
    We recommend that you regularly test your backups by recovering them and then
    measuring the time it takes to recover from scratch. This technique helps you to refine
    your RTO predictability. Recovery time is influenced by the size of the
    base backup and the amount of WAL files that need to be fetched from the archive
    and replayed during recovery. (Remember that WAL archiving is what enables
    continuous backup in PostgreSQL!)
    Based on our experience, a weekly base backup is more than enough for most
    cases. It's rare to schedule backups more frequently than once
    a day.

You can choose whether to schedule a backup on a defined object store or a
volume snapshot using the `.spec.method` attribute. By default, it's set to
`barmanObjectStore`. If you properly defined
[volume snapshots](backup_volumesnapshot.md#how-to-configure-volume-snapshot-backups)
in the `backup` stanza of the cluster, you can set `method: volumeSnapshot`
to start scheduling base backups on volume snapshots.

You can suspend scheduled backups by setting `.spec.suspend: true`.
This setting stops any new backup from being scheduled until you remove the option
or set it to `false`.

If you want to issue a backup as soon as the `ScheduledBackup` resource is created,
you can set `.spec.immediate: true`.

!!! Note
    `.spec.backupOwnerReference` indicates which `ownerReference` to put inside
    the created backup resources.

    - **none** – No owner reference for created backup objects (same behavior as before the field was introduced).
    - **self** – Sets the scheduled backup object as owner of the backup.
    - **cluster** – Set the cluster as owner of the backup.

## On-demand backups

!!! Info
    See [`BackupSpec`](cloudnative-pg.v1.md#postgresql-cnpg-io-v1-BackupSpec)
    in the API reference for a full list of options.

To request a new backup, you need to create a new `Backup` resource
like the following:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Backup
metadata:
  name: backup-example
spec:
  method: barmanObjectStore
  cluster:
    name: pg-backup
```

In this case, the operator starts to orchestrate the cluster to take the
required backup on an object store, using `barman-cloud-backup`. You can check
the backup status using the plain `kubectl describe backup <name>` command:

```text
Name:         backup-example
Namespace:    default
Labels:       <none>
Annotations:  API Version:  postgresql.cnpg.io/v1
Kind:         Backup
Metadata:
  Creation Timestamp:  2020-10-26T13:57:40Z
  Self Link:         /apis/postgresql.cnpg.io/v1/namespaces/default/backups/backup-example
  UID:               ad5f855c-2ffd-454a-a157-900d5f1f6584
Spec:
  Cluster:
    Name:  pg-backup
Status:
  Phase:       running
  Started At:  2020-10-26T13:57:40Z
Events:        <none>
```

When the backup is complete, the phase is `completed`,
as in the following example:

```text
Name:         backup-example
Namespace:    default
Labels:       <none>
Annotations:  API Version:  postgresql.cnpg.io/v1
Kind:         Backup
Metadata:
  Creation Timestamp:  2020-10-26T13:57:40Z
  Self Link:         /apis/postgresql.cnpg.io/v1/namespaces/default/backups/backup-example
  UID:               ad5f855c-2ffd-454a-a157-900d5f1f6584
Spec:
  Cluster:
    Name:  pg-backup
Status:
  Backup Id:         20201026T135740
  Destination Path:  s3://backups/
  Endpoint URL:      http://minio:9000
  Phase:             completed
  s3Credentials:
    Access Key Id:
      Key:   ACCESS_KEY_ID
      Name:  minio
    Secret Access Key:
      Key:      ACCESS_SECRET_KEY
      Name:     minio
  Server Name:  pg-backup
  Started At:   2020-10-26T13:57:40Z
  Stopped At:   2020-10-26T13:57:44Z
Events:         <none>
```

!!!Important
    This feature doesn't back up the secrets for the superuser and the
    application user. The secrets are backed up as part of
    the standard backup procedures for the Kubernetes cluster.

## Backup from a standby

<!-- TODO: Adapt for Volume Snapshots -->
Taking a base backup requires scraping the whole data content of the
PostgreSQL instance on disk, possibly resulting in I/O contention with the
actual workload of the database.

For this reason, CloudNativePG allows you to take advantage of a
feature that's directly available in PostgreSQL: back up from a standby.

By default, backups run on the most aligned replica of a cluster. If
no replicas are available, backups run on the primary instance.

!!! Info
    Although the standby might not always be up to date with the primary,
    in the time continuum from the first available backup to the last
    archived WAL, this is normally irrelevant. The base backup indeed
    represents the starting point from which to begin a recovery operation,
    including PITR. Similarly to what happens with
    [`pg_basebackup`](https://www.postgresql.org/docs/current/app-pgbasebackup.html),
    when backing up from an online standby, we don't force a switch of the WAL on the
    primary. This might produce unexpected results in the short term (before
    `archive_timeout` kicks in) in deployments with low write activity.

If you prefer to always run backups on the primary, you can set the backup
target to `primary` as this example shows:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  [...]
spec:
  backup:
    target: "primary"
```

!!! Warning
    Beware of setting the target to primary when performing a cold backup
    with volume snapshots, as this shuts down the primary for
    the time needed to take the snapshot, which affects write operations.
    This warning also applies to taking a cold backup in a single-instance cluster, even
    if you didn't explicitly set the primary as the target.

When the backup target is set to `prefer-standby`, such a policy ensures
backups run on the most up-to-date available secondary instance. If no
other instance is available, backups run on the primary instance.

By default, when not otherwise specified, the target is automatically set to take
backups from a standby.

The backup target specified in the cluster can be overridden in the `Backup`
and `ScheduledBackup` types, as shown in the following example:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Backup
metadata:
  [...]
spec:
  cluster:
    name: [...]
  target: "primary"
```

In this example, CloudNativePG chooses the primary
instance even if the cluster is set to prefer replicas.
