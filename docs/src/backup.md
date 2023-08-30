# Backup

<!-- TODO:

- Explain the two methods: object store and volume snapshots
- Explain the role of WAL archiving for both cold/hot backups and PITR

-->

CloudNativePG natively supports **online/hot backup** of PostgreSQL
clusters through continuous physical backup and WAL archiving.
This means that the database is always up (no downtime required)
and that you can recover at any point in time from the first
available base backup in your system. The latter is normally
referred to as "Point In Time Recovery" (PITR).

The operator can orchestrate a continuous backup infrastructure
that is based on the [Barman](https://pgbarman.org) tool. Instead
of using the classical architecture with a Barman server, which
backs up many PostgreSQL instances, the operator relies on the
`barman-cloud-wal-archive`, `barman-cloud-check-wal-archive`,
`barman-cloud-backup`, `barman-cloud-backup-list`, and
`barman-cloud-backup-delete` tools. As a result, base backups will
be *tarballs*. Both base backups and WAL files can be compressed
and encrypted.

For this, it is required to use an image with `barman-cli-cloud` included.
You can use the image `ghcr.io/cloudnative-pg/postgresql` for this scope,
as it is composed of a community PostgreSQL image and the latest
`barman-cli-cloud` package.

!!! Important
    Always ensure that you are running the latest version of the operands
    in your system to take advantage of the improvements introduced in
    Barman cloud (as well as improve the security aspects of your cluster).

A backup is performed from a primary or a designated primary instance in a
`Cluster` (please refer to
[replica clusters](replica_cluster.md)
for more information about designated primary instances), or alternatively
on a [standby](#backup-from-a-standby).

## Common object stores

If you are looking for a specific object store such as
[AWS S3](appendixes/object_stores.md#aws-s3),
[Microsoft Azure Blob Storage](appendixes/object_stores.md#azure-blob-storage),
[Google Cloud Storage](appendixes/object_stores.md#google-cloud-storage), or
[MinIO Gateway](appendixes/object_stores.md#minio-gateway), or a compatible
provider, please refer to [Appendix A - Common object stores](appendixes/object_stores.md).

## On-demand backups

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

## WAL archiving

WAL archiving is enabled as soon as you choose a destination path
and you configure your cloud credentials.

If required, you can choose to compress WAL files as soon as they
are uploaded and/or encrypt them:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
[...]
spec:
  backup:
    barmanObjectStore:
      [...]
      wal:
        compression: gzip
        encryption: AES256
```

You can configure the encryption directly in your bucket, and the operator
will use it unless you override it in the cluster configuration.

PostgreSQL implements a sequential archiving scheme, where the
`archive_command` will be executed sequentially for every WAL
segment to be archived.

!!! Important
    By default, CloudNativePG sets `archive_timeout` to `5min`, ensuring
    that WAL files, even in case of low workloads, are closed and archived
    at least every 5 minutes, providing a deterministic time-based value for
    your Recovery Point Objective (RPO). Even though you change the value
    of the [`archive_timeout` setting in the PostgreSQL configuration](https://www.postgresql.org/docs/current/runtime-config-wal.html#GUC-ARCHIVE-TIMEOUT),
    our experience suggests that the default value set by the operator is
    suitable for most use cases.

When the bandwidth between the PostgreSQL instance and the object
store allows archiving more than one WAL file in parallel, you
can use the parallel WAL archiving feature of the instance manager
like in the following example:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
[...]
spec:
  backup:
    barmanObjectStore:
      [...]
      wal:
        compression: gzip
        maxParallel: 8
        encryption: AES256
```

In the previous example, the instance manager optimizes the WAL
archiving process by archiving in parallel at most eight ready
WALs, including the one requested by PostgreSQL.

When PostgreSQL will request the archiving of a WAL that has
already been archived by the instance manager as an optimization,
that archival request will be just dismissed with a positive status.

## Backup from a standby

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

## Retention policies

CloudNativePG can manage the automated deletion of backup files from
the backup object store, using **retention policies** based on the recovery
window.

Internally, the retention policy feature uses `barman-cloud-backup-delete`
with `--retention-policy “RECOVERY WINDOW OF {{ retention policy value }} {{ retention policy unit }}”`.

For example, you can define your backups with a retention policy of 30 days as
follows:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
[...]
spec:
  backup:
    barmanObjectStore:
      destinationPath: "<destination path here>"
      s3Credentials:
        accessKeyId:
          name: aws-creds
          key: ACCESS_KEY_ID
        secretAccessKey:
          name: aws-creds
          key: ACCESS_SECRET_KEY
    retentionPolicy: "30d"
```

!!! Note "There's more ..."
    The **recovery window retention policy** is focused on the concept of
    *Point of Recoverability* (`PoR`), a moving point in time determined by
    `current time - recovery window`. The *first valid backup* is the first
    available backup before `PoR` (in reverse chronological order).
    CloudNativePG must ensure that we can recover the cluster at
    any point in time between `PoR` and the latest successfully archived WAL
    file, starting from the first valid backup. Base backups that are older
    than the first valid backup will be marked as *obsolete* and permanently
    removed after the next backup is completed.

## Compression algorithms

CloudNativePG by default archives backups and WAL files in an
uncompressed fashion. However, it also supports the following compression
algorithms via `barman-cloud-backup` (for backups) and
`barman-cloud-wal-archive` (for WAL files):

* bzip2
* gzip
* snappy

The compression settings for backups and WALs are independent. See the
[DataBackupConfiguration](cloudnative-pg.v1.md#postgresql-cnpg-io-v1-DataBackupConfiguration) and
[WALBackupConfiguration](cloudnative-pg.v1.md#postgresql-cnpg-io-v1-WalBackupConfiguration) sections in
the API reference.

It is important to note that archival time, restore time, and size change
between the algorithms, so the compression algorithm should be chosen according
to your use case.

The Barman team has performed an evaluation of the performance of the supported
algorithms for Barman Cloud. The following table summarizes a scenario where a
backup is taken on a local MinIO deployment. The Barman GitHub project includes
a [deeper analysis](https://github.com/EnterpriseDB/barman/issues/344#issuecomment-992547396).

| Compression | Backup Time (ms) | Restore Time (ms) | Uncompressed size (MB) | Compressed size (MB)  | Approx ratio |
|-------------|------------------|-------------------|------------------------|-----------------------|--------------|
| None        | 10927            | 7553              | 395                    | 395                   | 1:1          |
| bzip2       | 25404            | 13886             | 395                    | 67                    | 5.9:1        |
| gzip        | 116281           | 3077              | 395                    | 91                    | 4.3:1        |
| snappy      | 8134             | 8341              | 395                    | 166                   | 2.4:1        |

## Tagging of backup objects

Barman 2.18 introduces support for tagging backup resources when saving them in
object stores via `barman-cloud-backup` and `barman-cloud-wal-archive`. As a
result, if your PostgreSQL container image includes Barman with version 2.18 or
higher, CloudNativePG enables you to specify tags as key-value pairs
for backup objects, namely base backups, WAL files and history files.

You can use two properties in the `.spec.backup.barmanObjectStore` definition:

- `tags`: key-value pair tags to be added to backup objects and archived WAL
  file in the backup object store
- `historyTags`: key-value pair tags to be added to archived history files in
  the backup object store

The excerpt of a YAML manifest below provides an example of usage of this
feature:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
[...]
spec:
  backup:
    barmanObjectStore:
      [...]
      tags:
        backupRetentionPolicy: "expire"
      historyTags:
        backupRetentionPolicy: "keep"
```
