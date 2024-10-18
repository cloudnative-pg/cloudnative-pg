# Declarative Publication/Subscription Management 

Declarative publication/subscription management enables users to set up
logical replication via new Custom Resource Definitions (CRD)
- `Database` ,
- `Publication`,
- `Subscription`,

Database CRD is widely discussed in
["Declarative database management"](declarative_database_management.md) section.

Logical replication is set up between one source cluster with publication 
and one destination cluster that is subscribed to that publication.

### Example: Simple Publication Declaration

A `Publication` object is managed by the instance manager of the source cluster's
primary instance.
Below is an example of a basic `Publication` configuration:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Publication
metadata:
  name: pub-one
spec:
  name: pub
  dbname: cat
  cluster:
    name: source-cluster
  target:
    allTables: true
```

The `dbname` field specifies the database the publication is applied to.
Once the reconciliation cycle is completed successfully, the `Publication` 
status will show a `ready` field set to `true` and an empty `error` field.

### Publication Deletion and Reclaim Policies

A finalizer named `cnpg.io/deletePublication` is automatically added
to each `Publication` object to control its deletion process.

By default, the `publicationReclaimPolicy` is set to `retain`, which means
that if the `Publication` object is deleted, the actual PostgreSQL publication
is retained for manual management by an administrator.

Alternatively, if the `publicationReclaimPolicy` is set to `delete`,
the PostgreSQL publication will be automatically deleted when the `Publication`
object is removed.

### Example: Publication with Delete Reclaim Policy

The following example illustrates a `Publication` object with a `delete`
reclaim policy:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Publication
metadata:
  name: pub-one
spec:
  name: pub
  dbname: cat
  publicationReclaimPolicy: delete
  cluster:
    name: source-cluster
  target:
    allTables: true
```

In this case, when the `Publication` object is deleted, the corresponding PostgreSQL publication will also be removed automatically.


### Example: Simple Subscription Declaration

A `Subscription` object is managed by the instance manager of the destination cluster's
primary instance.
Below is an example of a basic `Subscription` configuration:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Subscription
metadata:
  name: sub-one
spec:
  name: sub
  dbname: cat
  publicationName: pub
  cluster:
    name: destination-cluster
  externalClusterName: source-cluster
```

The `dbname` field specifies the database the publication is applied to.
The `publicationName` field specifies the name of the publication the subscription refers to.
The `externalClusterName` field specifies the external cluster the publication belongs to.

Once the reconciliation cycle is completed successfully, the `Subscription`
status will show a `ready` field set to `true` and an empty `error` field.

### Subscription Deletion and Reclaim Policies

A finalizer named `cnpg.io/deleteSubscription` is automatically added
to each `Subscription` object to control its deletion process.

By default, the `subscriptionReclaimPolicy` is set to `retain`, which means
that if the `Subscription` object is deleted, the actual PostgreSQL publication
is retained for manual management by an administrator.

Alternatively, if the `subscriptionReclaimPolicy` is set to `delete`,
the PostgreSQL publication will be automatically deleted when the `Publication`
object is removed.

### Example: Subscription with Delete Reclaim Policy

The following example illustrates a `Subscription` object with a `delete`
reclaim policy:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Subscription
metadata:
  name: sub-one
spec:
  name: sub
  dbname: cat
  publicationName: pub
  subscriptionReclaimPolicy: delete
  cluster:
    name: destination-cluster
  externalClusterName: source-cluster
```

In this case, when the `Subscription` object is deleted, the corresponding PostgreSQL publication will also be removed automatically.
