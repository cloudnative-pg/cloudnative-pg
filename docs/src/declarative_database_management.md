# Declarative Database Management

Declarative database management allows the user to handle databases via a new 
separate CRD named `Database`.

Database CRD is only controlled by the instance manager of primary instance.
This feature works only with clusters that are not replica clusters.

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

Once Database CRD reconciliation succeeded, its status shows `ready` field set to true and `error` field is empty.

Database deletion is triggered by a finalizer which gets added by the reconciliation loop itself. 
By default, the database reclaim policy is set to `retain`, preventing the controller from deleting the database.
When the database reclaim policy is set to `delete` and the Database CRD is deleted, the database gets deleted too.

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
