# Importing Postgres databases

You can import one or more existing PostgreSQL
databases to a new CloudNativePG cluster.

The import operation is based on the concept of online logical backups in PostgreSQL.
It relies on pg_dump, by way of a network connection to the origin host, and pg_restore.
Because of native multi-version concurrency control (MVCC) and snapshots,
PostgreSQL enables taking consistent backups over the network, in a concurrent
manner, without stopping any write activity.

Logical backups are also the most common, flexible, and reliable technique to
perform major upgrades of PostgreSQL versions.

As a result, the instructions that follow are suitable for both:

- Importing one or more databases from an existing PostgreSQL instance, even
  outside Kubernetes
- Importing the database from any PostgreSQL version to one that's either the
  same or newer, enabling major upgrades of PostgreSQL (for example, from version 11.x
  to version 15.x)

!!! Warning
    When performing major upgrades of PostgreSQL, you're responsible for making
    sure that applications are compatible with the new version and that the
    upgrade path of the objects contained in the database (including extensions) is
    feasible.

In both cases, the operation is performed on a consistent snapshot of the
origin database.

!!! Important
    Because the import is performed on a consistent snapshot of the origin database, 
    we suggest that you stop write operations on the source before
    the final import in the `Cluster` resource. Changes done to the source
    database after the start of the backup won't be in the destination cluster,
    which is why this feature is referred to as *offline import* or *offline major
    upgrade*.

## How it works

Conceptually, the import requires you to create a new cluster 
(the *destination cluster*) from scratch, using the [`initdb` bootstrap method](bootstrap.md).
Then you complete the `initdb.import` subsection to import objects from an
existing Postgres cluster (the *source cluster*). As per the PostgreSQL recommendation,
we suggest that the PostgreSQL major version of the destination cluster be
greater than or equal to the source cluster's.

CloudNativePG provides two main ways to import objects from the source cluster
to the destination cluster:

- **Microservice approach** - The destination cluster is designed to host a
  single application database owned by the specified application user, as
  recommended by the CloudNativePG project. This method is available by way 
  of the `microservice` type.

- **Monolith approach** - The destination cluster is designed to host multiple
  databases and different users, imported from the source cluster. This method
  is available by way of the `monolith` type.

