# Declarative Tablespaces

<!-- TODO: content needs to be added ahead of merging to main -->
With *declarative tablespaces*, CloudNativePG brings support for
[PostgreSQL tablespaces](https://www.postgresql.org/docs/current/manage-ag-tablespaces.html).

Quoting from the PostgreSQL documentation on Tablespaces:

> By using tablespaces, an administrator can control the disk layout of a
> PostgreSQL installation. This is useful in at least two ways.
>
> - First, if the partition or volume on which the cluster was initialized runs
>   out of space and cannot be extended, a tablespace can be created on a
>   different partition and used until the system can be reconfigured.
> - Second, tablespaces allow an administrator to use knowledge of the usage
>   pattern of database objects to optimize performance.

Being a part of the Kubernetes ecosystem, CloudNativePG's declarative
tablespaces are implemented leveraging Persistent Volume Claims (and Persistent
Volumes).
Each tablespace defined in the cluster is housed in its own persistent volume.
CloudNativePG takes care of generating the PVCs, mounting the required volumes
in the instance Pods, and ensuring replicas are ready to support tablespaces
before activating them in the primary.

Using declarative tablespaces is easy. You can find a full example in
[`cluster-example-with-tablespaces.yaml`](samples/cluster-example-with-tablespaces.yaml).

Simply use the new `tablespaces` stanza:

``` yaml
spec:
  instances: 3

  <- snipped ->

  tablespaces:
  tablespaces:
    atablespace:
      storage:
        size: 1Gi
        storageClass: standard
    another_tablespace:
      storage:
        size: 2Gi
        storageClass: standard
    tablespacea1:
      storage:
        size: 2Gi
        storageClass: standard
```

Note that each tablespace has its own storage section where the size and the
storage class of the generated PVC can be configured. The administrator can thus
plan to use different storage classes for different kinds of workloads.

The creation of tablespaces is quick, and you will see this reflected in
Postgres:

``` txt
app=# select oid, spcname from pg_tablespace;
  oid  |      spcname       
-------+--------------------
  1663 | pg_default
  1664 | pg_global
 16387 | another_tablespace
 16388 | atablespace
 16389 | tablespacea1
(5 rows)
```

And you can start using them right away:

``` txt
app=# create table fibonacci(num int) tablespace another_tablespace;
CREATE TABLE
```

The cluster status has a section for tablespaces:

``` yaml
status:

  <- snipped ->

  tablespacesStatus:
    byStatus:
      reconciled:
      - another_tablespace
      - atablespace
      - tablespacea1
```

Tablespaces, coupled with PostgreSQL's
[declarative partitioning](https://www.postgresql.org/docs/14/ddl-partitioning.html),
can be
The PostgreSQL documentation contains an example of this usage.

``` sql
CREATE TABLE measurement_y2007m12 PARTITION OF measurement
    FOR VALUES FROM ('2007-12-01') TO ('2008-01-01')
    TABLESPACE fasttablespace;
```
