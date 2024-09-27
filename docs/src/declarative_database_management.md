# Declarative Database Management

Declarative database management enables users to control the lifecycle of
databases via the `Database` Custom Resource Definition (CRD).

## Example: Simple Database Declaration

Below is an example of a basic `Database` manifest:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Database
metadata:
  name: db-one
spec:
  name: one
  owner: app
  cluster:
    name: cluster-example
```

Note that the `Database` references a Cluster, on which the database will be
created.
The `Database` object is managed by the instance manager of the cluster's
primary instance. Declarative database management is not supported in replica
clusters, as replica clusters lack a primary instance to manage the `Database`
object.

In the CRD, there is the `metadata.name` representing the name seen by
Kubernetes, which is guaranteed to be unique per namespace.
There is also the `spec.name` which is the name that will be used in Postgres
for the database. This name, therefore, must be a valid Postgres identifier.

!!! Note
    Having separate `metadata.name` and `spec.name` makes it possible to have
    two different CloudNativePG clusters in the same namespace, with Databases
    that have the same name in Postgres.

Once the instance manager has completed the reconciliation of a Database,
its status will be updated with a `ready` field set to `true`, and an
`observedGeneration` field which keeps track of the last applied `generation`.
If there were errors during the reconciliation of a database, the `ready` field
would show `false`, and an additional field `error` would be displayed in the
status.

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Database
metadata:
  [... snipped ...]
  generation: 1
  name: db-one
spec:
  cluster:
    name: cluster-example
  name: declarative
  owner: app
  template: template0
status:
  observedGeneration: 1
  ready: true
```

!!! Warning
    While declarative database management in CloudNativePG adheres to the
    [PostgreSQL database creation commands](https://www.postgresql.org/docs/current/sql-createdatabase.html),
    it does not support the renaming of databases.

## Database Deletion and Reclaim Policies

A finalizer named `cnpg.io/deleteDatabase` is automatically added
to each `Database` object to control its deletion process.

By default, the `databaseReclaimPolicy` is set to `retain`, which means
that if the `Database` object is deleted, the actual PostgreSQL database
is retained for manual management by an administrator.

Alternatively, if the `databaseReclaimPolicy` is set to `delete`,
the PostgreSQL database will be automatically deleted when the `Database`
object is removed.

### Example: Database with Delete Reclaim Policy

The following example illustrates a `Database` object with a `delete`
reclaim policy:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Database
metadata:
  name: db-one-with-delete-reclaim-policy
spec:
  databaseReclaimPolicy: delete
  name: two
  owner: app
  cluster:
    name: cluster-example
```

In this case, when the `Database` object is deleted, the corresponding PostgreSQL database will also be removed automatically.
