# WAL archiving

WAL archiving is the process that feeds a [WAL archive](backup.md#wal-archive)
in CloudNativePG.

!!! Important
    CloudNativePG currently only supports WAL archives on object stores. Such
    WAL archives serve for both object store backups and volume snapshot backups.

The WAL archive is defined in the `.spec.backup.barmanObjectStore` stanza of
a `Cluster` resource. Use the instructions in
[Backup on object stores](backup_barmanobjectstore.md) to set up
the WAL archive.

!!! Info
    See [`BarmanObjectStoreConfiguration`](cloudnative-pg.v1.md#postgresql-cnpg-io-v1-barmanobjectstoreconfiguration)
    in the API reference for a full list of options.

If required, you can choose to compress WAL files as soon as they're
uploaded or encrypt them:

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
uses it unless you override it in the cluster configuration.

PostgreSQL implements a sequential archiving scheme, where the
`archive_command` is executed sequentially for every WAL
segment to be archived.

!!! Important
    By default, CloudNativePG sets `archive_timeout` to `5min`, ensuring
    that WAL files, even in case of low workloads, are closed and archived
    at least every 5 minutes. This approach provides a deterministic time-based value for
    your recovery point objective (RPO). Even though you change the value
    of the [`archive_timeout` setting in the PostgreSQL configuration](https://www.postgresql.org/docs/current/runtime-config-wal.html#GUC-ARCHIVE-TIMEOUT),
    our experience suggests that the default value set by the operator is
    suitable for most use cases.

When the bandwidth between the PostgreSQL instance and the object
store allows archiving more than one WAL file in parallel, you
can use the parallel WAL archiving feature of the instance manager,
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

In this example, the instance manager optimizes the WAL
archiving process by archiving in parallel at most eight ready
WALs, including the one requested by PostgreSQL.

When PostgreSQL requests the archiving of a WAL that was
already archived by the instance manager as an optimization,
that archival request is dismissed with a positive status.
