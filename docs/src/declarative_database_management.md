# Declarative Database Management

Declarative database management allows the user to handle the lifecycle of
databases via a new CRD named `Database`.

A `Database` object is only controlled by the instance manager of the cluster's
primary instance.
This feature doesn't work with replica clusters.

This is an example of a simple Database declaration:

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

Once the Database reconciliation cycle succeeds, the status of the `Database`
will show a `ready` field set to `true` and an empty `error` field.

Database deletion is controlled by a finalizer named `cnpg.io/deleteDatabase`,
which is added by the controller on each `Database`.
By default, the `databaseReclaimPolicy` is set to `retain`, meaning that if the
Database object is deleted, the PostgreSQL Database will be retained
for manual handling by the administrator.
When the `databaseReclaimPolicy` is set to `delete` and the `Database` object
is deleted, the PostgreSQL Database will be deleted too.
The following example shows this:

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
