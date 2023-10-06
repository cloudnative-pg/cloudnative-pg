# Backup

!!! Important
    With version 1.21, backup and recovery capabilities in CloudNativePG
    have sensibly changed due to the introduction of native support for
    [Kubernetes Volume Snapshots](backup_volumesnapshot.md).
    Up to that point, backup and recovery were available only for object
    stores. Please carefully read this section and the [recovery](recovery.md)
    one if you have been a user of CloudNativePG 1.15 through 1.20.

PostgreSQL natively provides first class backup and recovery capabilities based
on file system level (physical) copy. These have been successfully used for
more than 15 years in mission critical production databases, helping
organizations all over the world achieve their disaster recovery goals with
Postgres.

!!! Note
    There's another way to backup databases in PostgreSQL, through the
    `pg_dump` utility - which relies on logical backups instead of physical ones.
    However, logical backups are not suitable for business continuity use cases
    and as such are not covered by CloudNativePG (yet, at least).
    If you want to use the `pg_dump` utility, let yourself be inspired by the
    ["Troubleshooting / Emergency backup" section](troubleshooting.md#emergency-backup).

In CloudNativePG, the backup infrastructure for each PostgreSQL cluster is made
up of the following resources:

- **WAL archive**: a location containing the WAL files (transactional logs)
  that are continuously written by Postgres and archived for data durability
- **Physical base backups**: a copy of all the files that PostgreSQL uses to
  store the data in the database (primarily the `PGDATA` and any tablespace)

The WAL archive can only be stored on object stores at the moment.

On the other hand, CloudNativePG supports two ways to store physical base backups:

- on [object stores](backup_barmanobjectstore.md), as tarballs - optionally
  compressed
- on [Kubernetes Volume Snapshots](backup_volumesnapshot.md), if supported by
  the underlying storage class

!!! Important
    Before choosing your backup strategy with CloudNativePG, it is important that
    you take some time to familiarize with some basic concepts, like WAL archive,
    hot and cold backups.

## WAL archive

The WAL archive in PostgreSQL is at the heart of **continuous backup**, and it
is fundamental for the following reasons:

- **Hot backups**: the possibility to take physical base backups from any
  instance in the Postgres cluster (either primary or standby) without shutting
  down the server; they are also known as online backups 
- **Point in Time recovery** (PITR): to possibility to recover at any point in
  time from the first available base backup in your system

!!! Warning
    WAL archive alone is useless. Without a physical base backup, you cannot
    restore a PostgreSQL cluster.

In general, the presence of a WAL archive enhances the resilience of a
PostgreSQL cluster, allowing each instance to fetch any required WAL file from
the archive if needed (normally the WAL archive has higher retention periods
than any Postgres instance that normally recycles those files).

This use case can also be extended to [replica clusters](replica_cluster.md),
as they can simply rely on the WAL archive to synchronize across long
distances, extending disaster recovery goals across different regions.

When you [configure a WAL archive](wal_archiving.md), CloudNativePG provides
out-of-the-box an RPO <= 5 minutes for disaster recovery, even across regions.

!!! Important
    Our recommendation is to always setup the WAL archive in production.
    There are known use cases - normally involving staging and development
    environments - where none of the above benefits are needed and the WAL
    archive is not necessary. RPO in this case can be any value, such as
    24 hours (daily backups) or infinite (no backup at all).

## Cold and Hot backups

Hot backups have already been defined in the previous section. They require the
presence of a WAL archive and they are the norm in any modern database management
system.

**Cold backups**, also known as offline backups, are instead physical base backups
taken when the PostgreSQL instance (standby or primary) is shut down. They are
consistent per definition and they represent a snapshot of the database at the
time it was shut down.

As a result, PostgreSQL instances can be restarted from a cold backup without
the need of a WAL archive, even though they can take advantage of it, if
available (with all the benefits on the recovery side highlighted in the
previous section).

In those situations with a higher RPO (for example, 1 hour or 24 hours), and
shorter retention periods, cold backups represent a viable option to be considered
for your disaster recovery plans.

## Object stores or volume snapshots: which one to use?

In CloudNativePG, object store based backups:

- always require the WAL archive
- support hot backup only
- don't support incremental copy
- don't support differential copy

VolumeSnapshots instead:

- don't require the WAL archive, although in production it is always recommended
- support cold backup only (currently)
- support incremental copy, depending on the underlying storage classes
- support differential copy, depending on the underlying storage classes

Which one to use depends on your specific requirements and environment,
including:

- availability of a viable object store solution in your Kubernetes cluster
- availability of a trusted storage class that supports volume snapshots
- size of the database: with object stores, the larger your database, the
  longer backup and, most importantly, recovery procedures take (the latter
  impacts RTO); in presence of Very Large Databases (VLDB), the general
  advice is to rely on Volume Snapshots as, thanks to copy-on-write, they
  provide faster recovery
- data mobility and possibility to store or relay backup files on a
  secondary location in a different region, or any subsequent one
- other factors, mostly based on the confidence and familiarity with the
  underlying storage solutions

The summary table below highlights some of the main differences between the two
available methods for storing physical base backups.

|                                   | Object store |   Volume Snapshots   |
|-----------------------------------|:------------:|:--------------------:|
| **WAL archiving**                 |   Required   |    Recommended (1)   |
| **Cold backup**                   |      𐄂       |           ✓          |
| **Hot backup**                    |       ✓      |        𐄂  (2)        |
| **Incremental copy**              |      𐄂       |         ✓  (3)       |
| **Differential copy**             |      𐄂       |         ✓  (3)       |
| **Backup from a standby**         |       ✓      |           ✓          |
| **Snapshot recovery**             |    𐄂 (4)     |           ✓          |
| **Point In Time Recovery (PITR)** |       ✓      | Requires WAL archive |
| **Underlying technology**         | Barman Cloud |   Kubernetes API     |


> See the explanation below for the notes in the above table:
>
> 1. WAL archive must be on an object store
> 2. Hot backup is not available yet for volume snapshots, and it is planned
> for version 1.22; however, considering that cold backup is taken by fencing
> temporarily a standby, the operation does not induce any downtime for your
> write applications
> 3. If supported by the underlying storage classes of the PostgreSQL volumes
> 4. Snapshot recovery can be emulated using the `bootstrap.recovery.recoveryTarget.targetImmediate` option

## Scheduled backups

Scheduled backups are the recommended way to configure your backup strategy in
CloudNativePG. They are managed by the `ScheduledBackup` resource.

!!! Info
    Please refer to [`ScheduledBackupSpec`](cloudnative-pg.v1.md#postgresql-cnpg-io-v1-ScheduledBackupSpec)
    in the API reference for a full list of options.

The `schedule` field allows you to define a *six-term cron schedule* specification,
which includes seconds, as expressed in
the [Go `cron` package format](https://pkg.go.dev/github.com/robfig/cron#hdr-CRON_Expression_Format).

!!! Warning
    Beware that this format accepts also the `seconds` field, and it is
    different from the `crontab` format in Unix/Linux systems.

This is an example of a scheduled backup:

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

The above example will schedule a backup every day at midnight because the schedule
specifies zero for the second, minute, and hour, while specifying wildcard, meaning all,
for day of the month, month, and day of the week.

In Kubernetes CronJobs, the equivalent expression is `0 0 * * *` because seconds
are not included.

!!! Hint
    Backup frequency might impact your recovery time object (RTO) after a
    disaster which requires a full or Point-In-Time recovery operation. Our
    advice is that you regularly test your backups by recovering them, and then
    measuring the time it takes to recover from scratch so that you can refine
    your RTO predictability. Recovery time is influenced by the size of the
    base backup and the amount of WAL files that need to be fetched from the archive
    and replayed during recovery (remember that WAL archiving is what enables
    continuous backup in PostgreSQL!).
    Based on our experience, a weekly base backup is more than enough for most
    cases - while it is extremely rare to schedule backups more frequently than once
    a day.

You can choose whether to schedule a backup on a defined object store or a
volume snapshot via the `.spec.method` attribute, by default set to
`barmanObjectStore`. If you have properly defined
[volume snapshots](backup_volumesnapshot.md#how-to-configure-volume-snapshot-backups)
in the `backup` stanza of the cluster, you can set `method: volumeSnapshot`
to start scheduling base backups on volume snapshots.

ScheduledBackups can be suspended if needed by setting `.spec.suspend: true`,
this will stop any new backup to be scheduled as long as the option is set to false.

In case you want to issue a backup as soon as the ScheduledBackup resource is created
you can set `.spec.immediate: true`.

!!! Note
    `.spec.backupOwnerReference` indicates which ownerReference should be put inside
    the created backup resources.

    - *none:* no owner reference for created backup objects (same behavior as before the field was introduced)
    - *self:* sets the Scheduled backup object as owner of the backup
    - *cluster:* set the cluster as owner of the backup

## On-demand backups

!!! Info
    Please refer to [`BackupSpec`](cloudnative-pg.v1.md#postgresql-cnpg-io-v1-BackupSpec)
    in the API reference for a full list of options.

To request a new backup, you need to create a new `Backup` resource
like the following one:

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

In this case, the operator will start to orchestrate the cluster to take the
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

When the backup has been completed, the phase will be `completed`
like in the following example:

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
    This feature will not backup the secrets for the superuser and the
    application user. The secrets are supposed to be backed up as part of
    the standard backup procedures for the Kubernetes cluster.

## Backup from a standby

<!-- TODO: Adapt for Volume Snapshots -->
Taking a base backup requires to scrape the whole data content of the
PostgreSQL instance on disk, possibly resulting in I/O contention with the
actual workload of the database.

For this reason, CloudNativePG allows you to take advantage of a
feature which is directly available in PostgreSQL: **backup from a standby**.

By default, backups will run on the most aligned replica of a `Cluster`. If
no replicas are available, backups will run on the primary instance.

!!! Info
    Although the standby might not always be up to date with the primary,
    in the time continuum from the first available backup to the last
    archived WAL this is normally irrelevant. The base backup indeed
    represents the starting point from which to begin a recovery operation,
    including PITR. Similarly to what happens with
    [`pg_basebackup`](https://www.postgresql.org/docs/current/app-pgbasebackup.html),
    when backing up from an online standby we do not force a switch of the WAL on the
    primary. This might produce unexpected results in the short term (before
    `archive_timeout` kicks in) in deployments with low write activity.

If you prefer to always run backups on the primary, you can set the backup
target to `primary` as outlined in the example below:

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
    strategy on volume snapshots, as this will shut down the primary for
    the time needed to take the snapshot, impacting write operations.

When the backup target is set to `prefer-standby`, such policy will ensure
backups are run on the most up-to-date available secondary instance, or if no
other instance is available, on the primary instance.

By default, when not otherwise specified, target is automatically set to take
backups from a standby.

The backup target specified in the `Cluster` can be overridden in the `Backup`
and `ScheduledBackup` types, like in the following example:

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

In the previous example, CloudNativePG will invariably choose the primary
instance even if the `Cluster` is set to prefer replicas.

