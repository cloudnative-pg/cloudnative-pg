# Tablespaces

A tablespace stands as a robust and widely embraced feature within database
management systems, offering a powerful means to enhance the vertical
scalability of a database by decoupling the physical and logical modeling of
data. Essentially, it serves as a technique for physical database modeling,
enabling the efficient distribution of I/O operations across multiple volumes
on distinct storage, thereby optimizing performance through parallel on-disk
read/write operations.

In the context of the database industry, tablespaces play a strategic role,
particularly when paired with table partitioning—a logical database modeling
technique. They prove instrumental in managing large-scale databases and are
also employed for tasks such as separating tables from indexes or executing
temporary operations.

Tablespaces in PostgreSQL have been playing a pivotal role since 2005 (version
8.0), while declarative partitioning was introduced in 2017 (version 10).
Consequently, tablespaces are seamlessly integrated into all supported releases
of PostgreSQL. Quoting from the
[PostgreSQL documentation on Tablespaces](https://www.postgresql.org/docs/current/manage-ag-tablespaces.html):

> By using tablespaces, an administrator can control the disk layout of a
> PostgreSQL installation. This is useful in at least two ways.
>
> - First, if the partition or volume on which the cluster was initialized runs
>   out of space and cannot be extended, a tablespace can be created on a
>   different partition and used until the system can be reconfigured.
> - Second, tablespaces allow an administrator to use knowledge of the usage
>   pattern of database objects to optimize performance.

## Declarative tablespaces

CloudNativePG provides support for PostgreSQL tablespaces through **declarative
tablespaces**, operating at two distinct levels:

- Kubernetes: managing persistent volume claims, identically to how PGDATA and
  WAL volumes are handled
- PostgreSQL: managing the `TABLESPACE` global objects within the PostgreSQL
  instance

Being a part of the Kubernetes ecosystem, CloudNativePG's declarative
tablespaces are implemented leveraging Persistent Volume Claims (and Persistent
Volumes). Each tablespace defined in the cluster is housed in its own
persistent volume. CloudNativePG takes care of generating the PVCs, mounting
the required volumes in the instance Pods in normalized locations, and ensuring
replicas are ready to support tablespaces before activating them in the
primary.

Tablespaces can be setup when the cluster is created, or added at a later time
— provided the storage is available when requested. Currently, they cannot be
removed, but this limitation will be addressed in a future minor/patch version
of CloudNativePG.

## Using declarative tablespaces

Using declarative tablespaces is easy. You can find a full example in
[`cluster-example-with-tablespaces.yaml`](samples/cluster-example-with-tablespaces.yaml).

Simply use the new `tablespaces` stanza on a new or existing `Cluster` resource:

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

Note that each tablespace has its own storage section where the size and the
storage class of the generated PVC can be configured. The administrator can thus
plan to use different storage classes for different kinds of workloads, as
explained in the next section.

CloudNativePG will create the above persistent volume claims for each instance
in the high availability Postgres cluster, and mount them in each pod when they
have been provisioned. Then, it will ensure that the `tbs1`, `tbs2`, and `tbs3`
tablespaces are created on the primary PostgreSQL instance using the `CREATE
TABLESPACE` command. This process is quick, and you will see this reflected in
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

As for PGDATA and WAL volumes, you can use different storage classes for your
tablespaces too. This is a very convenient way of optimizing your resources,
balancing performance and costs of your storage based on data access usage and
expectations.

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
    - name: current
      size: 100Gi
      storageClass: fastest
    - name: this_year
      size: 500Gi
      storageClass: balanced
```

The `yardbirds` cluster example above requests 4 persistent volume claims using
3 different storage classes:

- default storage class: used by the `PGDATA` and WALs
- `fastest`: used by the `current` tablespace to store the most active and
  demanding set of data in the database
- `balanced`: used by the `this_year` tablespace to store older partitions of
  data that are rarely accessed by users and where performance expectations
  are not the highest

You can then take advantage of horizontal table partitioning and create
the current month's table (e.g. facts for December 2023) in the `current`
tablespace:

``` sql
CREATE TABLE facts_202312 PARTITION OF facts
    FOR VALUES FROM ('2023-12-01') TO ('2024-01-01')
    TABLESPACE current;
```

!!! Important
    The above example assumes you are familiar with
    [PostgreSQL declarative partitioning](https://www.postgresql.org/docs/current/ddl-partitioning.html).

## Tablespace ownership

By default, unless differently specified, tablespaces are owned by the `app`
application user (as defined in `.spec.bootstrap.initdb.owner`) — see
["Bootstrap a new cluster](bootstrap.md#bootstrap-an-empty-cluster-initdb) for
details.
This default behavior should work in most microservice database use cases.

You can set the owner of a tablespace through the `owner` stanza, for example
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

!!! Important
    Make sure that, if you change the ownership of a tablespace, you are using
    an existing role. Otherwise, the status of the cluster will report the
    issue and stop reconciling tablespaces until fixed. It is your responsibility
    to monitor the status and the log, and promptly intervene by fixing the issue.

If you define a tablespace with an owner that doesn't exist, CloudNativePG will
be unable to create the tablespace, and will reflect this in the cluster status:

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

## Backup and Recovery

CloudNativePG automatically handles backup of tablespaces (and the relative
tablespace map) both on object stores and volume snapshots.

!!! Warning
    By default, backups are taken from replica nodes. A backup taken immediately
    after the creation of tablespaces in a cluster could result in an
    incomplete view of the tablespaces from the replica, and thus an incomplete
    backup. The lag will be resolved in a maximum of 5 minutes, with the next
    reconciliation.

Once a cluster with tablespaces has a base backup, it is possible to restore a
new cluster from it.  When it comes to the recovery side, it is your
responsibility to ensure that the `Cluster` definition of the recovered
database contains the exact list of tablespaces.

## Replica clusters

Replica clusters must have the same tablespace definition as their origin.
The reason is that tablespace management commands like `CREATE TABLESPACE`
are WAL logged and will be replayed by any physical replication client (streaming and/or via WAL shipping).

It is your responsibility to ensure that replica cluster have the same list of
tablespaces, with the same name (storage class and size might vary).

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
`CREATE` command does not explicitly specify a tablespace, as well as temporary
files for purposes such as sorting large data sets. When no temporary
tablespace is specified, PostgreSQL uses the default tablespace of a database -
currently the main `PGDATA` volume.

When you specify more than one temporary tablespace, PostgreSQL randomly picks
one the first time a temporary object needs to be created in a transaction,
then sequentially iterates through the list.

Temporary tablespaces work like regular tablespaces, also regarding backups.

CloudNativePG provides the `.spec.tablespaces[*].name.temporary` option to
determine whether a tablespace should be added to the `temp_tablespaces`
PostgreSQL parameter, and thus become eligible to store temporary data that
does not have an explicit tablespace assignment.

```yaml
spec:
  [...]
  tablespaces:
    - name: atablespace
      storage:
        size: 1Gi
      temporary: true
```

They can be created at the initialization time or added later, requiring a
rolling update. The `temporary: true/false` simply adds/removes the
tablespace name to/from the list of tablespaces in the `temp_tablespaces`
option (which doesn't require a restart of PostgreSQL to be changed).

Although temporary tablespaces can also work as regular tablespaces (meaning
that users can also host regular data on them while also using them for
temporary operations), we recommend not to mix the two workloads.

## kubectl plugin support

The [kubectl status](kubectl-plugin.md#status) plugin includes a section
dedicated to tablespaces which offers a convenient overview, including
tablespace status, owner, temporary flag, or any errors:

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

Currently, tablespaces cannot be removed from an existing CloudNativePG
cluster.