!!! Warning
    It's your responsibility to ensure that the destination cluster can
    access the source cluster with a superuser or a user having enough
    privileges to take a logical backup using pg_dump. See the
    [PostgreSQL documentation on pg_dump](https://www.postgresql.org/docs/current/app-pgdump.html)
    for more information.

## The `microservice` type

With the microservice approach, you can specify a single database you want to
import from the source cluster to the destination cluster. The operation is
performed in four (optionally five) steps:

- `initdb` bootstrap of the new cluster
- Export of the selected database (in `initdb.import.databases`) using
  `pg_dump -Fc`
- Import of the database using `pg_restore --no-acl --no-owner` into the
  `initdb.database` (application database) owned by the `initdb.owner` user
- Cleanup of the database dump file
- Optional execution of the user-defined SQL queries in the application
  database by way of the `postImportApplicationSQL` parameter
- Execution of `ANALYZE VERBOSE` on the imported database

![Example of microservice import type](./images/microservice-import.png)

For example, the YAML that follows:

- Creates a three-instance PostgreSQL cluster (latest
available major version at the time the operator was released) called
`cluster-microservice`
- Imports the `angus` database from the
`cluster-pg96` cluster (with the unsupported PostgreSQL 9.6) by connecting to
the `postgres` database using the postgres user 
- Uses the password stored in the `cluster-pg96-superuser` secret to connect to the `postges` database

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-microservice
spec:
  instances: 3

  bootstrap:
    initdb:
      import:
        type: microservice
        databases:
          - angus
        source:
          externalCluster: cluster-pg96
        #postImportApplicationSQL:
        #- |
        #  INSERT YOUR SQL QUERIES HERE
  storage:
    size: 1Gi
  externalClusters:
    - name: cluster-pg96
      connectionParameters:
        # Use the correct IP or host name for the source database
        host: pg96.local
        user: postgres
        dbname: postgres
      password:
        name: cluster-pg96-superuser
        key: password
```

!!! Warning
    The example deliberately uses a source database running a version of
    PostgreSQL that isn't supported anymore by the community and, consequently, by
    CloudNativePG.
    Data export from the source instance is performed using the version of
    pg_dump in the destination cluster, which must be a supported one and
    equal to or greater than the source one.
    Based on our experience, this way of exporting data works on older
    and unsupported versions of Postgres too, giving you the chance to move your
    legacy data to a better system, inside Kubernetes.
    This is the main reason why we used 9.6 in the examples.
    Contact us if you experience any issues in this area.

Be aware of the following when using the `microservice` type:

- It requires an `externalCluster` that points to an existing PostgreSQL
  instance containing the data to import. (For more information, see
  [`externalClusters`](bootstrap.md#the-externalclusters-section).)
- Traffic must be allowed between the Kubernetes cluster and the
  `externalCluster` during the operation.
- Connection to the source database must be granted with the specified user
  that needs to run pg_dump and read roles information (superuser is OK).
- Currently, the `pg_dump -Fc` result is stored temporarily in the `dumps`
  folder in the `PGDATA` volume, so there should be enough available space to
  temporarily contain the dump result on the assigned node, as well as the
  restored data and indexes. Once the import operation is complete,
  the operator deletes the folder.
- You can specify only one database in the `initdb.import.databases` array.
- Roles aren't imported. As such, you can't specify them in `initdb.import.roles`.

## The `monolith` type

With the monolith approach, you can specify a set of roles and databases you
want to import from the source cluster to the destination cluster.
The operation is performed in the following steps:

- `initdb` bootstrap of the new cluster
- Export and import of the selected roles
- Export of the selected databases (in `initdb.import.databases`), one at a time,
  using `pg_dump -Fc`
- Creation of each of the selected databases and importing of data using `pg_restore`
- Running `ANALYZE` on each imported database
- Cleanup of the database dump files

![Example of monolith import type](./images/monolith-import.png)

For example, the following YAML creates a new three-instance PostgreSQL cluster (latest
available major version at the time the operator was released) called
`cluster-monolith`. It imports the accountant and the bank_user roles
as well as the `accounting`, `banking`, `resort` databases from the
`cluster-pg96` cluster (with the unsupported PostgreSQL 9.6) by connecting to
the `postgres` database using the postgres user. It uses the password stored in
the `cluster-pg96-superuser` secret.

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-monolith
spec:
  instances: 3
  bootstrap:
    initdb:
      import:
        type: monolith
        databases:
          - accounting
          - banking
          - resort
        roles:
          - accountant
          - bank_user
        source:
          externalCluster: cluster-pg96
  storage:
    size: 1Gi
  externalClusters:
    - name: cluster-pg96
      connectionParameters:
        # Use the correct IP or host name for the source database
        host: pg96.local
        user: postgres
        dbname: postgres
        sslmode: require
      password:
        name: cluster-pg96-superuser
        key: password
```

Be aware of the following when using the `monolith` type:

- It requires an `externalCluster` that points to an existing PostgreSQL
  instance containing the data to import. (For more information, see
  [`externalClusters`](bootstrap.md#the-externalclusters-section).)
- Traffic must be allowed between the Kubernetes cluster and the
  `externalCluster` during the operation.
- Connection to the source database must be granted with the specified user
  that needs to run pg_dump and retrieve roles information (superuser is
  OK).
- Currently, the `pg_dump -Fc` result is stored temporarily in the `dumps`
  folder in the `PGDATA` volume, so there should be enough available space to
  temporarily contain the dump result on the assigned node as well as the
  restored data and indexes. Once the import operation is complete, the
  operator deletes the folder.
- At least one database must be specified in the `initdb.import.databases` array.
- Any role that's required by the imported databases must be specified in
  `initdb.import.roles`, with these limitations:
    - The following roles, if present, aren't imported:
      postgres, streaming_replica, cnp_pooler_pgbouncer.
    - The `SUPERUSER` option is removed from any imported role.
- Wildcard `"*"` can be used as the only element in the `databases` or
  `roles` arrays to import every object of the kind. When matching databases,
  the wildcard ignores the `postgres` database, template databases,
  and those databases not allowing connections.
- After the clone procedure is done, `ANALYZE VERBOSE` is executed for every
  database.
- `postImportApplicationSQL` field isn't supported.
