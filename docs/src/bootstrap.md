# Bootstrap

This section describes the options you have to create a new
PostgreSQL cluster and the design rationale behind them.
There are primarily two ways to bootstrap a new cluster:

- from scratch (`initdb`)
- from an existing PostgreSQL cluster, either directly (`pg_basebackup`)
  or indirectly (`recovery`)

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
    Please see ["Recovery from Volume Snapshot objects"](#recovery-from-volumesnapshot-objects)
    for details.

## The `bootstrap` section

The *bootstrap* method can be defined in the `bootstrap` section of the cluster
specification. CloudNativePG currently supports the following bootstrap methods:

- `initdb`: initialize a new PostgreSQL cluster (default)
- `recovery`: create a PostgreSQL cluster by restoring from a base backup of an
  existing cluster, and replaying all the available WAL files or up to
  a given *point in time*
- `pg_basebackup`: create a PostgreSQL cluster by cloning an existing one of
  the same major version using `pg_basebackup` via streaming replication protocol -
  useful if you want to migrate databases to CloudNativePG, even
  from outside Kubernetes.

Differently from the `initdb` method, both `recovery` and `pg_basebackup`
create a new cluster based on another one (either offline or online) and can be
used to spin up replica clusters. They both rely on the definition of external
clusters.

!!! Seealso "API reference"
    Please refer to the ["API reference for the `bootstrap` section](api_reference.md#BootstrapConfiguration)
    for more information.

## The `externalClusters` section

The `externalClusters` section allows you to define one or more PostgreSQL
clusters that are somehow related to the current one. While in the future
this section will enable more complex scenarios, it is currently intended
to define a cross-region PostgreSQL cluster based on physical replication,
and spanning over different Kubernetes clusters or even traditional VM/bare-metal
environments.

As far as bootstrapping is concerned, `externalClusters` can be used
to define the source PostgreSQL cluster for either the `pg_basebackup`
method or the `recovery` one. An external cluster needs to have:

- a name that identifies the origin cluster, to be used as a reference via the
  `source` option
- at least one of the following:

    - information about streaming connection
    - information about the **recovery object store**, which is a Barman Cloud
      compatible object store that contains the backup files of the source
      cluster - that is, WAL archive and base backups.

!!! Note
    A recovery object store is normally an AWS S3, or an Azure Blob Storage,
    or a Google Cloud Storage source that is managed by Barman Cloud.

When only the streaming connection is defined, the source can be used for the
`pg_basebackup` method. When only the recovery object store is defined, the
source can be used for the `recovery` method. When both are defined, any of the
two bootstrap methods can be chosen.

Furthermore, in case of `pg_basebackup` or full `recovery`point in time), the
cluster is eligible for replica cluster mode. This means that the cluster is
continuously fed from the source, either via streaming, via WAL shipping
through the PostgreSQL's `restore_command`, or any of the two.

!!! Seealso "API reference"
    Please refer to the ["API reference for the `externalClusters` section](api_reference.md#ExternalCluster)
    for more information.

## Bootstrap an empty cluster (`initdb`)

The `initdb` bootstrap method is used to create a new PostgreSQL cluster from
scratch. It is the default one unless specified differently.

The following example contains the full structure of the `initdb` configuration:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-example-initdb
spec:
  instances: 3

  superuserSecret:
    name: superuser-secret

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
2. set a password for the `postgres` *superuser* from the secret named `superuser-secret`
3. create an *unprivileged* user named `app`
4. set the password of the latter (`app`) using the one in the `app-secret`
   secret (make sure that `username` matches the same name of the `owner`)
5. create a database called `app` owned by the `app` user.

Thanks to the *convention over configuration paradigm*, you can let the
operator choose a default database name (`app`) and a default application
user name (same as the database name), as well as randomly generate a
secure password for both the superuser and the application user in
PostgreSQL.

Alternatively, you can generate your passwords, store them as secrets,
and use them in the PostgreSQL cluster - as described in the above example.

The supplied secrets must comply with the specifications of the
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
    Future implementations of the operator might allow you to create
    additional users in a declarative configuration fashion.

The `postgres` superuser and the `postgres` database are supposed to be used
only by the operator to configure the cluster.

In case you don't supply any database name, the operator will proceed
by convention and create the `app` database, and adds it to the cluster
definition using a *defaulting webhook*.
The user that owns the database defaults to the database name instead.

The application user is not used internally by the operator, which instead
relies on the superuser to reconcile the cluster with the desired status.

!!! Important
    For now, changes to the name of the superuser secret are not applied
    to the cluster.

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

CloudNativePG supports another way to customize the behavior of the
`initdb` invocation, using the `options` subsection. However, given that there
are options that can break the behavior of the operator (such as `--auth` or
`-d`), this technique is deprecated and will be removed from future versions of
the API.

You can also specify a custom list of queries that will be executed
once, just after the database is created and configured. These queries will
be executed as the *superuser* (`postgres`), connected to the `postgres`
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
        - CREATE ROLE angus
        - CREATE ROLE malcolm
  storage:
    size: 1Gi
```

!!! Warning
    Please use the `postInitSQL`, `postInitApplicationSQL` and `postInitTemplateSQL` options with extreme care,
    as queries are run as a superuser and can disrupt the entire cluster.
    An error in any of those queries interrupts the bootstrap phase, leaving the cluster incomplete.

Moreover, you can specify a list of Secrets and/or ConfigMaps which contains SQL script that will be executed after the database is created and configured. These SQL script will be executed using the **superuser** role (`postgres`), connected to the database specified in the `initdb` section:

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
    The SQL scripts referenced in `secretRefs` will be executed before the ones referenced in `configMapRefs`. For both sections the SQL scripts will be executed respecting the order in the list.
    Inside SQL scripts, each SQL statement is executed in a single exec on the server according to the [PostgreSQL semantics](https://www.postgresql.org/docs/current/protocol-flow.html#PROTOCOL-FLOW-MULTI-STATEMENT), comments can be included, but internal command like `psql` cannot.

!!! Warning
    Please make sure the existence of the entries inside the ConfigMaps or Secrets specified in `postInitApplicationSQLRefs`, otherwise the bootstrap will fail.
    Errors in any of those SQL files will prevent the bootstrap phase to complete successfully.

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

The `recovery` bootstrap mode lets you create a new cluster from an existing
physical base backup, and then reapply the WAL files containing the REDO log
from the archive. Both base backups and WAL files are pulled from the
*recovery object store*.

Recovery from a *recovery object store* can be achieved in two ways:

- using a recovery object store, that is a backup of another cluster
  created by Barman Cloud and defined via the `barmanObjectStore` option
  in the `externalClusters` section (*recommended*)
- using an existing `Backup` object in the same namespace (this was the
  only option available before version 1.8.0).

Both recovery methods enable either full recovery (up to the last
available WAL) or up to a [point in time](#point-in-time-recovery).
When performing a full recovery, the cluster can also be started
in replica mode. Also, make sure that the PostgreSQL configuration
(`.spec.postgresql.parameters`) of the recovered cluster is
compatible, from a physical replication standpoint, with the original one.

!!! Note
    You can find more information about backup and recovery of a running cluster
    in the ["Backup and recovery" page](backup_recovery.md).

CloudNativePG is also introducing support for Kubernetes' volume snapshots.
With the current version of CloudNativePG, you can:

- take a consistent cold backup of the Postgres cluster from a standby through
  the `kubectl cnpg snapshot` command - which creates the necessary
  `VolumeSnapshot` objects (currently one or two, if you have WALs in a separate
  volume)
- recover from the above *VolumeSnapshot* objects through the `volumeSnapshots`
  option in the `.spec.bootstrap.recovery` stanza, as described in
  ["Recovery from `VolumeSnapshot` objects"](#recovery-from-volumesnapshot-objects)
  below

#### Recovery from an object store

You can recover from a backup created by Barman Cloud and stored on a supported
object storage. Once you have defined the external cluster, including all the
required configuration in the `barmanObjectStore` section, you need to
reference it in the `.spec.recovery.source` option. The following example
defines a recovery object store in a blob container in Azure:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-restore
spec:
  [...]
  
  superuserSecret:
    name: superuser-secret
    
  bootstrap:
    recovery:
      source: clusterBackup

  externalClusters:
    - name: clusterBackup
      barmanObjectStore:
        destinationPath: https://STORAGEACCOUNTNAME.blob.core.windows.net/CONTAINERNAME/
        azureCredentials:
          storageAccount:
            name: recovery-object-store-secret
            key: storage_account_name
          storageKey:
            name: recovery-object-store-secret
            key: storage_account_key
        wal:
          maxParallel: 8
```

!!! Important
    By default the `recovery` method strictly uses the `name` of the
    cluster in the `externalClusters` section to locate the main folder
    of the backup data within the object store, which is normally reserved
    for the name of the server. You can specify a different one with the
    `barmanObjectStore.serverName` property (by default assigned to the
    value of `name` in the external clusters definition).

!!! Note
    In the above example we are taking advantage of the parallel WAL restore
    feature, dedicating up to 8 jobs to concurrently fetch the required WAL
    files from the archive. This feature can appreciably reduce the recovery time.
    Make sure that you plan ahead for this scenario and correctly tune the
    value of this parameter for your environment. It will certainly make a
    difference **when** (not if) you'll need it.

#### Recovery from a `Backup` object

In case a Backup resource is already available in the namespace in which the
cluster should be created, you can specify its name through
`.spec.bootstrap.recovery.backup.name`, as in the following example:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-example-initdb
spec:
  instances: 3

  superuserSecret:
    name: superuser-secret

  bootstrap:
    recovery:
      backup:
        name: backup-example

  storage:
    size: 1Gi
```

This bootstrap method allows you to specify just a reference to the
backup that needs to be restored.

#### Recovery from `VolumeSnapshot` objects

CloudNativePG can create a new cluster from a `VolumeSnapshot` of a PVC of an
existing `Cluster` that's been taken with `kubectl cnpg snapshot`.
You need to specify the name of the snapshot as in the following example:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-restore
spec:
  [...]

bootstrap:
    recovery:
      volumeSnapshots:
        storage:
          name: <snapshot name>
          kind: VolumeSnapshot
          apiGroup: snapshot.storage.k8s.io
```

!!! Warning
    As the development of declarative support for Kubernetes' `VolumeSnapshot` API
    progresses, you'll be able to use this technique in conjunction with a WAL
    archive for Point In Time Recovery operations or replica clusters.

In case the backed-up cluster was using a separate PVC to store the WAL files,
the recovery must include that too:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-restore
spec:
  [...]

bootstrap:
    recovery:
      volumeSnapshots:
        storage:
          name: <snapshot name>
          kind: VolumeSnapshot
          apiGroup: snapshot.storage.k8s.io

        walStorage:
          name: <snapshot name>
          kind: VolumeSnapshot
          apiGroup: snapshot.storage.k8s.io
```

The `kubectl cnpg snapshot` command is able to take consistent snapshots of a
replica through a technique known as *cold backup*, by fencing the standby
before taking a physical copy of the volumes. For details, please refer to
["Snapshotting a Postgres cluster"](#snapshotting-a-postgres-cluster).

#### Additional considerations

Whether you recover from a recovery object store or an existing `Backup`
resource, the following considerations apply:

- The application database name and the application database user are preserved
from the backup that is being restored. The operator does not currently attempt
to back up the underlying secrets, as this is part of the usual maintenance
activity of the Kubernetes cluster itself.
- In case you don't supply any `superuserSecret`, a new one is automatically
generated with a secure and random password. The secret is then used to
reset the password for the `postgres` user of the cluster.
- By default, the recovery will continue up to the latest
available WAL on the default target timeline (`current` for PostgreSQL up to
11, `latest` for version 12 and above).
You can optionally specify a `recoveryTarget` to perform a point in time
recovery (see the ["Point in time recovery" section](#point-in-time-recovery)).

!!! Important
    Consider using the `barmanObjectStore.wal.maxParallel` option to speed
    up WAL fetching from the archive by concurrently downloading the transaction
    logs from the recovery object store.

#### Point in time recovery (PITR)

Instead of replaying all the WALs up to the latest one, we can ask PostgreSQL
to stop replaying WALs at any given point in time, after having extracted a
base backup. PostgreSQL uses this technique to achieve *point-in-time* recovery
(PITR).

!!! Note
    PITR is available from recovery object stores as well as `Backup` objects.

The operator will generate the configuration parameters required for this
feature to work in case a recovery target is specified, like in the following
example that uses a recovery object stored in Azure and a timestamp based
goal:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-restore-pitr
spec:
  instances: 3

  storage:
    size: 5Gi

  bootstrap:
    recovery:
      source: clusterBackup
      recoveryTarget:
        targetTime: "2020-11-26 15:22:00.00000+00"

  externalClusters:
    - name: clusterBackup
      barmanObjectStore:
        destinationPath: https://STORAGEACCOUNTNAME.blob.core.windows.net/CONTAINERNAME/
        azureCredentials:
          storageAccount:
            name: recovery-object-store-secret
            key: storage_account_name
          storageKey:
            name: recovery-object-store-secret
            key: storage_account_key
        wal:
          maxParallel: 8
```

You might have noticed that in the above example you only had to specify
the `targetTime` in the form of a timestamp, without having to worry about
specifying the base backup from which to start the recovery.

The `backupID` option is the one that allows you to specify the base backup
from which to initiate the recovery process. By default, this value is
empty.

If you assign a value to it (in the form of a Barman backup ID), the operator
will use that backup as base for the recovery.

!!! Important
    You need to make sure that such a backup exists and is accessible.

If the backup ID is not specified, the operator will automatically detect the
base backup for the recovery as follows:

- when you use `targetTime` or `targetLSN`, the operator selects the closest
  backup that was completed before that target
- otherwise the operator selects the last available backup in chronological
  order.

Here are the recovery target criteria you can use:

targetTime
:  time stamp up to which recovery will proceed, expressed in
   [RFC 3339](https://datatracker.ietf.org/doc/html/rfc3339) format
   (the precise stopping point is also influenced by the `exclusive` option)

targetXID
:  transaction ID up to which recovery will proceed
   (the precise stopping point is also influenced by the `exclusive` option);
   keep in mind that while transaction IDs are assigned sequentially at
   transaction start, transactions can complete in a different numeric order.
   The transactions that will be recovered are those that committed before
   (and optionally including) the specified one

targetName
:  named restore point (created with `pg_create_restore_point()`) to which
   recovery will proceed

targetLSN
:  LSN of the write-ahead log location up to which recovery will proceed
   (the precise stopping point is also influenced by the `exclusive` option)

targetImmediate
:  recovery should end as soon as a consistent state is reached - i.e. as early
   as possible. When restoring from an online backup, this means the point where
   taking the backup ended


!!! Important
    While the operator is able to automatically retrieve the closest backup
    when either `targetTime` or `targetLSN` is specified, this is not possible
    for the remaining targets: `targetName`, `targetXID`, and `targetImmediate`.
    In such cases, it is important to specify `backupID`, unless you are OK with
    the last available backup in the catalog.

The example below uses a `targetName` based recovery target:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
[...]
  bootstrap:
    recovery:
      source: clusterBackup
      recoveryTarget:
        backupID: 20220616T142236
        targetName: 'restore_point_1'
[...]
```

You can choose only a single one among the targets above in each
`recoveryTarget` configuration.

Additionally, you can specify `targetTLI` force recovery to a specific
timeline.

By default, the previous parameters are considered to be exclusive, stopping
just before the recovery target. You can request inclusive behavior,
stopping right after the recovery target, setting the `exclusive` parameter to
`false` like in the following example relying on a blob container in Azure:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-restore-pitr
spec:
  instances: 3

  storage:
    size: 5Gi

  bootstrap:
    recovery:
      source: clusterBackup
      recoveryTarget:
        backupID: 20220616T142236
        targetName: "maintenance-activity"
        exclusive: false

  externalClusters:
    - name: clusterBackup
      barmanObjectStore:
        destinationPath: https://STORAGEACCOUNTNAME.blob.core.windows.net/CONTAINERNAME/
        azureCredentials:
          storageAccount:
            name: recovery-object-store-secret
            key: storage_account_name
          storageKey:
            name: recovery-object-store-secret
            key: storage_account_key
        wal:
          maxParallel: 8
```

#### Configure the application database

For the recovered cluster, we can configure the application database name and
credentials with additional configuration. To update application database
credentials, we can generate our own passwords, store them as secrets, and
update the database use the secrets. Or we can also let the operator generate a
secret with randomly secure password for use. Please reference the
["Bootstrap an empty cluster"](#bootstrap-an-empty-cluster-initdb)
section for more information about secrets.

The following example configure the application database `app` with owner
`app`, and supplied secret `app-secret`.

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
[...]
spec:
  bootstrap:
    recovery:
      database: app
      owner: app
      secret:
        name: app-secret
      [...]
```

With the above configuration, the following will happen after recovery is completed:

1. if database `app` does not exist, a new database `app` will be created.
2. if user `app` does not exist, a new user `app` will be created.
3. if user `app` is not the owner of database, user `app` will be granted 
as owner of database `app`.
4. If value of `username` match value of `owner` in secret, the password of 
application database will be changed to the value of `password` in secret. 

!!! Important
    For a replica cluster with replica mode enabled, the operator will not
    create any database or user in the PostgreSQL instance, as these will be
    recovered from the original cluster.

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
- source must not have any tablespace defined (see ["Current limitations"](#current-limitations) below)
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

The following manifest creates a new PostgreSQL 15.3 cluster,
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
  imageName: ghcr.io/cloudnative-pg/postgresql:15.3

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
the same PostgreSQL version (in our case 15.3).

#### TLS certificate authentication

The second authentication method supported by CloudNativePG
with the `pg_basebackup` bootstrap is based on TLS client certificates.
This is the recommended approach from a security standpoint.

The following example clones an existing PostgreSQL cluster (`cluster-example`)
in the same Kubernetes cluster.

!!! Note
    This example can be easily adapted to cover an instance that resides
    outside the Kubernetes cluster.

The manifest defines a new PostgreSQL 15.3 cluster called `cluster-clone-tls`,
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
  imageName: ghcr.io/cloudnative-pg/postgresql:15.3

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

The following example configure the application database `app` with password in
supplied secret `app-secret` after bootstrap from a live cluster.

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

With the above configuration, the following will happen after recovery is completed:

1. if database `app` does not exist, a new database `app` will be created.
2. if user `app` does not exist, a new user `app` will be created.
3. if user `app` is not the owner of database, user `app` will be granted
   as owner of database `app`.
4. If value of `username` match value of `owner` in secret, the password of
   application database will be changed to the value of `password` in secret.

!!! Important
    For a replica cluster with replica mode enabled, the operator will not
    create any database or user in the PostgreSQL instance, as these will be
    recovered from the original cluster.

#### Current limitations

##### Missing tablespace support

CloudNativePG does not currently include full declarative management
of PostgreSQL global objects, namely roles, databases, and tablespaces.
While roles and databases are copied from the source instance to the target
cluster, tablespaces require a capability that this version of
CloudNativePG is missing: definition and management of additional
persistent volumes. When dealing with base backup and tablespaces, PostgreSQL
itself requires that the exact mount points in the source instance
must also exist in the target instance, in our case, the pods in Kubernetes
that CloudNativePG manages. For this reason, you cannot directly
migrate in CloudNativePG a PostgreSQL instance that takes advantage
of tablespaces (you first need to remove them from the source or, if your
organization requires this feature, contact EDB to prioritize it).

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
    applications in production. Feel free to contact EDB for assistance.
