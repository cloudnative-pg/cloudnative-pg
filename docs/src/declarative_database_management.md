# Declarative Database Management

Declarative database management allows the user to handle the lifecycle of
databases via a new separate CRD named `Database`.

A `Database` object is only controlled by the instance manager of its primary instance.
This feature doesn't work with replica clusters.

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

Once the Database reconciliation cycle succeeds, the status of the `Database` will
show a `ready` field set to `true` and an empty `error` field.

Database deletion is controlled by a finalizer named `cnpg.io/deleteDatabase`, which is being added by the
reconciliation loop itself on each `Database` pointing to an existing `Cluster`.
By default, the `databaseReclaimPolicy` is set to `retain`, meaning that the PostgreSQL Database will be retained
for manual reclamation by the administrator.
When the `databaseReclaimPolicy` is set to `delete` and the `Database` object is deleted, the PostgreSQL Database
gets deleted too.

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
