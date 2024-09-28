# Bootstrap

This section describes the options you have to create a new
PostgreSQL cluster and the design rationale behind them.
There are primarily two ways to bootstrap a new cluster:

- from scratch (`initdb`)
- from an existing PostgreSQL cluster, either directly (`pg_basebackup`)
  or indirectly through a physical base backup (`recovery`)

The `initdb` bootstrap also offers the possibility to import one or more
databases from an existing Postgres cluster, even outside Kubernetes, and
having a different major version of Postgres.
For more detailed information about this feature, please refer to the
["Importing Postgres databases"](database_import.md) section.

!!! Important
    Bootstrapping from an existing cluster opens up the possibility
    to create a **replica cluster**, that is an independent PostgreSQL
    cluster which is in continuous recovery, synchronized with the source
    and that accepts read-only connections.

!!! Warning
    CloudNativePG requires both the `postgres` user and database to
    always exists. Using the local Unix Domain Socket, it needs to connect
    as `postgres` user to the `postgres` database via `peer` authentication in
    order to perform administrative tasks on the cluster.  
    **DO NOT DELETE** the `postgres` user or the `postgres` database!!!

!!! Info
    CloudNativePG is gradually introducing support for
    [Kubernetes' native `VolumeSnapshot` API](https://github.com/cloudnative-pg/cloudnative-pg/issues/2081)
    for both incremental and differential copy in backup and recovery
    operations - if supported by the underlying storage classes.
    Please see ["Recovery from Volume Snapshot objects"](recovery.md#recovery-from-volumesnapshot-objects)
    for details.

## The `bootstrap` section

The *bootstrap* method can be defined in the `bootstrap` section of the cluster
specification. CloudNativePG currently supports the following bootstrap methods:

- `initdb`: initialize a new PostgreSQL cluster (default)
- `recovery`: create a PostgreSQL cluster by restoring from a base backup of an
  existing cluster and, if needed, replaying all the available WAL files or up to
  a given *point in time*
- `pg_basebackup`: create a PostgreSQL cluster by cloning an existing one of
  the same major version using `pg_basebackup` via streaming replication protocol -
  useful if you want to migrate databases to CloudNativePG, even
  from outside Kubernetes.

Differently from the `initdb` method, both `recovery` and `pg_basebackup`
create a new cluster based on another one (either offline or online) and can be
used to spin up replica clusters. They both rely on the definition of external
clusters.

Given that there are several possible backup methods and combinations of backup
storage that the CloudNativePG operator provides, please refer to the
["Recovery" section](recovery.md) for guidance on each method.

!!! Seealso "API reference"
    Please refer to the ["API reference for the `bootstrap` section](cloudnative-pg.v1.md#postgresql-cnpg-io-v1-BootstrapConfiguration)
    for more information.

## The `externalClusters` section

The `externalClusters` section provides a mechanism for specifying one or more
PostgreSQL clusters associated with the current configuration. Its primary use
cases include:

1. **Importing Databases:** Specify an external source to be utilized during
  the [importation of databases](database_import.md) via logical backup and
  restore, as part of the `initdb` bootstrap method.
2. **Cross-Region Replication:** Define a cross-region PostgreSQL cluster
  employing physical replication, capable of extending across distinct Kubernetes
  clusters or traditional VM/bare-metal environments.
3. **Recovery from Physical Base Backup:** Recover, fully or at a
  given Point-In-Time, a PostgreSQL cluster by referencing a physical base
  backup.

!!! Info
    Ongoing development will extend the functionality of `externalClusters` to
    accommodate additional use cases, such as logical replication and foreign
    servers in future releases.

As far as bootstrapping is concerned, `externalClusters` can be used
to define the source PostgreSQL cluster for either the `pg_basebackup`
method or the `recovery` one. An external cluster needs to have:

- a name that identifies the origin cluster, to be used as a reference via the
  `source` option
- at least one of the following:

    - information about streaming connection
    - information about the **recovery object store**, which is a Barman Cloud
      compatible object store that contains:
        - the WAL archive (required for Point In Time Recovery)
        - the catalog of physical base backups for the Postgres cluster

!!! Note
    A recovery object store is normally an AWS S3, or an Azure Blob Storage,
    or a Google Cloud Storage source that is managed by Barman Cloud.

When only the streaming connection is defined, the source can be used for the
`pg_basebackup` method. When only the recovery object store is defined, the
source can be used for the `recovery` method. When both are defined, any of the
two bootstrap methods can be chosen.

Furthermore, in case of `pg_basebackup` or full `recovery` point in time, the
cluster is eligible for replica cluster mode. This means that the cluster is
continuously fed from the source, either via streaming, via WAL shipping
through the PostgreSQL's `restore_command`, or any of the two.

!!! Seealso "API reference"
    Please refer to the ["API reference for the `externalClusters` section](cloudnative-pg.v1.md#postgresql-cnpg-io-v1-ExternalCluster)
    for more information.

### Password files

Whenever a password is supplied within an `externalClusters` entry,
CloudNativePG autonomously manages a [PostgreSQL password file](https://www.postgresql.org/docs/current/libpq-pgpass.html)
for it, residing at `/controller/external/NAME/pgpass` in each instance.

This approach empowers CloudNativePG to securely establish connections with an
external server without exposing any passwords in the connection string.
Instead, the connection safely references the aforementioned file through the
`passfile` connection parameter.

## Bootstrap an empty cluster (`initdb`)

The `initdb` bootstrap method is used to create a new PostgreSQL cluster from
scratch. It is the default one unless specified differently.

The following example contains the full structure of the `initdb`
configuration:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-example-initdb
spec:
  instances: 3

  bootstrap:
    initdb:
      database: app
      owner: app
      secret:
        name: app-secret

  storage:
    size: 1Gi
```

The above example of bootstrap will:

1. create a new `PGDATA` folder using PostgreSQL's native `initdb` command
2. create an *unprivileged* user named `app`
3. set the password of the latter (`app`) using the one in the `app-secret`
   secret (make sure that `username` matches the same name of the `owner`)
4. create a database called `app` owned by the `app` user.

Thanks to the *convention over configuration paradigm*, you can let the
operator choose a default database name (`app`) and a default application
user name (same as the database name), as well as randomly generate a
secure password for both the superuser and the application user in
PostgreSQL.

Alternatively, you can generate your password, store it as a secret,
and use it in the PostgreSQL cluster - as described in the above example.

The supplied secret must comply with the specifications of the
[`kubernetes.io/basic-auth` type](https://kubernetes.io/docs/concepts/configuration/secret/#basic-authentication-secret).
As a result, the `username` in the secret must match the one of the `owner`
(for the application secret) and `postgres` for the superuser one.

The following is an example of a `basic-auth` secret:

```yaml
apiVersion: v1
data:
  username: YXBw
  password: cGFzc3dvcmQ=
kind: Secret
metadata:
  name: app-secret
type: kubernetes.io/basic-auth
```

The application database is the one that should be used to store application
data. Applications should connect to the cluster with the user that owns
the application database.

!!! Important
    If you need to create additional users, please refer to
    ["Declarative database role management"](declarative_role_management.md).

In case you don't supply any database name, the operator will proceed
by convention and create the `app` database, and adds it to the cluster
definition using a *defaulting webhook*.
The user that owns the database defaults to the database name instead.

The application user is not used internally by the operator, which instead
relies on the superuser to reconcile the cluster with the desired status.

### Passing options to `initdb`

The actual PostgreSQL data directory is created via an invocation of the
`initdb` PostgreSQL command. If you need to add custom options to that command
(i.e., to change the `locale` used for the template databases or to add data
checksums), you can use the following parameters:

dataChecksums
:   When `dataChecksums` is set to `true`, CNPG invokes the `-k` option in
    `initdb` to enable checksums on data pages and help detect corruption by the
    I/O system - that would otherwise be silent (default: `false`).

encoding
:   When `encoding` set to a value, CNPG passes it to the `--encoding` option in `initdb`,
    which selects the encoding of the template database (default: `UTF8`).

localeCollate
:   When `localeCollate` is set to a value, CNPG passes it to the `--lc-collate`
    option in `initdb`. This option controls the collation order (`LC_COLLATE`
    subcategory), as defined in ["Locale Support"](https://www.postgresql.org/docs/current/locale.html)
    from the PostgreSQL documentation (default: `C`).

localeCType
:   When `localeCType` is set to a value, CNPG passes it to the `--lc-ctype` option in
    `initdb`. This option controls the collation order (`LC_CTYPE` subcategory), as
    defined in ["Locale Support"](https://www.postgresql.org/docs/current/locale.html)
    from the PostgreSQL documentation (default: `C`).

walSegmentSize
:   When `walSegmentSize` is set to a value, CNPG passes it to the `--wal-segsize`
    option in `initdb` (default: not set - defined by PostgreSQL as 16 megabytes).

!!! Note
    The only two locale options that CloudNativePG implements during
    the `initdb` bootstrap refer to the `LC_COLLATE` and `LC_TYPE` subcategories.
    The remaining locale subcategories can be configured directly in the PostgreSQL
    configuration, using the `lc_messages`, `lc_monetary`, `lc_numeric`, and
    `lc_time` parameters.

The following example enables data checksums and sets the default encoding to
`LATIN1`:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-example-initdb
spec:
  instances: 3

  bootstrap:
    initdb:
      database: app
      owner: app
      dataChecksums: true
      encoding: 'LATIN1'
  storage:
    size: 1Gi
```

!!! Warning
    CloudNativePG supports another way to customize the behavior of the
    `initdb` invocation, using the `options` subsection. However, given that there
    are options that can break the behavior of the operator (such as `--auth` or
    `-d`), this technique is deprecated and will be removed from future versions of
    the API.

### Executing Queries After Initialization

You can specify a custom list of queries that will be executed once,
immediately after the cluster is created and configured. These queries will be
executed as the *superuser* (`postgres`) against three different databases, in
this specific order:

1. The `postgres` database (`postInit` section)
2. The `template1` database (`postInitTemplate` section)
3. The application database (`postInitApplication` section)

For each of these sections, CloudNativePG provides two ways to specify custom
queries, executed in the following order:

- As a list of SQL queries in the cluster's definition (`postInitSQL`,
  `postInitTemplateSQL`, and `postInitApplicationSQL` stanzas)
- As a list of Secrets and/or ConfigMaps, each containing a SQL script to be
  executed (`postInitSQLRefs`, `postInitTemplateSQLRefs`, and
  `postInitApplicationSQLRefs` stanzas). Secrets are processed before ConfigMaps.

Objects in each list will be processed sequentially.

!!! Warning
    Use the `postInit`, `postInitTemplate`, and `postInitApplication` options
    with extreme care, as queries are run as a superuser and can disrupt the entire
    cluster. An error in any of those queries will interrupt the bootstrap phase,
    leaving the cluster incomplete and requiring manual intervention.

!!! Important
    Ensure the existence of entries inside the ConfigMaps or Secrets specified
    in `postInitSQLRefs`, `postInitTemplateSQLRefs`, and
    `postInitApplicationSQLRefs`, otherwise the bootstrap will fail. Errors in any
    of those SQL files will prevent the bootstrap phase from completing
    successfully.

The following example runs a single SQL query as part of the `postInitSQL`
stanza:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-example-initdb
spec:
  instances: 3

  bootstrap:
    initdb:
      database: app
      owner: app
      dataChecksums: true
      localeCollate: 'en_US'
      localeCType: 'en_US'
      postInitSQL:
        - CREATE DATABASE angus
  storage:
    size: 1Gi
```

The example below relies on `postInitApplicationSQLRefs` to specify a secret
and a ConfigMap containing the queries to run after the initialization on the
application database:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-example-initdb
spec:
  instances: 3

  bootstrap:
    initdb:
      database: app
      owner: app
      postInitApplicationSQLRefs:
        secretRefs:
        - name: my-secret
          key: secret.sql
        configMapRefs:
        - name: my-configmap
          key: configmap.sql
  storage:
    size: 1Gi
```

!!! Note
    Within SQL scripts, each SQL statement is executed in a single exec on the
    server according to the [PostgreSQL semantics](https://www.postgresql.org/docs/current/protocol-flow.html#PROTOCOL-FLOW-MULTI-STATEMENT).
    Comments can be included, but internal commands like `psql` cannot.

## Bootstrap from another cluster

CloudNativePG enables the bootstrap of a cluster starting from
another one of the same major version.
This operation can happen by connecting directly to the source cluster via
streaming replication (`pg_basebackup`), or indirectly via an existing
physical *base backup* (`recovery`).

The source cluster must be defined in the `externalClusters` section, identified
by `name` (our recommendation is to use the same `name` of the origin cluster).

!!! Important
    By default the `recovery` method strictly uses the `name` of the
    cluster in the `externalClusters` section to locate the main folder
    of the backup data within the object store, which is normally reserved
    for the name of the server. You can specify a different one with the
    `barmanObjectStore.serverName` property (by default assigned to the
    value of `name` in the external cluster definition).

### Bootstrap from a backup (`recovery`)

Given the several possibilities, methods, and combinations that the
CloudNativePG operator provides in terms of backup and recovery, please refer
to the ["Recovery" section](recovery.md).

### Bootstrap from a live cluster (`pg_basebackup`)

The `pg_basebackup` bootstrap mode lets you create a new cluster (*target*) as
an exact physical copy of an existing and **binary compatible** PostgreSQL
instance (*source*), through a valid *streaming replication* connection.
The source instance can be either a primary or a standby PostgreSQL server.

The primary use case for this method is represented by **migrations** to CloudNativePG,
either from outside Kubernetes or within Kubernetes (e.g., from another operator).

!!! Warning
    The current implementation creates a *snapshot* of the origin PostgreSQL
    instance when the cloning process terminates and immediately starts
    the created cluster. See ["Current limitations"](#current-limitations) below for details.

Similar to the case of the `recovery` bootstrap method, once the clone operation
completes, the operator will take ownership of the target cluster, starting from
the first instance. This includes overriding some configuration parameters, as
required by CloudNativePG, resetting the superuser password, creating
the `streaming_replica` user, managing the replicas, and so on. The resulting
cluster will be completely independent of the source instance.

!!! Important
    Configuring the network between the target instance and the source instance
    goes beyond the scope of CloudNativePG documentation, as it depends
    on the actual context and environment.

The streaming replication client on the target instance, which will be
transparently managed by `pg_basebackup`, can authenticate itself on the source
instance in any of the following ways:

1. via [username/password](#usernamepassword-authentication)
2. via [TLS client certificate](#tls-certificate-authentication)

The latter is the recommended one if you connect to a source managed
by CloudNativePG or configured for TLS authentication.
The first option is, however, the most common form of authentication to a
PostgreSQL server in general, and might be the easiest way if the source
instance is on a traditional environment outside Kubernetes.
Both cases are explained below.

#### Requirements

The following requirements apply to the `pg_basebackup` bootstrap method:

- target and source must have the same hardware architecture
- target and source must have the same major PostgreSQL version
- target and source must have the same tablespaces
- source must be configured with enough `max_wal_senders` to grant
  access from the target for this one-off operation by providing at least
  one *walsender* for the backup plus one for WAL streaming
- the network between source and target must be configured to enable the target
  instance to connect to the PostgreSQL port on the source instance
- source must have a role with `REPLICATION LOGIN` privileges and must accept
  connections from the target instance for this role in `pg_hba.conf`, preferably
  via TLS (see ["About the replication user"](#about-the-replication-user) below)
- target must be able to successfully connect to the source PostgreSQL instance
  using a role with `REPLICATION LOGIN` privileges

!!! Seealso
    For further information, please refer to the
    ["Planning" section for Warm Standby](https://www.postgresql.org/docs/current/warm-standby.html#STANDBY-PLANNING),
    the
    [`pg_basebackup` page](https://www.postgresql.org/docs/current/app-pgbasebackup.html)
    and the
    ["High Availability, Load Balancing, and Replication" chapter](https://www.postgresql.org/docs/current/high-availability.html)
    in the PostgreSQL documentation.

#### About the replication user

As explained in the requirements section, you need to have a user
with either the `SUPERUSER` or, preferably, just the `REPLICATION`
privilege in the source instance.

If the source database is created with CloudNativePG, you
can reuse the `streaming_replica` user and take advantage of client
TLS certificates authentication (which, by default, is the only allowed
connection method for `streaming_replica`).

For all other cases, including outside Kubernetes, please verify that
you already have a user with the `REPLICATION` privilege, or create
a new one by following the instructions below.

As `postgres` user on the source system, please run:

```console
createuser -P --replication streaming_replica
```

Enter the password at the prompt and save it for later, as you
will need to add it to a secret in the target instance.

!!! Note
    Although the name is not important, we will use `streaming_replica`
    for the sake of simplicity. Feel free to change it as you like,
    provided you adapt the instructions in the following sections.

#### Username/Password authentication

The first authentication method supported by CloudNativePG
with the `pg_basebackup` bootstrap is based on username and password matching.

Make sure you have the following information before you start the procedure:

- location of the source instance, identified by a hostname or an IP address
  and a TCP port
- replication username (`streaming_replica` for simplicity)
- password

You might need to add a line similar to the following to the `pg_hba.conf`
file on the source PostgreSQL instance:

```
# A more restrictive rule for TLS and IP of origin is recommended
host replication streaming_replica all md5
```

The following manifest creates a new PostgreSQL 17.0 cluster,
called `target-db`, using the `pg_basebackup` bootstrap method
to clone an external PostgreSQL cluster defined as `source-db`
(in the `externalClusters` array). As you can see, the `source-db`
definition points to the `source-db.foo.com` host and connects as
the `streaming_replica` user, whose password is stored in the
`password` key of the `source-db-replica-user` secret.

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: target-db
spec:
  instances: 3
  imageName: ghcr.io/cloudnative-pg/postgresql:17.0

  bootstrap:
    pg_basebackup:
      source: source-db

  storage:
    size: 1Gi

  externalClusters:
  - name: source-db
    connectionParameters:
      host: source-db.foo.com
      user: streaming_replica
    password:
      name: source-db-replica-user
      key: password
```

All the requirements must be met for the clone operation to work, including
the same PostgreSQL version (in our case 17.0).

#### TLS certificate authentication

The second authentication method supported by CloudNativePG
with the `pg_basebackup` bootstrap is based on TLS client certificates.
This is the recommended approach from a security standpoint.

The following example clones an existing PostgreSQL cluster (`cluster-example`)
in the same Kubernetes cluster.

!!! Note
    This example can be easily adapted to cover an instance that resides
    outside the Kubernetes cluster.

The manifest defines a new PostgreSQL 17.0 cluster called `cluster-clone-tls`,
which is bootstrapped using the `pg_basebackup` method from the `cluster-example`
external cluster. The host is identified by the read/write service
in the same cluster, while the `streaming_replica` user is authenticated
thanks to the provided keys, certificate, and certification authority
information (respectively in the `cluster-example-replication` and
`cluster-example-ca` secrets).

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-clone-tls
spec:
  instances: 3
  imageName: ghcr.io/cloudnative-pg/postgresql:17.0

  bootstrap:
    pg_basebackup:
      source: cluster-example

  storage:
    size: 1Gi

  externalClusters:
  - name: cluster-example
    connectionParameters:
      host: cluster-example-rw.default.svc
      user: streaming_replica
      sslmode: verify-full
    sslKey:
      name: cluster-example-replication
      key: tls.key
    sslCert:
      name: cluster-example-replication
      key: tls.crt
    sslRootCert:
      name: cluster-example-ca
      key: ca.crt
```

#### Configure the application database

We also support to configure the application database for cluster which bootstrap
from a live cluster, just like the case of `initdb` and  `recovery` bootstrap method.
If the new cluster is created as a replica cluster (with replica mode enabled), application
database configuration will be skipped.

!!! Important
    While the `Cluster` is in recovery mode, no changes to the database,
    including the catalog, are permitted. This restriction includes any role
    overrides, which are deferred until the `Cluster` transitions to primary.
    During the recovery phase, roles remain as defined in the source cluster.

The example below configures the `app` database with the owner `app` and
the password stored in the provided secret `app-secret`, following the
bootstrap from a live cluster.

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
[...]
spec:
  bootstrap:
    pg_basebackup:
      database: app
      owner: app
      secret:
        name: app-secret
      source: cluster-example
```

With the above configuration, the following will happen only **after recovery is
completed**:

1. If the `app` database does not exist, it will be created.
2. If the `app` user does not exist, it will be created.
3. If the `app` user is not the owner of the `app` database, ownership will be
   granted to the `app` user.
4. If the `username` value matches the `owner` value in the secret, the
   password for the application user (the `app` user in this case) will be
   updated to the `password` value in the secret.

#### Current limitations

##### Snapshot copy

The `pg_basebackup` method takes a snapshot of the source instance in the form of
a PostgreSQL base backup. All transactions written from the start of
the backup to the correct termination of the backup will be streamed to the target
instance using a second connection (see the `--wal-method=stream` option for
`pg_basebackup`).

Once the backup is completed, the new instance will be started on a new timeline
and diverge from the source.
For this reason, it is advised to stop all write operations to the source database
before migrating to the target database in Kubernetes.

!!! Important
    Before you attempt a migration, you must test both the procedure
    and the applications. In particular, it is fundamental that you run the migration
    procedure as many times as needed to systematically measure the downtime of your
    applications in production.
