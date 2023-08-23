# WAL archiving

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
