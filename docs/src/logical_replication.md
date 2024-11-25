# Logical Replication

PostgreSQL extends its replication capabilities beyond physical replication,
which works at the level of exact block addresses and byte-by-byte copying, by
also offering [logical replication](https://www.postgresql.org/docs/current/logical-replication.html).
Logical replication enables data objects and their changes to be replicated
based on a defined replication identity, typically the primary key.

Logical replication uses a publish-and-subscribe model, where one or more
subscribers connect to one or more publications on a publisher node.
Subscribers pull data changes from the publications they subscribe to and can
re-publish this data, enabling cascading replication or more complex
replication topologies.

This flexible approach is particularly suited for use cases such as:

- Online data migrations
- Live PostgreSQL version upgrades
- Data distribution across multiple systems
- Real-time analytics
- Seamless integration with external applications

!!! Info
    For detailed information and examples, see the official PostgreSQL
    documentation on [Logical Replication](https://www.postgresql.org/docs/current/logical-replication.html).

**CloudNativePG** further enhances this feature by providing declarative
support for the core PostgreSQL objects that manage logical replication:

- **Publications** via the `Publication` resource
- **Subscriptions** via the `Subscription` resource


## Overview

The procedure to set up logical replication:

- Begins with two CloudNativePG clusters.
    - One of them will be the "source"
    - The "destination" cluster should have an `externalClusters` stanza
      containing the connection information to the source cluster
- A Database object creating a database (e.g. named `sample`) in the source
  cluster
- A Database object creating a database with the same name in the destination
  cluster
- A Publication in the source cluster referencing the database
- A Subscription in the destination cluster, referencing the Publication that
  was created in the previous step

Once these objects are reconciled, PostgreSQL will replicate the data from
the source cluster to the destination cluster using logical replication. There
are many use cases for logical replication; please refer to the
[PostgreSQL documentation](https://www.postgresql.org/docs/current/logical-replication.html)
for detailed discussion.

!!! Note
    the `externalClusters` section in the destination cluster has the same
    structure used in [database import](database_import.md) as well as for
    replica clusters. However, the destination cluster does not necessarily
    have to be bootstrapped via replication nor import.

### Example: Simple Publication Declaration

A `Publication` object is managed by the instance manager of the source
cluster's primary instance.
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
status will show a `ready` field set to `true`, and an empty `error` field.

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

A `Subscription` object is managed by the instance manager of the destination
cluster's primary instance.
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

## Subscription Deletion and Reclaim Policies

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
