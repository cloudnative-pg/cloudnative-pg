# Declarative Database Management

Declarative database management enables users to control the lifecycle of
databases via a new Custom Resource Definition (CRD) called `Database`.

A `Database` object is managed by the instance manager of the cluster's
primary instance. This feature is not supported in replica clusters,
as replica clusters lack a primary instance to manage the `Database` object.

### Example: Simple Database Declaration

Below is an example of a basic `Database` configuration:

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

Once the reconciliation cycle is completed successfully, the `Database` 
status will show a `ready` field set to `true` and an empty `error` field.

### Database Deletion and Reclaim Policies

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
