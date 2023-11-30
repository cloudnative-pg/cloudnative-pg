# Tablespaces

## Introduction

A tablespace is an extraordinarily robust and widely used capability for
improving vertical scalability of a database by decoupling physical and logical
modeling of data. Strictly speaking, it is a physical database modeling
technique that allows the distribution of I/O over multiple volumes on
different storage, improving the performance on a single machine by
performing parallel on-disk read/write operations.

In the database industry, tablespaces are strategic in handling large scale
databases when adopted together with table partitioning, a logical database
modeling technique. Tablespaces are also used to separate tables from indexes
or to perform temporary operations.

When it comes to PostgreSQL, [tablespaces](https://www.postgresql.org/docs/current/manage-ag-tablespaces.html)
have been supported since 2005 (version 8.0), while declarative partitioning
since 2017 (version 10). As a result, they are part of all the supported
releases of PostgreSQL.

## Tablespaces in CloudNativePG

CloudNativePG handles tablespaces at two levels:

- Kubernetes: managing persistent volume claims, identically to how PGDATA and
  WAL volumes are handled
- PostgreSQL: managing the `TABLESPACE` global objects within the PostgreSQL
  instance

Let's use the following example to explain the feature:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: yardbirds
spec:
  instances: 3

  storage:
    size: 10Gi
  walStorage:
    size: 10Gi
  tablespaces:
    current:
      size: 100Gi
      storageClass: fastest
    this_year:
      size: 500Gi
      storageClass: balanced
```

The `yardbirds` cluster above requests 5 persistent volume claims using 3
different storage classes:

- default storage class: used by the `PGDATA` and WALs
- `fastest`: used by the `current` tablespace to store the most active and
  demanding set of data in the database
- `balanced`: used by the `old` tablespace to store older partitions of data
  that are rarely accessed by users and where performance expectations are
  not the highest

CloudNativePG will create the above persistent volume claims for each instance
in the high availability Postgres cluster, and mount them in each pod when they
have been provisioned.

Then, it will ensure that the `current` and `this_year` tablespace are created
on the primary PostgreSQL instance using the `CREATE TABLESPACE` command. By
default, unless differently specified, tablespaces are owned by the `app`
application user (as defined in `.spec.bootstrap.initdb.owner`) — see
["Bootstrap a new cluster](bootstrap.md#bootstrap-an-empty-cluster-initdb) for
details.
This default behavior should work in most microservice database use cases.

You can change the owner of a tablespace through the `owner` option, for example
the `postgres` user, like in the following excerpt:

```yaml
  # ...
  tablespaces:
    clapton:
      size: 10Gi
      owner: postgres
```

!!! Important
    Make sure that, if you change the ownership of a tablespace, you are using
    an existing role. Otherwise, the status of the cluster will report the
    issue and stop reconciling tablespaces until fixed.

## Adding a new tablespace

TODO

## Backup and Recovery

CloudNativePG automatically handles backup of tablespaces (and the relative
tablespace map) both on object stores and volume snapshots.

When it comes to the recovery side, it is your responsibility to ensure that
the `Cluster` definition of the recovered database contains the exact list of
tablespaces.

## Replica clusters

Replica clusters must have the same tablespace definition as their origin.
The reason is that tablespace management commands like `CREATE TABLESPACE`
are WAL logged and will be replayed by any physical replication client (streaming and/or via WAL shipping).

It is your responsibility to ensure that replica cluster have the same list of
tablespaces, with the same name (storage class and size might vary).

<!--
## Temporary tablespaces

PostgreSQL allows you to define one or more temporary tablespaces to create
temporary objects (temporary tables and indexes on temporary tables) when a
`CREATE` command does not explicitly specify a tablespace, as well as temporary
files for purposes such as sorting large data sets. When no temporary
tablespace is specified, PostgreSQL uses the default tablespace of a database -
currently the main `PGDATA` volume.

When you specify more than one temporary tablespace, PostgreSQL randomly picks
one the first time a temporary object needs to be created in a transaction,
then sequentially iterates through the list.

Temporary tablespaces work like regular tablespaces, including the backup part.

CloudNativePG provides the `.spec.tablespaces.NAME.temporary` option to
determine whether a tablespace can be used for temporary usage, entirely
abstracting the management of the `temp_tablespaces` PostgreSQL option from
you.

```yaml
```

They can be created at the initialization time or added later, requiring a
rolling update. The `temporary: true/false` simply adds/removes the
tablespace name to/from the list of tablespaces in the `temp_tablespaces`
option (which doesn't require a restart of PostgreSQL to be changed).

Although temporary tablespaces can also work as regular tablespaces (meaning
that users can also host regular data on them while also using them for
temporary operations), we recommend not to mix the two workloads.

-->

## Limitations

TODO:

- Delete
