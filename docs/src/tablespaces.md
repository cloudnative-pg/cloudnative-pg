---
id: tablespaces
sidebar_position: 250
title: Tablespaces
---

# Tablespaces
<!-- SPDX-License-Identifier: CC-BY-4.0 -->

A tablespace is a robust and widely embraced feature in database
management systems. It offers a powerful means to enhance the vertical
scalability of a database by decoupling the physical and logical modeling of
data. Essentially, it serves as a technique for physical database modeling,
enabling the efficient distribution of I/O operations across multiple volumes
on distinct storage. It thereby optimizes performance through parallel on-disk
read/write operations.

In the context of the database industry, tablespaces play a strategic role,
particularly when paired with table partitioning, a logical database modeling
technique. They prove instrumental in managing large-scale databases and are
also used for tasks such as separating tables from indexes or executing
temporary operations.

Tablespaces in PostgreSQL have been playing a pivotal role since 2005 (version
8.0), while declarative partitioning was introduced in 2017 (version 10).
Consequently, tablespaces are seamlessly integrated into all supported releases
of PostgreSQL. Quoting from the
[PostgreSQL documentation on tablespaces](https://www.postgresql.org/docs/current/manage-ag-tablespaces.html):

> By using tablespaces, an administrator can control the disk layout of a
> PostgreSQL installation. This is useful in at least two ways.
>
> - First, if the partition or volume on which the cluster was initialized runs
>   out of space and cannot be extended, a tablespace can be created on a
>   different partition and used until the system can be reconfigured.
> - Second, tablespaces allow an administrator to use knowledge of the usage
>   pattern of database objects to optimize performance.

## Declarative tablespaces

CloudNativePG provides support for PostgreSQL tablespaces through *declarative
tablespaces*, operating at two distinct levels:

- Kubernetes, managing persistent volume claims, identically to how PGDATA and
  WAL volumes are handled
- PostgreSQL, managing the `TABLESPACE` global objects in the PostgreSQL
  instance

Being a part of the Kubernetes ecosystem, CloudNativePG's declarative
tablespaces are implemented by leveraging persistent volume claims (and persistent
volumes). Each tablespace defined in the cluster is housed in its own
persistent volume. CloudNativePG takes care of generating the PVCs. It mounts
the required volumes in the instance pods in normalized locations and ensures
replicas are ready to support tablespaces before activating them in the
primary.

You can set up tablespaces when creating the cluster or add them later,
provided the storage is available when requested. Currently, you can't
remove them. However, this limitation will be addressed in a future minor or patch version
of CloudNativePG.

## Using declarative tablespaces

Using declarative tablespaces is straightforward. You can find a full example in
[`cluster-example-with-tablespaces.yaml`](samples/cluster-example-with-tablespaces.yaml).

To use them, use the new `tablespaces` stanza on a new or existing `Cluster` resource:

``` yaml
spec:
  instances: 3

  # ...

  tablespaces:
    - name: tbs1
      storage:
        size: 1Gi
    - name: tbs2
      storage:
        size: 2Gi
    - name: tbs3
      storage:
        size: 2Gi
```

Each tablespace has its own storage section where you can configure the size and the
storage class of the generated PVC. The administrator can thus
plan to use different storage classes for different kinds of workloads, as
explained in [Storage classes and tablespaces](#storage-classes-and-tablespaces).

CloudNativePG creates the persistent volume claims for each instance
in the high-availability Postgres cluster. It mounts them in each pod when they
have been provisioned. Then, it ensures that the `tbs1`, `tbs2`, and `tbs3`
tablespaces are created on the primary PostgreSQL instance using the `CREATE
TABLESPACE` command. This process is quick, and you see this reflected in
Postgres:

``` txt
app=# SELECT oid, spcname FROM pg_tablespace;
  oid  |      spcname       
-------+--------------------
  1663 | pg_default
  1664 | pg_global
 16387 | tbs1
 16388 | tbs2
 16389 | tbs3
(5 rows)
```

You can start using them right away:

``` txt
app=# CREATE TABLE fibonacci(num INTEGER) TABLESPACE tbs1;
CREATE TABLE
```

The cluster status has a section for tablespaces:

``` yaml
status:

  <- snipped ->
  tablespacesStatus:
  - name: atablespace
    state: reconciled
  - name: another_tablespace
    state: reconciled
  - name: tablespacea1
    state: reconciled
```

## Storage classes and tablespaces

You can use different storage classes for your tablespaces, just as you can for PGDATA and
WAL volumes. This is a convenient way of optimizing your resources,
balancing performance and costs of your storage based on data access usage and
expectations.

This example helps to explain the feature:

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
    - name: current
      storage:        
        size: 100Gi
        storageClass: fastest
    - name: this_year
      storage:
        size: 500Gi
        storageClass: balanced
```

The `yardbirds` cluster example requests 4 persistent volume claims using
3 different storage classes:

- Default storage class – Used by the `PGDATA` and WAL volumes.
- `fastest` – Used by the `current` tablespace to store the most active and
  demanding set of data in the database.
- `balanced` – Used by the `this_year` tablespace to store older partitions of
  data that are rarely accessed by users and where performance expectations
  aren't the highest.

You can then take advantage of horizontal table partitioning and create
the current month's table (for example, facts for December 2023) in the `current`
tablespace:

``` sql
CREATE TABLE facts_202312 PARTITION OF facts
    FOR VALUES FROM ('2023-12-01') TO ('2024-01-01')
    TABLESPACE current;
```

:::info[Important]
    This example assumes you're familiar with
    [PostgreSQL declarative partitioning](https://www.postgresql.org/docs/current/ddl-partitioning.html).
:::

## Tablespace ownership

By default, unless otherwise specified, tablespaces are owned by the `app`
application user, as defined in `.spec.bootstrap.initdb.owner`. See
[Bootstrap a new cluster](bootstrap.md#bootstrap-an-empty-cluster-initdb) for
details.
This default behavior works in most microservice database use cases.

You can set the owner of a tablespace in the `owner` stanza, for example
the `postgres` user, like in the following excerpt:

```yaml
  # ...
  tablespaces:
    - name: clapton
      owner:
        name: postgres
      storage:
        size: 1Gi
```

:::info[Important]
    If you change the ownership of a tablespace, make sure that you're using
    an existing role. Otherwise, the status of the cluster reports the
    issue and stops reconciling tablespaces until fixed. It's your responsibility
    to monitor the status and the log and to promptly intervene by fixing the issue.
:::

If you define a tablespace with an owner that doesn't exist, CloudNativePG can't
create the tablespace and reflects this in the cluster status:

``` yaml
spec:
  instances: 3

  # ...

  tablespaces:
    - name: tbs1
      storage:
        size: 1Gi
    - name: tbs2
      storage:
        size: 2Gi
    - name: tbs3
      owner:
        name: badhombre
      storage:
        size: 2Gi
        status:

  <- snipped ->
  tablespacesStatus:
  - name: tbs1
    status: reconciled
  - name: tbs2
    status: reconciled
  - error: 'while creating tablespace tbs3: ERROR: role "badhombre" does
      not exist (SQLSTATE 42704)'
    name: tbs3
    status: pending
```

## Backup and recovery

CloudNativePG handles backup of tablespaces (and the relative
tablespace map) both on object stores and volume snapshots.

:::warning
    By default, backups are taken from replica nodes. A backup taken immediately
    after creating tablespaces in a cluster can result in an
    incomplete view of the tablespaces from the replica and thus an incomplete
    backup. The lag will be resolved in a maximum of 5 minutes, with the next
    reconciliation.
:::

:::warning
    When you add or remove a tablespace in an existing cluster, recovery
    from WAL will fail until you take a new base backup.
:::

Once a cluster with tablespaces has a base backup, you can restore a
new cluster from it. When it comes to the recovery side, it's your
responsibility to ensure that the `Cluster` definition of the recovered
database contains the exact list of tablespaces.

## Replica clusters

Replica clusters must have the same tablespace definition as their origin.
The reason is that tablespace management commands like `CREATE TABLESPACE`
are WAL logged and are replayed by any physical replication client (streaming or by way of WAL shipping).

It's your responsibility to ensure that replica clusters have the same list of
tablespaces, with the same name. Storage class and size might vary.

For example:

``` yaml
spec:

  # ...
  bootstrap:
    recovery:
      # ... your selected recovery method

  tablespaces:
    - name: tbs1
      storage:
        size: 1Gi
    - name: tbs2
      storage:
        size: 2Gi
    - name: tbs3
      storage:
        size: 2Gi
```

## Temporary tablespaces

PostgreSQL allows you to define one or more temporary tablespaces to create
temporary objects (temporary tables and indexes on temporary tables) when a
`CREATE` command doesn't explicitly specify a tablespace, and to create temporary
files for purposes such as sorting large data sets. When no temporary
tablespace is specified, PostgreSQL uses the default tablespace of a database, which is
currently the main `PGDATA` volume.

When you specify more than one temporary tablespace, PostgreSQL randomly picks
one the first time a temporary object needs to be created in a transaction.
Then it sequentially iterates through the list.

Temporary tablespaces also work like regular tablespaces with regard to backups.

CloudNativePG provides the `.spec.tablespaces[*].temporary` option to
determine whether to add a tablespace to the `temp_tablespaces`
PostgreSQL parameter and thus become eligible to store temporary data that
doesn't have an explicit tablespace assignment.

```yaml
spec:
  [...]
  tablespaces:
    - name: atablespace
      storage:
        size: 1Gi
      temporary: true
```

They can be created at initialization time or added later, requiring a
rolling update. The `temporary: true/false` option adds or removes the
tablespace name to or from the list of tablespaces in the `temp_tablespaces`
option. This change doesn't require a restart of PostgreSQL.

Although temporary tablespaces can also work as regular tablespaces (meaning
that users can also host regular data on them while using them for
temporary operations), we recommend that you don't mix the two workloads.

See the [PostgreSQL documentation on `temp_tablespaces`](https://www.postgresql.org/docs/current/runtime-config-client.html#GUC-TEMP-TABLESPACES)
for details.

## kubectl plugin support

The [kubectl status](kubectl-plugin.md#status) plugin includes a section
dedicated to tablespaces that offers a convenient overview, including
tablespace status, owner, temporary flag, and any errors:

``` yaml
[...]

Tablespaces status
Tablespace          Owner  Status      Temporary  Error
----------          -----  ------      ---------  -----
atablespace         app    reconciled  true       
another_tablespace  app    reconciled  true       
tablespacea1        app    reconciled  false 

Instances status
[...]
```

## Limitations

Currently, you can't remove tablespaces from an existing CloudNativePG
cluster.
