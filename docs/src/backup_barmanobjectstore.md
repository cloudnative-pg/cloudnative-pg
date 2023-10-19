# Backup on object stores

CloudNativePG natively supports *online/hot backup* of PostgreSQL
clusters through continuous physical backup and WAL archiving on an object
store. This means that the database is always up (no downtime required)
and that point-in-time recovery (PITR) is available.

The operator can orchestrate a continuous backup infrastructure
that's based on the [Barman Cloud](https://pgbarman.org) tool. Instead
of using the classic architecture with a Barman server, which
backs up many PostgreSQL instances, the operator relies on the
`barman-cloud-wal-archive`, `barman-cloud-check-wal-archive`,
`barman-cloud-backup`, `barman-cloud-backup-list`, and
`barman-cloud-backup-delete` tools. As a result, base backups are
*tarballs*, which are sets of packaged files. Both base backups and WAL files can be compressed
and encrypted.

For this operation, you need to use an image with `barman-cli-cloud` included.
You can use the image `ghcr.io/cloudnative-pg/postgresql` for this scope,
as it's composed of a community PostgreSQL image and the latest
`barman-cli-cloud` package.

!!! Important
    Always ensure that you're running the latest version of the operands
    in your system to take advantage of the improvements introduced in
    Barman cloud and to improve the security aspects of your cluster.

A backup is performed from a primary or a designated primary instance in a
cluster (see [replica clusters](replica_cluster.md).
Alternatively perform a backup
on a [standby](#backup-from-a-standby).

## Common object stores

If you're looking for a specific object store, such as
[AWS S3](appendixes/object_stores.md#aws-s3),
[Microsoft Azure Blob Storage](appendixes/object_stores.md#azure-blob-storage),
[Google Cloud Storage](appendixes/object_stores.md#google-cloud-storage),
[MinIO Gateway](appendixes/object_stores.md#minio-gateway), or a compatible
provider, see [Common object stores](appendixes/object_stores.md).

## Retention policies

!!! Important
    Retention policies aren't currently available on volume snapshots.

CloudNativePG can manage the automated deletion of backup files from
the backup object store, using retention policies based on the recovery
window.

Internally, the retention policy feature uses `barman-cloud-backup-delete`
with `--retention-policy “RECOVERY WINDOW OF {{ retention policy value }} {{ retention policy unit }}”`.

For example, you can define your backups with a retention policy of 30 days:

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
    The recovery window retention policy is focused on the concept of
    *point of recoverability* (PoR), a moving point in time determined by
    `current time - recovery window`. The *first valid backup* is the first
    available backup before PoR (in reverse chronological order).
    CloudNativePG must ensure that it can recover the cluster at
    any point in time between PoR and the latest successfully archived WAL
    file, starting from the first valid backup. Base backups that are older
    than the first valid backup are marked as obsolete and permanently
    removed after the next backup is completed.

## Compression algorithms

By default, CloudNativePG archives backups and WAL files in an
uncompressed fashion. However, it also supports the following compression
algorithms by way of `barman-cloud-backup` (for backups) and
`barman-cloud-wal-archive` (for WAL files):

* bzip2
* gzip
* snappy

The compression settings for backups and WALs are independent. See
[DataBackupConfiguration](cloudnative-pg.v1.md#postgresql-cnpg-io-v1-DataBackupConfiguration) and
[WALBackupConfiguration](cloudnative-pg.v1.md#postgresql-cnpg-io-v1-WalBackupConfiguration) in
the API reference.

!!! Important 
    Archival time, restore time, and size change between the algorithms. Choose the compression algorithm according to your use case.

The Barman team performed an evaluation of the performance of the supported
algorithms for Barman Cloud. The table summarizes a scenario where a
backup is taken on a local MinIO deployment. The Barman GitHub project includes
a [deeper analysis](https://github.com/EnterpriseDB/barman/issues/344#issuecomment-992547396).

| Compression | Backup time (ms) | Restore time (ms) | Uncompressed size (MB) | Compressed size (MB)  | Approx ratio |
|-------------|------------------|-------------------|------------------------|-----------------------|--------------|
| None        | 10927            | 7553              | 395                    | 395                   | 1:1          |
| bzip2       | 25404            | 13886             | 395                    | 67                    | 5.9:1        |
| gzip        | 116281           | 3077              | 395                    | 91                    | 4.3:1        |
| snappy      | 8134             | 8341              | 395                    | 166                   | 2.4:1        |

## Tagging of backup objects

Barman 2.18 introduces support for tagging backup resources when saving them in
object stores using `barman-cloud-backup` and `barman-cloud-wal-archive`. As a
result, if your PostgreSQL container image includes Barman with version 2.18 or
later, CloudNativePG enables you to specify tags as key-value pairs
for backup objects, namely base backups, WAL files, and history files.

You can use two properties in the `.spec.backup.barmanObjectStore` definition:

- `tags` – Key-value pair tags to add to backup objects and archived WAL
  file in the backup object store.
- `historyTags` – Key-value pair tags to add to archived history files in
  the backup object store.

This excerpt of a YAML manifest shows an example of this
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
