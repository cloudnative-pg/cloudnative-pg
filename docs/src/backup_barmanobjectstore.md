# Backup on object stores

CloudNativePG natively supports **online/hot backup** of PostgreSQL
clusters through continuous physical backup and WAL archiving on an object
store. This means that the database is always up (no downtime required)
and that Point In Time Recovery is available.

The operator can orchestrate a continuous backup infrastructure
that is based on the [Barman Cloud](https://pgbarman.org) tool. Instead
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

## Retention policies

!!! Important
    Retention policies are not currently available on volume snapshots.

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
[DataBackupConfiguration](api_reference.md#DataBackupConfiguration) and
[WALBackupConfiguration](api_reference.md#WalBackupConfiguration) sections in
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
