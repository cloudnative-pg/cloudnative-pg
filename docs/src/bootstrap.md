# Bootstrap

You have two main options for bootstrapping a new PostgreSQL cluster. You can create a cluster:

- From scratch (`initdb`)
- From an existing PostgreSQL cluster, either directly (`pg_basebackup`)
  or indirectly through a physical base backup (`recovery`)

The `initdb` bootstrap also lets you import one or more
databases from an existing Postgres cluster. The cluster can be outside Kubernetes and it can
have a different major version of Postgres.
For more detailed information about this feature, see
[Importing Postgres databases](database_import.md).

!!! Important
    Bootstrapping from an existing cluster opens up the possibility
    of creating a *replica cluster*. A replica cluster is an independent PostgreSQL
    cluster that's in continuous recovery, synchronized with the source,
    and that accepts read-only connections.

!!! Warning
    CloudNativePG requires both the postgres user and the `postgres` database to
    always exists. Using the local Unix domain socket, it needs to connect
    as the postgres user to the `postgres` database via `peer` authentication
    to perform administrative tasks on the cluster.  
    **DO NOT DELETE** the postgres user or the `postgres` database.

!!! Info
    CloudNativePG is gradually introducing support for the
    [Kubernetes' native `VolumeSnapshot` API](https://github.com/cloudnative-pg/cloudnative-pg/issues/2081)
    for both incremental and differential copy in backup and recovery
    operations if supported by the underlying storage classes.
    See [Recovery from volume snapshot objects](recovery.md#recovery-from-volumesnapshot-objects)
    for details.

## The `bootstrap` section

You can define the bootstrap method in the `bootstrap` section of the cluster
specification. CloudNativePG currently supports the following bootstrap methods:

- `initdb` – Initialize a new PostgreSQL cluster (default).
- `recovery` – Create a PostgreSQL cluster by restoring from a base backup of an
  existing cluster and, if needed, replaying all the available WAL files or up to
  a given point in time.
- `pg_basebackup` – Create a PostgreSQL cluster by cloning an existing one of
  the same major version using `pg_basebackup` by way of streaming replication protocol. This approach is
  useful if you want to migrate databases to CloudNativePG, even
  from outside Kubernetes.

Both `recovery` and `pg_basebackup` differ from the `initdb` method. They
create a cluster based on another one (either offline or online) and can be
used to spin up replica clusters. They both rely on the definition of external
clusters.

The CloudNativePG operator provides several possible backup methods and combinations of backup
storage. See [Recovery](recovery.md) for guidance on each method.

!!! Seealso "API reference"
    See the [API reference for the `bootstrap` section](cloudnative-pg.v1.md#postgresql-cnpg-io-v1-BootstrapConfiguration)
    for more information.

## The `externalClusters` section

The `externalClusters` section allows you to define one or more PostgreSQL
clusters that are somehow related to the current one. In the future,
this section will enable more complex scenarios. However, it's currently intended
to define a cross-region PostgreSQL cluster based on physical replication
and spanning over different Kubernetes clusters or even traditional VM or bare-metal
environments.

As far as bootstrapping is concerned, you can use `externalClusters` 
to define the source PostgreSQL cluster for either the `pg_basebackup`
method or the `recovery` one. An external cluster needs:

- A name that identifies the origin cluster, to use as a reference by way of the
  `source` option
- At least one of the following:

    - Information about streaming connection
    - Information about the *recovery object store*, which is a Barman cloud-compatible 
    object store that contains:
        - The WAL archive (required for point-in-time recovery)
        - The catalog of physical base backups for the Postgres cluster

!!! Note
    A recovery object store is normally an AWS S3, Azure Blob Storage,
    or Google Cloud Storage source that's managed by Barman Cloud.

When only the streaming connection is defined, the source can be used for the
`pg_basebackup` method. When only the recovery object store is defined, the
source can be used for the `recovery` method. When both are defined, you can choose either of the
two bootstrap methods.

Furthermore, in the case of `pg_basebackup` or full `recovery` point in time, the
cluster is eligible for replica cluster mode. This means that the cluster is
continuously fed from the source, by way of streaming or WAL shipping
through the PostgreSQL's `restore_command`, or by either of the two.

!!! Seealso "API reference"
    See the [API reference for the `externalClusters` section](cloudnative-pg.v1.md#postgresql-cnpg-io-v1-ExternalCluster)
    for more information.

## Bootstrap an empty cluster (`initdb`)

The `initdb` bootstrap method is the default method for creating a PostgreSQL cluster from
scratch.

This example contains the full structure of the `initdb`
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

This example bootstrap:

1. Creates a `PGDATA` folder using PostgreSQL's native `initdb` command.
2. Creates an unprivileged user named `app`.
3. Sets the password of `app` using the one in the `app-secret`
   secret. Make sure that `username` matches the name of `owner`.
4. Creates a database called `app` owned by the `app` user.

Because of the [convention-over-configuration](https://en.wikipedia.org/wiki/Convention_over_configuration) paradigm, you can let the
operator choose a default database name (`app`) and a default application
user name (same as the database name). You can also let the operator randomly generate a
secure password for both the superuser and the application user in
PostgreSQL.

Alternatively, you can generate your password, store it as a secret,
and use it in the PostgreSQL cluster, as the example showed.

The supplied secret must comply with the specifications of the
[`kubernetes.io/basic-auth` type](https://kubernetes.io/docs/concepts/configuration/secret/#basic-authentication-secret).
As a result, the `username` in the secret must match the one of the `owner`
for the application secret and `postgres` for the superuser one.

This example shows a `basic-auth` secret:

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

The application database is the one to use to store application
data. Applications must connect to the cluster with the user that owns
the application database.

!!! Important
    If you need to create more users, see
    [Declarative database role management](declarative_role_management.md).

If you don't supply any database name, the operator proceeds
by convention and creates the `app` database. It adds it to the cluster
definition using a [defaulting webhook](https://cluster-api.sigs.k8s.io/developer/providers/webhooks.html#defaulting-webhooks).
The user that owns the database defaults to the database name instead.

The operator doesn't use the application user. It instead
relies on the superuser to reconcile the cluster with the desired status.

### Passing options to `initdb`

The actual PostgreSQL data directory is created by invoking the
`initdb` PostgreSQL command. If you need to add custom options to that command
(that is, to change the `locale` used for the template databases or to add data
checksums), you can use the following parameters:

dataChecksums
:   When `dataChecksums` is set to `true`, CNPG invokes the `-k` option in
    `initdb` to enable checksums on data pages. It also helps detect corruption by the
    I/O system that is otherwise silent (default: `false`).

encoding
:   When `encoding` is set to a value, CNPG passes it to the `--encoding` option in `initdb`,
    which selects the encoding of the template database (default: `UTF8`).

localeCollate
:   When `localeCollate` is set to a value, CNPG passes it to the `--lc-collate`
    option in `initdb`. This option controls the collation order (`LC_COLLATE`
    subcategory), as defined in [Locale Support](https://www.postgresql.org/docs/current/locale.html)
    from the PostgreSQL documentation (default: `C`).

localeCType
:   When `localeCType` is set to a value, CNPG passes it to the `--lc-ctype` option in
    `initdb`. This option controls the collation order (`LC_CTYPE` subcategory), as
    defined in [Locale Support](https://www.postgresql.org/docs/current/locale.html)
    from the PostgreSQL documentation (default: `C`).

walSegmentSize
:   When `walSegmentSize` is set to a value, CNPG passes it to the `--wal-segsize`
    option in `initdb` (default: not set - defined by PostgreSQL as 16MB).

!!! Note
    The only two locale options that CloudNativePG implements during
    the `initdb` bootstrap refer to the `LC_COLLATE` and `LC_TYPE` subcategories.
    You can configure the remaining locale subcategories directly in the PostgreSQL
    configuration using the `lc_messages`, `lc_monetary`, `lc_numeric`, and
    `lc_time` parameters.

This example enables data checksums and sets the default encoding to
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

You can also specify a custom list of queries that execute
once, just after the database is created and configured. These queries are
executed as the superuser (postgres), connected to the `postgres`
database:

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

!!! Warning
    Use the `postInitSQL`, `postInitApplicationSQL`, and
    `postInitTemplateSQL` options with extreme care, as queries are run as a
    superuser and can disrupt the entire cluster. An error in any of those queries
    interrupts the bootstrap phase, leaving the cluster incomplete.

### Executing queries after initialization

You can specify a list of secrets or ConfigMaps that contains an
SQL script that executes after the database is created and configured.
These SQL scripts are executed using the superuser role (postgres) and
connected to the database specified in the `initdb` section:

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
    The SQL scripts referenced in `secretRefs` are executed before the ones
    referenced in `configMapRefs`. For both sections, the SQL scripts are
    executed respecting the order in the list. Inside SQL scripts, each SQL
    statement is executed in a single exec on the server according to the
    [PostgreSQL semantics](https://www.postgresql.org/docs/current/protocol-flow.html#PROTOCOL-FLOW-MULTI-STATEMENT).
    You can include comments but not internal commands like psql.

!!! Warning
    Make sure the existence of the entries inside the ConfigMaps or
    secrets are specified in `postInitApplicationSQLRefs`. Otherwise, the bootstrap
    fails. Errors in any of those SQL files prevent the bootstrap phase from
    completing successfully.

## Bootstrap from another cluster

CloudNativePG enables the bootstrap of a cluster starting from
another cluster of the same major version.
You can perform this operation by connecting directly to the source cluster by way of
streaming replication (`pg_basebackup`) or indirectly by way of an existing
physical base backup (`recovery`).

The source cluster must be defined in the `externalClusters` section, identified
by `name`. We recommend using the same `name` as that of the origin cluster.

!!! Important
    By default, the `recovery` method strictly uses the `name` of the
    cluster in the `externalClusters` section to locate the main folder
    of the backup data in the object store, which is normally reserved
    for the name of the server. You can specify a different name using the
    `barmanObjectStore.serverName` property, which by default is assigned to the
    value of `name` in the external cluster definition.

### Bootstrap from a backup (`recovery`)

Given the several possibilities, methods, and combinations that the
CloudNativePG operator provides in terms of backup and recovery, see
[Recovery](recovery.md) for complete details.

### Bootstrap from a live cluster (`pg_basebackup`)

The `pg_basebackup` bootstrap mode lets you create a cluster (the target) as
an exact physical copy of an existing and binary compatible PostgreSQL
instance (the source), through a valid streaming replication connection.
The source instance can be either a primary or a standby PostgreSQL server.

The main use case for this method is represented by migrations to CloudNativePG,
either from outside Kubernetes or within Kubernetes, for example, from another operator.

!!! Warning
    The current implementation creates a snapshot of the origin PostgreSQL
    instance when the cloning process terminates and immediately starts
    the created cluster. See [Current limitations](#current-limitations) for details.

Similar to the case of the `recovery` bootstrap method, once the clone operation
completes, the operator takes ownership of the target cluster, starting from
the first instance. This includes overriding some configuration parameters, as
required by CloudNativePG, resetting the superuser password, creating
the `streaming_replica` user, managing the replicas, and so on. The resulting
cluster is completely independent of the source instance.

!!! Important
    Configuring the network between the target instance and the source instance
    is beyond the scope of CloudNativePG documentation, as it depends
    on the actual context and environment.

The streaming replication client on the target instance, which is
transparently managed by `pg_basebackup`, can authenticate itself on the source
instance either by:

- Username and password
- TLS client certificate

Using a TLS client certificate is recommended if you connect to a source managed
by CloudNativePG or configured for TLS authentication.
The username/password option, however, is the most common form of authentication to a
PostgreSQL server in general. It might be the easiest way if the source
instance is on a traditional environment outside Kubernetes. 

For details on these two methods, see:

- [Username/password authentication](#usernamepassword-authentication) 
- [TLS client certificate authentication](#tls-certificate-authentication).

#### Requirements

The following requirements apply to the `pg_basebackup` bootstrap method:

- Target and source must have the same hardware architecture.
- Target and source must have the same major PostgreSQL version.
- Source must not have any tablespace defined. (See [Current limitations](#current-limitations).)
- Source must be configured with enough `max_wal_senders` to grant
  access from the target for this one-off operation by providing at least
  one walsender for the backup plus one for WAL streaming.
- The network between source and target must be configured to enable the target
  instance to connect to the PostgreSQL port on the source instance.
- Source must have a role with `REPLICATION LOGIN` privileges and must accept
  connections from the target instance for this role in `pg_hba.conf`, preferably
  by way of TLS. (See [About the replication user](#about-the-replication-user).)
- Target must be able to successfully connect to the source PostgreSQL instance
  using a role with `REPLICATION LOGIN` privileges.

!!! Seealso
    For more information, see
    [Planning for warm standby](https://www.postgresql.org/docs/current/warm-standby.html#STANDBY-PLANNING),
    [`pg_basebackup`](https://www.postgresql.org/docs/current/app-pgbasebackup.html),
    and
    [High Availability, Load Balancing, and Replication](https://www.postgresql.org/docs/current/high-availability.html)
    in the PostgreSQL documentation.

#### About the replication user

As explained in [Requirements](#requirements), you need to have a user
with either the `SUPERUSER` or, preferably, just the `REPLICATION`
privilege in the source instance.

If the source database is created with CloudNativePG, you
can reuse the `streaming_replica` user and take advantage of client
TLS certificates authentication. (By default, TLS certificates authentication is the only allowed
connection method for `streaming_replica`.)

For all other cases, including outside Kubernetes, verify that
you already have a user with the `REPLICATION` privilege, or create
one. 

To create the user, as postgres user on the source system, run:

```console
createuser -P --replication streaming_replica
```

Enter the password at the prompt and save it for later, as you
need to add it to a secret in the target instance.

!!! Note
    Although the name isn't important, the examples that follow use `streaming_replica`
    for the sake of simplicity. Feel free to change it,
    and adapt the instructions in the instructions that follow.

#### Username/password authentication

The first authentication method supported by CloudNativePG
with the `pg_basebackup` bootstrap is based on username and password matching.

Make sure you have the following information before you start to set up authentication:

- Location of the source instance, identified by a hostname or an IP address
  and a TCP port
- Replication username (`streaming_replica` in the examples)
- Password

You might need to add a line similar to the following to the `pg_hba.conf`
file on the source PostgreSQL instance:

```
# A more restrictive rule for TLS and IP of origin is recommended
host replication streaming_replica all md5
```

The following manifest creates a PostgreSQL 16.1 cluster,
called `target-db`. It uses the `pg_basebackup` bootstrap method
to clone an external PostgreSQL cluster defined as `source-db`
in the `externalClusters` array. The `source-db`
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
  imageName: ghcr.io/cloudnative-pg/postgresql:16.1

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
the same PostgreSQL version (in this case 16.1).

#### TLS certificate authentication

The second authentication method supported by CloudNativePG
with the `pg_basebackup` bootstrap is based on TLS client certificates.
From a security standpoint, we recommend this approach.

The following example clones an existing PostgreSQL cluster (`cluster-example`)
in the same Kubernetes cluster.

!!! Note
    You can adapt this example to cover an instance that resides
    outside the Kubernetes cluster.

The manifest defines a PostgreSQL 16.1 cluster called `cluster-clone-tls`,
which is bootstrapped using the `pg_basebackup` method from the `cluster-example`
external cluster. The host is identified by the read/write service
in the same cluster. The `streaming_replica` user is authenticated
with the provided keys, certificate, and certification authority
information (in the `cluster-example-replication` and
`cluster-example-ca` secrets, respectively).

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-clone-tls
spec:
  instances: 3
  imageName: ghcr.io/cloudnative-pg/postgresql:16.1

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

We also support configuring the application database for a cluster that bootstraps
from a live cluster, similar to the `initdb` and  `recovery` bootstrap methods.
If the cluster is created as a replica cluster (with replica mode enabled), application
database configuration is skipped.

This example configures the application database `app` with the password in
the supplied secret `app-secret` after a bootstrap from a live cluster.

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

With this configuration, the following happens after recovery is completed:

1. If database `app` doesn't exist, a database `app` is created.
2. If user `app` doesn't exist, a user `app` is created.
3. If user `app` isn't the owner of the database, the user `app` is made
   owner of the database `app`.
4. If the value of `username` matches the value of `owner` in the secret, the password of
   the application database is changed to the value of `password` in the secret.

!!! Important
    For a replica cluster with replica mode enabled, the operator doesn't
    create any database or user in the PostgreSQL instance, as these are
    recovered from the original cluster.

#### Current limitations

##### Missing tablespace support

CloudNativePG doesn't currently include full declarative management
of PostgreSQL global objects, namely roles, databases, and tablespaces.
While roles and databases are copied from the source instance to the target
cluster, tablespaces require a capability that this version of
CloudNativePG is missing: definition and management of additional
persistent volumes. When dealing with base backup and tablespaces, PostgreSQL
requires that the exact mount points in the source instance
also exist in the target instance, in our case, the pods in Kubernetes
that CloudNativePG manages. For this reason, you can't directly
migrate a PostgreSQL instance that takes advantage
of tablespaces in CloudNativePG. You first need to remove the tablespaces from the source or, if your
organization requires this feature, contact EDB to prioritize it.

##### Snapshot copy

The `pg_basebackup` method takes a snapshot of the source instance in the form of
a PostgreSQL base backup. All transactions written from the start of
the backup to the correct termination of the backup are streamed to the target
instance using a second connection (see the `--wal-method=stream` option for
`pg_basebackup`).

Once the backup is completed, the new instance starts on a new timeline
and diverges from the source.
For this reason, we recommend stopping all write operations to the source database
before migrating to the target database in Kubernetes.

!!! Important
    Before you attempt a migration, you must test both the procedure
    and the applications. In particular, it's fundamental that you run the migration
    procedure as many times as needed to systematically measure the downtime of your
    applications in production. You can contact EDB for assistance.
