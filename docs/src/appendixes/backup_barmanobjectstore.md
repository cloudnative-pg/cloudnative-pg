---
id: backup_barmanobjectstore
title: Appendix B - Backup on object stores
---

# Appendix B - Backup on object stores

<!-- SPDX-License-Identifier: CC-BY-4.0 -->

:::warning
    As of CloudNativePG 1.26, **native Barman Cloud support is deprecated** in
    favor of the **Barman Cloud Plugin**. This page has been moved to the appendix
    for reference purposes. While the native integration remains functional for
    now, we strongly recommend beginning a gradual migration to the plugin-based
    interface after appropriate testing.  For guidance, see
    [Migrating from Built-in CloudNativePG Backup](https://cloudnative-pg.io/plugin-barman-cloud/docs/migration/).
:::

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

:::info[Important]
    Always ensure that you are running the latest version of the operands
    in your system to take advantage of the improvements introduced in
    Barman cloud (as well as improve the security aspects of your cluster).
:::

:::warning[Changes in Barman Cloud 3.16+ and Bucket Creation]
    Starting with Barman Cloud 3.16, most Barman Cloud commands no longer
    automatically create the target bucket, assuming it already exists. Only the
    `barman-cloud-check-wal-archive` command creates the bucket now. Whenever this
    is not the first operation run on an empty bucket, CloudNativePG will throw an
    error. As a result, to ensure reliable, future-proof operations and avoid
    potential issues, we strongly recommend that you create and configure your
    object store bucket *before* creating a `Cluster` resource that references it.
:::

A backup is performed from a primary or a designated primary instance in a
`Cluster` (please refer to
[replica clusters](../replica_cluster.md)
for more information about designated primary instances), or alternatively
on a [standby](../backup.md#backup-from-a-standby).

## Common object stores

If you are looking for a specific object store such as
[AWS S3](object_stores.md#aws-s3),
[Microsoft Azure Blob Storage](object_stores.md#azure-blob-storage),
[Google Cloud Storage](object_stores.md#google-cloud-storage), or a compatible
provider, please refer to [Appendix C - Common object stores for backups](object_stores.md).

## WAL archiving

WAL archiving is the process that feeds a [WAL archive](../backup.md#wal-archive)
in CloudNativePG.

The WAL archive is defined in the `.spec.backup.barmanObjectStore` stanza of
a `Cluster` resource.

:::info
    Please refer to [`BarmanObjectStoreConfiguration`](https://pkg.go.dev/github.com/cloudnative-pg/barman-cloud/pkg/api#BarmanObjectStoreConfiguration)
    in the barman-cloud API for a full list of options.
:::

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

:::info[Important]
    By default, CloudNativePG sets `archive_timeout` to `5min`, ensuring
    that WAL files, even in case of low workloads, are closed and archived
    at least every 5 minutes, providing a deterministic time-based value for
    your Recovery Point Objective ([RPO](../before_you_start.md#rpo)). Even though you change the value
    of the [`archive_timeout` setting in the PostgreSQL configuration](https://www.postgresql.org/docs/current/runtime-config-wal.html#GUC-ARCHIVE-TIMEOUT),
    our experience suggests that the default value set by the operator is
    suitable for most use cases.
:::

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

:::note[There's more ...]
    The **recovery window retention policy** is focused on the concept of
    *Point of Recoverability* (`PoR`), a moving point in time determined by
    `current time - recovery window`. The *first valid backup* is the first
    available backup before `PoR` (in reverse chronological order).
    CloudNativePG must ensure that we can recover the cluster at
    any point in time between `PoR` and the latest successfully archived WAL
    file, starting from the first valid backup. Base backups that are older
    than the first valid backup will be marked as *obsolete* and permanently
    removed after the next backup is completed.
:::

## Compression algorithms

CloudNativePG by default archives backups and WAL files in an
uncompressed fashion. However, it also supports the following compression
algorithms via `barman-cloud-backup` (for backups) and
`barman-cloud-wal-archive` (for WAL files):

* bzip2
* gzip
* lz4
* snappy
* xz
* zstd

The compression settings for backups and WALs are independent. See the
[DataBackupConfiguration](https://pkg.go.dev/github.com/cloudnative-pg/barman-cloud/pkg/api#DataBackupConfiguration) and
[WALBackupConfiguration](https://pkg.go.dev/github.com/cloudnative-pg/barman-cloud/pkg/api#WalBackupConfiguration) sections in
the barman-cloud API reference.

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

* `tags`: key-value pair tags to be added to backup objects and archived WAL
  file in the backup object store
* `historyTags`: key-value pair tags to be added to archived history files in
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

## Extra options for the backup and WAL commands

You can append additional options to the `barman-cloud-backup` and `barman-cloud-wal-archive` commands by using
the `additionalCommandArgs` property in the
`.spec.backup.barmanObjectStore.data` and `.spec.backup.barmanObjectStore.wal` sections respectively.
These properties are lists of strings that will be appended to the
`barman-cloud-backup` and `barman-cloud-wal-archive` commands.

For example, you can use the `--read-timeout=60` to customize the connection
reading timeout.

For additional options supported by `barman-cloud-backup` and `barman-cloud-wal-archive` commands you can refer to the
official barman documentation [here](https://www.pgbarman.org/documentation/).

If an option provided in `additionalCommandArgs` is already present in the
declared options in its section (`.spec.backup.barmanObjectStore.data` or `.spec.backup.barmanObjectStore.wal`), the extra option will be
ignored.

The following is an example of how to use this property:

For backups:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
[...]
spec:
  backup:
    barmanObjectStore:
      [...]
      data:
        additionalCommandArgs:
        - "--min-chunk-size=5MB"
        - "--read-timeout=60"
```

For WAL files:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
[...]
spec:
  backup:
    barmanObjectStore:
      [...]
      wal:
        additionalCommandArgs:
        - "--max-concurrency=1"
        - "--read-timeout=60"
```

## Recovery from an object store

You can recover from a backup created by Barman Cloud and stored on a supported
object store. After you define the external cluster, including all the required
configuration in the `barmanObjectStore` section, you need to reference it in
the `.spec.recovery.source` option.

This example defines a recovery object store in a blob container in Azure:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-restore
spec:
  [...]

  superuserSecret:
    name: superuser-secret

  bootstrap:
    recovery:
      source: clusterBackup

  externalClusters:
    - name: clusterBackup
      barmanObjectStore:
        destinationPath: https://STORAGEACCOUNTNAME.blob.core.windows.net/CONTAINERNAME/
        azureCredentials:
          storageAccount:
            name: recovery-object-store-secret
            key: storage_account_name
          storageKey:
            name: recovery-object-store-secret
            key: storage_account_key
        wal:
          maxParallel: 8
```

The previous example assumes that the application database and its owning user
are named `app` by default. If the PostgreSQL cluster being restored uses
different names, you must specify these names before exiting the recovery phase,
as documented in ["Configure the application database"](../recovery.md#configure-the-application-database).

:::info[Important]
    By default, the `recovery` method strictly uses the `name` of the
    cluster in the `externalClusters` section as the name of the main folder
    of the backup data within the object store. This name is normally reserved
    for the name of the server. You can specify a different folder name
    using the `barmanObjectStore.serverName` property.
:::

:::note
    This example takes advantage of the parallel WAL restore feature,
    dedicating up to 8 jobs to concurrently fetch the required WAL files from the
    archive. This feature can appreciably reduce the recovery time. Make sure that
    you plan ahead for this scenario and correctly tune the value of this parameter
    for your environment. It will make a difference when you need it, and you will.
:::