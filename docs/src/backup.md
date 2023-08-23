# Backup

PostgreSQL natively provides first class backup and recovery capabilities based
on file system level (physical) copy. These have been successfully used for
more than 15 years in mission critical production databases, helping
organizations all over the world implement their disaster recovery solutions.

!!! Note
    PostgreSQL provides another way of backing up databases, based on logical
    backups, through the `pg_dump` utility. However, logical backups are not
    suitable for business continuity use cases and are not covered in this section.
    You can use the `pg_dump` utility if needed, as explained in the
    ["Troubleshooting / Emergency backup" section](troubleshooting.md#emergency-backup).

In CloudNativePG, the backup infrastructure for each PostgreSQL cluster is made
up of the following resources:

- **WAL archive**: a location containing the WAL files (transactional logs)
  that are continuously written by Postgres and archived for data durability
- **Physical base backups**: a copy of all the files that PostgreSQL uses to
  store the data in the database (primarily the `PGDATA` and any tablespace)

The WAL archive can only be stored on object stores at the moment.

On the other hand, CloudNativePG supports two ways to store physical base backups:

- on [object stores](backup_barmanobjectstore.md), as tarballs optionally
  compressed
- on [Kubernetes Volume Snapshots](backup_volumesnapshot.md), if supported by
  the underlying storage class

!!! Important
    Before choosing your backup strategy with CloudNativePG, it is important you
    take some time to familiarize with some basic concepts, like WAL archive, hot
    and cold backups.

## WAL archive

The WAL archive in PostgreSQL is fundamental for the following reasons:

- **hot backups**: the possibility to take physical base backups from any
  instance in the Postgres cluster (either primary or standby) without shutting
  down the server; they are also known as online backups 
- **Point in Time recovery** (PITR): to possibility to recover at any point in
  time from the first available base backup in your system

In general, the presence of a WAL archive enhances the resilience of a
PostgreSQL cluster, allowing each instance to fetch any required WAL file from
the archive if needed (normally the WAL archive has higher retention periods
than any Postgres instance that normally recycles those files).

This use case can also be extended to [replica clusters](replica_cluster.md),
as they can simply rely on the WAL archive to synchronize across long
distances, extending disaster recovery goals across different regions.

Finally, if configured, CloudNativePG provides out-of-the-box an RPO <= 5
minutes for disaster recovery, even across regions.

Our recommendation is to always setup the WAL archive, unless you are happy
to miss all of the above capabilities (e.g. development purposes).

## Cold and Hot backups

Hot backups have already been defined in the previous section. They require the
presence of a WAL archive.

**Cold backups**, also known as offline backups, are a physical base backup
taken when the PostgreSQL instance (standby or primary) is shut down. They are
consistent per definition and they represent a snapshot of the database at the
time it was shut down.

As a result, they do not require the WAL archive to be restarted, but they can
take advantage of it if available (with all the benefits for the recovery
explained in the previous section).

In those situations with higher RPOs (for example, 1 hour or 24 hours), and
shorter retention periods, cold backups represent a viable option to be considered
for your disaster recovery plans.

## Object stores or volume snapshots: which one to use?

In CloudNativePG, object store based backups:

- require the WAL archive
- support hot backup only
- don't support incremental copy
- don't support differential copy

VolumeSnapshots instead:

- don't require the WAL archive
- support cold backup only (currently)
- support incremental copy, depending on the underlying storage classes
- support differential copy, depending on the underlying storage classes
- open up for consistent database snapshots (cold backup based disaster
  recovery scenarios with no point in time recovery and higher RPOs) 

## On-demand backups

<!-- TODO: Adapt for Volume Snapshots -->
To request a new backup, you need to create a new Backup resource
like the following one:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Backup
metadata:
  name: backup-example
spec:
  cluster:
    name: pg-backup
```

The operator will start to orchestrate the cluster to take the
required backup using `barman-cloud-backup`. You can check
the backup status using the plain `kubectl describe backup <name>`
command:

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

## Scheduled backups

<!-- TODO: Adapt for Volume Snapshots -->
You can also schedule your backups periodically by creating a
resource named `ScheduledBackup`. The latter is similar to a
`Backup` but with an added field, called `schedule`.

This field is a *cron schedule* specification, which follows the same
[format used in Kubernetes CronJobs](https://pkg.go.dev/github.com/robfig/cron#hdr-CRON_Expression_Format).

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

The above example will schedule a backup every day at midnight.

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
    when backing up from a standby we do not force a switch of the WAL on the
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

