# Bootstrap

This section describes the options you have to create a new
PostgreSQL cluster and the design rationale behind them.

When a PostgreSQL cluster is defined, you can configure the
*bootstrap* method using the `bootstrap` section of the cluster
specification.

In the following example:

```yaml
apiVersion: postgresql.k8s.enterprisedb.io/v1
kind: Cluster
metadata:
  name: cluster-example-initdb
spec:
  instances: 3

  bootstrap:
    initdb:
      database: appdb
      owner: appuser

  storage:
    size: 1Gi
```

The `initdb` bootstrap method is used.

We currently support the following bootstrap methods:

- `initdb`: initialize an empty PostgreSQL cluster
- `recovery`: create a PostgreSQL cluster by restoring from an existing backup
   and replaying all the available WAL files or up to a given point in time
- `pg_basebackup`: create a PostgreSQL cluster by cloning an existing one of the
   same major version using `pg_basebackup` via streaming replication protocol -
   useful if you want to migrate databases to Cloud Native PostgreSQL, even
   from outside Kubernetes.

## initdb

The `initdb` bootstrap method is used to create a new PostgreSQL cluster from
scratch. It is the default one unless specified differently.

The following example contains the full structure of the `initdb` configuration:

```yaml
apiVersion: postgresql.k8s.enterprisedb.io/v1
kind: Cluster
metadata:
  name: cluster-example-initdb
spec:
  instances: 3

  superuserSecret:
    name: superuser-secret

  bootstrap:
    initdb:
      database: appdb
      owner: appuser
      secret:
        name: appuser-secret

  storage:
    size: 1Gi
```

The above example of bootstrap will:

1. create a new `PGDATA` folder using PostgreSQL's native `initdb` command
2. set a *superuser* password from the secret named `superuser-secret`
3. create an *unprivileged* user named `appuser`
4. set the password of the latter using the one in the `appuser-secret` secret
5. create a database called `appdb` owned by the `appuser` user.

Thanks to the *convention over configuration paradigm*, you can let the
operator choose a default database name (`app`) and a default application
user name (same as the database name), as well as randomly generate a
secure password for both the superuser and the application user in
PostgreSQL.

Alternatively, you can generate your passwords, store them as secrets,
and use them in the PostgreSQL cluster - as described in the above example.

