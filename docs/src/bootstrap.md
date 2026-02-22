---
id: bootstrap
sidebar_position: 80
title: Bootstrap
---

# Bootstrap
<!-- SPDX-License-Identifier: CC-BY-4.0 -->

This section describes the options available to create a new
PostgreSQL cluster and the design rationale behind them.
There are primarily two ways to bootstrap a new cluster:

- from scratch (`initdb`)
- from an existing PostgreSQL cluster, either directly (`pg_basebackup`)
  or indirectly through a physical base backup (`recovery`)

The `initdb` bootstrap also provides the option to import one or more
databases from an existing PostgreSQL cluster, even if it's outside
Kubernetes or running a different major version of PostgreSQL.
For more detailed information about this feature, please refer to the
["Importing Postgres databases"](database_import.md) section.

:::info[Important]
    Bootstrapping from an existing cluster enables the creation of a
    **replica cluster**—an independent PostgreSQL cluster that remains in
    continuous recovery, stays synchronized with the source cluster, and
    accepts read-only connections.
    For more details, refer to the [Replica Cluster section](replica_cluster.md).
:::

:::warning
    CloudNativePG requires both the `postgres` user and database to
    always exist. Using the local Unix Domain Socket, it needs to connect
    as the `postgres` user to the `postgres` database via `peer` authentication in
    order to perform administrative tasks on the cluster.
    **DO NOT DELETE** the `postgres` user or the `postgres` database!!!
:::

