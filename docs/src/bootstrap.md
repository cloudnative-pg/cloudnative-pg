# Bootstrap

This section describes the options you have to create a new
PostgreSQL cluster and the design rationale behind them.

When a PostgreSQL cluster is defined, you can configure the
*bootstrap* method using the `bootstrap` section of the cluster
specification.

In the following example:

```yaml
apiVersion: postgresql.k8s.enterprisedb.io/v1alpha1
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

- `initdb`: initialise an empty PostgreSQL cluster
- `fullRecovery`: create a PostgreSQL cluster restoring from an existing backup
   and replaying all the available WAL files.

## initdb

The `initdb` bootstrap method is used to create a new PostgreSQL cluster from
scratch. It is the default one, unless specified differently.

The following example contains the full structure of the `initdb` configuration:

```yaml
apiVersion: postgresql.k8s.enterprisedb.io/v1alpha1
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

Alternatively, you can generate your own passwords, store them as secrets,
and use them in the PostgreSQL cluster - as described in the above example.

The supplied secrets must comply with the specifications of the
[`kubernetes.io/basic-auth` type](https://kubernetes.io/docs/concepts/configuration/secret/#basic-authentication-secret).
The operator will only use the `password` field of the secret,
ignoring the `username`one. If you plan to reuse the secret for application
connections, you can set the `username` field to the same value of the `owner`.

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
    For now changes to the name of the superuser secret are not applied
    to the cluster.

The actual PostgreSQL data directory is created via an invocation of the
`initdb` PostgreSQL command. If you need to add custom options to that
command (i.e. to change the locale used for the template databases or to
add data checksums), you can add them to the `options` section like in
the following example:

```
apiVersion: postgresql.k8s.enterprisedb.io/v1alpha1
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

## fullRecovery

The `fullRecovery` bootstrap mode lets you create a new cluster from
an existing backup. More information about the recovery feature
can be found in the ["Backup and recovery" page](backup_recovery.md).

The following example contains the full structure of the `fullRecovery`
section:

```yaml
apiVersion: postgresql.k8s.enterprisedb.io/v1alpha1
kind: Cluster
metadata:
  name: cluster-example-initdb
spec:
  instances: 3

  superuserSecret:
    name: superuser-secret

  bootstrap:
    fullRecovery:
      backup:
        name: backup-example

  storage:
    size: 1Gi
```

This bootstrap methods allows you to specify just a reference to the
backup that needs to be restored.

The application database name and the application database user are preserved
from the backup that is being restored. The operator does not currently attempt
to backup the underlying secrets, as this is part of the usual maintenance
activity of the Kubernetes cluster itself.

In case you don't supply any `superuserSecret`, a new one is automatically
generated with a secure and random password. The secret is then used to
reset the password for the `postgres` user of the cluster.