The supplied secrets must comply with the specifications of the
[`kubernetes.io/basic-auth` type](https://kubernetes.io/docs/concepts/configuration/secret/#basic-authentication-secret).
The operator will only use the `password` field of the secret,
ignoring the `username` one. If you plan to reuse the secret for application
connections, you can set the `username` field to the same value as the `owner`.

The following is an example of a `basic-auth` secret:

```yaml
apiVersion: v1
data:
  password: cGFzc3dvcmQ=
kind: Secret
metadata:
  name: cluster-example-app-user
type: kubernetes.io/basic-auth
```

The application database is the one that should be used to store application
data. Applications should connect to the cluster with the user that owns
the application database.

!!! Important
    Future implementations of the operator might allow you to create
    additional users in a declarative configuration fashion.

The superuser and the `postgres` database are supposed to be used only
by the operator to configure the cluster.

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
`initdb` PostgreSQL command. If you need to add custom options to that
command (i.e., to change the locale used for the template databases or to
add data checksums), you can add them to the `options` section like in
the following example:

```yaml
apiVersion: postgresql.k8s.enterprisedb.io/v1
kind: Cluster
metadata:
  name: cluster-example-initdb
spec:
  instances: 3

  bootstrap:
    initdb:
      database: appdb
      owner: appuser
      options:
      - "-k"
      - "--locale=en_US"
  storage:
    size: 1Gi
```

The user can also specify a custom list of queries that will be executed
once, just after the database is created and configured. These queries will
be executed as the *superuser*, connected to the `postgres` database:

```yaml
apiVersion: postgresql.k8s.enterprisedb.io/v1
kind: Cluster
metadata:
  name: cluster-example-initdb
spec:
  instances: 3

  bootstrap:
    initdb:
      database: appdb
      owner: appuser
      options:
      - "-k"
      - "--locale=en_US"
      postInitSQL:
        - CREATE ROLE angus
        - CREATE ROLE malcolm
  storage:
    size: 1Gi
```

!!! Warning
    Please use the `postInitSQL` option with extreme care as queries
    are run as a superuser and can disrupt the entire cluster.

## recovery

The `recovery` bootstrap mode lets you create a new cluster from
an existing backup. You can find more information about the recovery
feature in the ["Backup and recovery" page](backup_recovery.md).

The following example contains the full structure of the `recovery`
section:

```yaml
apiVersion: postgresql.k8s.enterprisedb.io/v1
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

The application database name and the application database user are preserved
from the backup that is being restored. The operator does not currently attempt
to backup the underlying secrets, as this is part of the usual maintenance
activity of the Kubernetes cluster itself.

In case you don't supply any `superuserSecret`, a new one is automatically
generated with a secure and random password. The secret is then used to
reset the password for the `postgres` user of the cluster.

By default, the recovery will continue up to the latest
available WAL on the default target timeline (`current` for PostgreSQL up to
11, `latest` for version 12 and above).
You can optionally specify a `recoveryTarget` to perform a point in time
recovery (see the ["Point in time recovery" chapter](#point-in-time-recovery)).

### Point in time recovery

Instead of replaying all the WALs up to the latest one,
we can ask PostgreSQL to stop replaying WALs at any given point in time.
PostgreSQL uses this technique to implement *point-in-time* recovery.
This allows you to restore the database to its state at any time after
the base backup was taken.

The operator will generate the configuration parameters required for this
feature to work if a recovery target is specified like in the following
example:

```yaml
apiVersion: postgresql.k8s.enterprisedb.io/v1
kind: Cluster
metadata:
  name: cluster-restore-pitr
spec:
  instances: 3

  storage:
    size: 5Gi

  bootstrap:
    recovery:
      backup:
        name: backup-example

      recoveryTarget:
        targetTime: "2020-11-26 15:22:00.00000+00"
```

Beside `targetTime`, you can use the following criteria to stop the recovery:

- `targetXID` specify a transaction ID up to which recovery will proceed

- `targetName` specify a restore point (created with `pg_create_restore_point`
  to which recovery will proceed)

- `targetLSN` specify the LSN of the write-ahead log location up to which
  recovery will proceed

- `targetImmediate` specify to stop as soon as a consistent state is
  reached

You can choose only a single one among the targets above in each
`recoveryTarget` configuration.

Additionally, you can specify `targetTLI` force recovery to a specific
timeline.

By default, the previous parameters are considered to be exclusive, stopping
just before the recovery target. You can request inclusive behavior,
stopping right after the recovery target, setting the `exclusive` parameter to
`false` like in the following example:

```yaml
apiVersion: postgresql.k8s.enterprisedb.io/v1
kind: Cluster
metadata:
  name: cluster-restore-pitr
spec:
  instances: 3

  storage:
    size: 5Gi

  bootstrap:
    recovery:
      backup:
        name: backup-example

      recoveryTarget:
        targetName: "maintenance-activity"
        exclusive: false
```

## pg_basebackup

The `pg_basebackup` bootstrap mode lets you create a new cluster (*target*) as
an exact physical copy of an existing and **binary compatible** PostgreSQL
instance (*source*), through a valid *streaming replication* connection.
The source instance can be either a primary or a standby PostgreSQL server.

The primary use case for this method is represented by **migrations** to Cloud Native PostgreSQL,
either from outside Kubernetes or within Kubernetes (e.g., from another operator).

!!! Warning
    The current implementation creates a *snapshot* of the origin PostgreSQL
    instance when the cloning process terminates and immediately starts
    the created cluster. See ["Current limitations"](#current-limitations) below for details.

Similar to the case of the `recovery` bootstrap method, once the clone operation
completes, the operator will take ownership of the target cluster, starting from
the first instance. This includes overriding some configuration parameters, as
required by Cloud Native PostgreSQL, resetting the superuser password, creating
the `streaming_replica` user, managing the replicas, and so on. The resulting
cluster will be completely independent of the source instance.

!!! Important
    Configuring the network between the target instance and the source instance
    goes beyond the scope of Cloud Native PostgreSQL documentation, as it depends
    on the actual context and environment.

The streaming replication client on the target instance, which will be
transparently managed by `pg_basebackup`, can authenticate itself on the source
instance in any of the following ways:

1. via [username/password](#usernamepassword-authentication)
2. via [TLS client certificate](#tls-certificate-authentication)

The latter is the recommended one if you connect to a source managed
by Cloud Native PostgreSQL or configured for TLS authentication.
The first option is, however, the most common form of authentication to a
PostgreSQL server in general, and might be the easiest way if the source
instance is on a traditional environment outside Kubernetes.
Both cases are explained below.

### Requirements

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

### About the replication user

As explained in the requirements section, you need to have a user
with either the `SUPERUSER` or, preferably, just the `REPLICATION`
privilege in the source instance.

If the source database is created with Cloud Native PostgreSQL, you
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

### Username/Password authentication

The first authentication method supported by Cloud Native PostgreSQL
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

The following manifest creates a new PostgreSQL 13.4 cluster,
called `target-db`, using the `pg_basebackup` bootstrap method
to clone an external PostgreSQL cluster defined as `source-db`
(in the `externalClusters` array). As you can see, the `source-db`
definition points to the `source-db.foo.com` host and connects as
the `streaming_replica` user, whose password is stored in the
`password` key of the `source-db-replica-user` secret.

```yaml
apiVersion: postgresql.k8s.enterprisedb.io/v1
kind: Cluster
metadata:
  name: target-db
spec:
  instances: 3
  imageName: quay.io/enterprisedb/postgresql:13.4

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
the same PostgreSQL version (in our case 13.4).

### TLS certificate authentication

The second authentication method supported by Cloud Native PostgreSQL
with the `pg_basebackup` bootstrap is based on TLS client certificates.
This is the recommended approach from a security standpoint.

The following example clones an existing PostgreSQL cluster (`cluster-example`)
in the same Kubernetes cluster.

!!! Note
    This example can be easily adapted to cover an instance that resides
    outside the Kubernetes cluster.

The manifest defines a new PostgreSQL 13.4 cluster called `cluster-clone-tls`,
which is bootstrapped using the `pg_basebackup` method from the `cluster-example`
external cluster. The host is identified by the read/write service
in the same cluster, while the `streaming_replica` user is authenticated
thanks to the provided keys, certificate, and certification authority
information (respectively in the `cluster-example-replication` and
`cluster-example-ca` secrets).

```yaml
apiVersion: postgresql.k8s.enterprisedb.io/v1
kind: Cluster
metadata:
  name: cluster-clone-tls
spec:
  instances: 3
  imageName: quay.io/enterprisedb/postgresql:13.4

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

### Current limitations

#### Missing tablespace support

Cloud Native PostgreSQL does not currently include full declarative management
of PostgreSQL global objects, namely roles, databases, and tablespaces.
While roles and databases are copied from the source instance to the target
cluster, tablespaces require a capability that this version of
Cloud Native PostgreSQL is missing: definition and management of additional
persistent volumes. When dealing with base backup and tablespaces, PostgreSQL
itself requires that the exact mount points in the source instance
must also exist in the target instance, in our case, the pods in Kubernetes
that Cloud Native PostgreSQL manages. For this reason, you cannot directly
migrate in Cloud Native PostgreSQL a PostgreSQL instance that takes advantage
of tablespaces (you first need to remove them from the source or, if your
organization requires this feature, contact EDB to prioritize it).

#### Snapshot copy

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

Future versions of Cloud Native PostgreSQL will enable users to control
PostgreSQL's continuous recovery mechanism via Write-Ahead Log (WAL) shipping
by creating a new cluster that is a replica of another PostgreSQL instance.
This will open up two main use cases:

- replication over different Kubernetes clusters in Cloud Native PostgreSQL
- *0 cutover time* migrations to Cloud Native PostgreSQL with the `pg_basebackup`
  bootstrap method