:::info
    CloudNativePG is gradually introducing support for
    [Kubernetes' native `VolumeSnapshot` API](https://github.com/cloudnative-pg/cloudnative-pg/issues/2081)
    for both incremental and differential copy in backup and recovery
    operations - if supported by the underlying storage classes.
    Please see ["Recovery from Volume Snapshot objects"](recovery.md#recovery-from-volumesnapshot-objects)
    for details.
:::

## The `bootstrap` section

The *bootstrap* method can be defined in the `bootstrap` section of the cluster
specification. CloudNativePG currently supports the following bootstrap methods:

- `initdb`: initialize a new PostgreSQL cluster (default)
- `recovery`: create a PostgreSQL cluster by restoring from a base backup of an
  existing cluster and, if needed, replaying all the available WAL files or up to
  a given *point in time*
- `pg_basebackup`: create a PostgreSQL cluster by cloning an existing one of
  the same major version using `pg_basebackup` through the streaming
  replication protocol. This method is particularly useful for migrating
  databases to CloudNativePG, although meeting all requirements can be
  challenging. Be sure to review the warnings in the
  [`pg_basebackup` subsection](#bootstrap-from-a-live-cluster-pg_basebackup)
  carefully.

Only one bootstrap method can be specified in the manifest.
Attempting to define multiple bootstrap methods will result in validation errors.

In contrast to the `initdb` method, both `recovery` and `pg_basebackup`
create a new cluster based on another one (either offline or online) and can be
used to spin up replica clusters. They both rely on the definition of external
clusters.
Refer to the [replica cluster section](replica_cluster.md) for more information.

Given the amount of possible backup methods and combinations of backup
storage that the CloudNativePG operator provides for `recovery`, please refer to
the dedicated ["Recovery" section](recovery.md) for guidance on each method.

:::note[API reference]
    Please refer to the ["API reference for the `bootstrap` section](cloudnative-pg.v1.md#bootstrapconfiguration)
    for more information.
:::

## The `externalClusters` section

The `externalClusters` section of the cluster manifest can be used to configure
access to one or more PostgreSQL clusters as *sources*.
The primary use cases include:

1. **Importing Databases:** Specify an external source to be utilized during
  the [importation of databases](database_import.md) via logical backup and
  restore, as part of the `initdb` bootstrap method.
2. **Cross-Region Replication:** Define a cross-region PostgreSQL cluster
  employing physical replication, capable of extending across distinct Kubernetes
  clusters or traditional VM/bare-metal environments.
3. **Recovery from Physical Base Backup:** Recover, fully or at a
  given Point-In-Time, a PostgreSQL cluster by referencing a physical base
  backup.

:::info
    Ongoing development will extend the functionality of `externalClusters` to
    accommodate additional use cases, such as logical replication and foreign
    servers in future releases.
:::

As far as bootstrapping is concerned, `externalClusters` can be used
to define the source PostgreSQL cluster for either the `pg_basebackup`
method or the `recovery` one. An external cluster needs to have:

- a name that identifies the external cluster, to be used as a reference via the
  `source` option
- at least one of the following:

    - information about streaming connection
    - information about the **recovery object store**, which is a Barman Cloud
      compatible object store that contains:
        - the WAL archive (required for Point In Time Recovery)
        - the catalog of physical base backups for the Postgres cluster

:::note
    A recovery object store is normally an AWS S3, Azure Blob Storage,
    or Google Cloud Storage source that is managed by Barman Cloud.
:::

When only the streaming connection is defined, the source can be used for the
`pg_basebackup` method. When only the recovery object store is defined, the
source can be used for the `recovery` method. When both are defined, any of
the two bootstrap methods can be chosen. The following table summarizes your
options:

| Content of externalClusters | pg_basebackup | recovery |
|:----------------------------|:-------------:|:--------:|
| Only streaming              | ✓             |          |
| Only object store           |               | ✓        |
| Streaming and object store  | ✓             | ✓        |

Furthermore, in case of `pg_basebackup` or full `recovery` point in time, the
cluster is eligible for replica cluster mode. This means that the cluster is
continuously fed from the source, either via streaming, via WAL shipping
through the PostgreSQL's `restore_command`, or any of the two.

:::note[API reference]
    Please refer to the ["API reference for the `externalClusters` section](cloudnative-pg.v1.md#externalcluster)
    for more information.
:::

### Password files

Whenever a password is supplied within an `externalClusters` entry,
CloudNativePG autonomously manages a [PostgreSQL password file](https://www.postgresql.org/docs/current/libpq-pgpass.html)
for it, residing at `/controller/external/NAME/pgpass` in each instance.

This approach enables CloudNativePG to securely establish connections with an
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

:::info[Important]
    If you need to create additional users, please refer to
    ["Declarative database role management"](declarative_role_management.md).
:::

In case you don't supply any database name, the operator will proceed
by convention and create the `app` database, and adds it to the cluster
definition using a *defaulting webhook*.
The user that owns the database defaults to the database name instead.

The application user is not used internally by the operator, which instead
relies on the superuser to reconcile the cluster with the desired status.

### Passing Options to `initdb`

The PostgreSQL data directory is initialized using the
[`initdb` PostgreSQL command](https://www.postgresql.org/docs/current/app-initdb.html).

CloudNativePG enables you to customize the behavior of `initdb` to modify
settings such as default locale configurations and data checksums.

:::warning
    CloudNativePG acts only as a direct proxy to `initdb` for locale-related
    options, due to the ongoing and significant enhancements in PostgreSQL's locale
    support. It is your responsibility to ensure that the correct options are
    provided, following the PostgreSQL documentation, and to verify that the
    bootstrap process completes successfully.
:::

To include custom options in the `initdb` command, you can use the following
parameters:

builtinLocale
:   When `builtinLocale` is set to a value, CloudNativePG passes it to the
    `--builtin-locale` option in `initdb`. This option controls the builtin locale, as
    defined in ["Locale Support"](https://www.postgresql.org/docs/current/locale.html)
    from the PostgreSQL documentation (default: empty). Note that this option requires
    `localeProvider` to be set to `builtin`. Available from PostgreSQL 17.

dataChecksums
:   When `dataChecksums` is set to `true`, CloudNativePG invokes the `-k` option in
    `initdb` to enable checksums on data pages and help detect corruption by the
    I/O system - that would otherwise be silent (default: `false`).

encoding
:   When `encoding` set to a value, CloudNativePG passes it to the `--encoding`
    option in `initdb`, which selects the encoding of the template database
    (default: `UTF8`).

icuLocale
:   When `icuLocale` is set to a value, CloudNativePG passes it to the
    `--icu-locale` option in `initdb`. This option controls the ICU locale, as
    defined in ["Locale Support"](https://www.postgresql.org/docs/current/locale.html)
    from the PostgreSQL documentation (default: empty).
    Note that this option requires `localeProvider` to be set to `icu`.
    Available from PostgreSQL 15.

icuRules
:   When `icuRules` is set to a value, CloudNativePG passes it to the
    `--icu-rules` option in `initdb`. This option controls the ICU locale, as
    defined in ["Locale
    Support"](https://www.postgresql.org/docs/current/locale.html) from the
    PostgreSQL documentation (default: empty). Note that this option requires
    `localeProvider` to be set to `icu`. Available from PostgreSQL 16.

locale
:   When `locale` is set to a value, CloudNativePG passes it to the `--locale`
    option in `initdb`. This option controls the locale, as defined in
    ["Locale Support"](https://www.postgresql.org/docs/current/locale.html) from
    the PostgreSQL documentation. By default, the locale parameter is empty. In
    this case, environment variables such as `LANG` are used to determine the
    locale. Be aware that these variables can vary between container images,
    potentially leading to inconsistent behavior.

localeCollate
:   When `localeCollate` is set to a value, CloudNativePG passes it to the `--lc-collate`
    option in `initdb`. This option controls the collation order (`LC_COLLATE`
    subcategory), as defined in ["Locale Support"](https://www.postgresql.org/docs/current/locale.html)
    from the PostgreSQL documentation (default: `C`).

localeCType
:   When `localeCType` is set to a value, CloudNativePG passes it to the `--lc-ctype` option in
    `initdb`. This option controls the collation order (`LC_CTYPE` subcategory), as
    defined in ["Locale Support"](https://www.postgresql.org/docs/current/locale.html)
    from the PostgreSQL documentation (default: `C`).

localeProvider
:   When `localeProvider` is set to a value, CloudNativePG passes it to the `--locale-provider`
option in `initdb`. This option controls the locale provider, as defined in
["Locale Support"](https://www.postgresql.org/docs/current/locale.html) from the
PostgreSQL documentation (default: empty, which means `libc` for PostgreSQL).
Available from PostgreSQL 15.

walSegmentSize
:   When `walSegmentSize` is set to a value, CloudNativePG passes it to the `--wal-segsize`
    option in `initdb` (default: not set - defined by PostgreSQL as 16 megabytes).

:::note
    The only two locale options that CloudNativePG implements during
    the `initdb` bootstrap refer to the `LC_COLLATE` and `LC_TYPE` subcategories.
    The remaining locale subcategories can be configured directly in the PostgreSQL
    configuration, using the `lc_messages`, `lc_monetary`, `lc_numeric`, and
    `lc_time` parameters.
:::

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

:::warning
    CloudNativePG supports another way to customize the behavior of the
    `initdb` invocation, using the `options` subsection. However, given that there
    are options that can break the behavior of the operator (such as `--auth` or
    `-d`), this technique is deprecated and will be removed from future versions of
    the API.
:::

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

:::warning
    Use the `postInit`, `postInitTemplate`, and `postInitApplication` options
    with extreme care, as queries are run as a superuser and can disrupt the entire
    cluster. An error in any of those queries will interrupt the bootstrap phase,
    leaving the cluster incomplete and requiring manual intervention.
:::

:::info[Important]
    Ensure the existence of entries inside the ConfigMaps or Secrets specified
    in `postInitSQLRefs`, `postInitTemplateSQLRefs`, and
    `postInitApplicationSQLRefs`, otherwise the bootstrap will fail. Errors in any
    of those SQL files will prevent the bootstrap phase from completing
    successfully.
:::

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

:::note
    Within SQL scripts, each SQL statement is executed in a single exec on the
    server according to the [PostgreSQL semantics](https://www.postgresql.org/docs/current/protocol-flow.html#PROTOCOL-FLOW-MULTI-STATEMENT).
    Comments can be included, but internal commands like `psql` cannot.
:::

## Bootstrap from another cluster

CloudNativePG enables bootstrapping a cluster starting from
another one of the same major version.
This operation can be carried out either connecting directly to the source cluster via
streaming replication (`pg_basebackup`), or indirectly via an existing
physical *base backup* (`recovery`).

The source cluster must be defined in the `externalClusters` section, identified
by `name` (our recommendation is to use the same `name` of the origin cluster).

:::info[Important]
    By default the `recovery` method strictly uses the `name` of the
    cluster in the `externalClusters` section to locate the main folder
    of the backup data within the object store, which is normally reserved
    for the name of the server. Backup plugins provide ways to specify a
    different one. For example, the Barman Cloud Plugin provides the [`serverName` parameter](https://cloudnative-pg.io/plugin-barman-cloud/docs/parameters/)
    (by default assigned to the value of `name` in the external cluster definition).
:::

### Bootstrap from a backup (`recovery`)

Given the variety of backup methods and combinations of backup storage
options provided by the CloudNativePG operator for `recovery`, please refer
to the dedicated ["Recovery" section](recovery.md) for detailed guidance on
each method.

### Bootstrap from a live cluster (`pg_basebackup`)

The `pg_basebackup` bootstrap mode allows you to create a new cluster
(*target*) as an exact physical copy of an existing and **binary-compatible**
PostgreSQL instance (*source*) managed by CloudNativePG, using a valid
*streaming replication* connection. The source instance can either be a primary
or a standby PostgreSQL server. It’s crucial to thoroughly review the
requirements section below, as the pros and cons of PostgreSQL physical
replication fully apply.

The primary use cases for this method include:

- Reporting and business intelligence clusters that need to be regenerated
  periodically (daily, weekly)
- Test databases containing live data that require periodic regeneration
  (daily, weekly, monthly) and anonymization
- Rapid spin-up of a standalone replica cluster
- Physical migrations of CloudNativePG clusters to different namespaces or
  Kubernetes clusters

:::info[Important]
    Avoid using this method, based on physical replication, to migrate an
    existing PostgreSQL cluster outside of Kubernetes into CloudNativePG, unless you
    are completely certain that all [requirements](#requirements) are met and
    the operation has been
    thoroughly tested. The CloudNativePG community does not endorse this approach
    for such use cases, and recommends using logical import instead. It is
    exceedingly rare that all requirements for physical replication are met in a
    way that seamlessly works with CloudNativePG.
:::

:::warning
    In its current implementation, this method clones the source PostgreSQL
    instance, thereby creating a *snapshot*. Once the cloning process has finished,
    the new cluster is immediately started.
    Refer to ["Current limitations"](#current-limitations) for more details.
:::

Similar to the `recovery` bootstrap method, once the cloning operation is
complete, the operator takes full ownership of the target cluster, starting
from the first instance. This includes overriding certain configuration
parameters as required by CloudNativePG, resetting the superuser password,
creating the `streaming_replica` user, managing replicas, and more. The
resulting cluster operates independently from the source instance.

:::info[Important]
    Configuring the network connection between the target and source instances
    lies outside the scope of CloudNativePG documentation, as it depends heavily on
    the specific context and environment.
:::

The streaming replication client on the target instance, managed transparently
by `pg_basebackup`, can authenticate on the source instance using one of the
following methods:

1. [Username/password](#usernamepassword-authentication)
2. [TLS client certificate](#tls-certificate-authentication)

Both authentication methods are detailed below.

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

:::note[Seealso]
    For further information, please refer to the
    ["Planning" section for Warm Standby](https://www.postgresql.org/docs/current/warm-standby.html#STANDBY-PLANNING),
    the
    [`pg_basebackup` page](https://www.postgresql.org/docs/current/app-pgbasebackup.html)
    and the
    ["High Availability, Load Balancing, and Replication" chapter](https://www.postgresql.org/docs/current/high-availability.html)
    in the PostgreSQL documentation.
:::

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

:::note
    Although the name is not important, we will use `streaming_replica`
    for the sake of simplicity. Feel free to change it as you like,
    provided you adapt the instructions in the following sections.
:::

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

The following manifest creates a new PostgreSQL 18.2 cluster,
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
  imageName: ghcr.io/cloudnative-pg/postgresql:18.2-system-trixie

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
the same PostgreSQL version (in our case 18.2).

#### TLS certificate authentication

The second authentication method supported by CloudNativePG
with the `pg_basebackup` bootstrap is based on TLS client certificates.
This is the recommended approach from a security standpoint.

The following example clones an existing PostgreSQL cluster (`cluster-example`)
in the same Kubernetes cluster.

:::note
    This example can be easily adapted to cover an instance that resides
    outside the Kubernetes cluster.
:::

The manifest defines a new PostgreSQL 18.2 cluster called `cluster-clone-tls`,
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
  imageName: ghcr.io/cloudnative-pg/postgresql:18.2-system-trixie

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

:::info[Important]
    While the `Cluster` is in recovery mode, no changes to the database,
    including the catalog, are permitted. This restriction includes any role
    overrides, which are deferred until the `Cluster` transitions to primary.
    During the recovery phase, roles remain as defined in the source cluster.
:::

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
before migrating to the target database.

Note that this limitation applies only if the target cluster is not defined as
a replica cluster.

:::info[Important]
    Before you attempt a migration, you must test both the procedure
    and the applications. In particular, it is fundamental that you run the migration
    procedure as many times as needed to systematically measure the downtime of your
    applications in production.
:::