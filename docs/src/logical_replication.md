# Logical Replication

PostgreSQL extends its replication capabilities beyond physical replication,
which operates at the level of exact block addresses and byte-by-byte copying,
by offering [logical replication](https://www.postgresql.org/docs/current/logical-replication.html).
Logical replication replicates data objects and their changes based on a
defined replication identity, typically the primary key.

Logical replication uses a publish-and-subscribe model, where subscribers
connect to publications on a publisher node. Subscribers pull data changes from
these publications and can re-publish them, enabling cascading replication and
complex topologies.

This flexible model is particularly useful for:

- Online data migrations
- Live PostgreSQL version upgrades
- Data distribution across systems
- Real-time analytics
- Integration with external applications

!!! Info
    For more details and examples, refer to the
    [official PostgreSQL documentation on Logical Replication](https://www.postgresql.org/docs/current/logical-replication.html).

**CloudNativePG** enhances this capability by providing declarative support for
key PostgreSQL logical replication objects:

- **Publications** via the `Publication` resource
- **Subscriptions** via the `Subscription` resource

## Publications

In PostgreSQL's publish-and-subscribe replication model, a
[**publication**](https://www.postgresql.org/docs/current/logical-replication-publication.html)
is the source of data changes. It acts as a logical container for the change
sets (also known as *replication sets*) generated from one or more tables within
a database. Publications can be defined on any PostgreSQL 10+ instance acting
as the *publisher*, including instances managed by popular DBaaS solutions in the
public cloud. Each publication is tied to a single database and provides
fine-grained control over which tables and changes are replicated.

For publishers outside Kubernetes, you can [create publications using SQL](https://www.postgresql.org/docs/current/sql-createpublication.html)
or leverage the [`cnpg publication create` plugin command](kubectl-plugin.md#logical-replication-publications).

When managing `Cluster` objects with **CloudNativePG**, PostgreSQL publications
can be defined declaratively through the `Publication` resource.

!!! Info
    Please refer to the [API reference](cloudnative-pg.v1.md#postgresql-cnpg-io-v1-Publication)
    the full list of attributes you can define for each `Publication` object.

Suppose you have a cluster named `freddie` and want to replicate all tables in
the `app` database. Here's a `Publication` manifest:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Publication
metadata:
  name: freddie-pub
spec:
  cluster:
    name: freddie
  dbname: app
  name: publisher
  target:
    allTables: true
```

In the above example:

- The publication is named `publisher` (`spec.name`).
- It includes all tables (`spec.target.allTables: true`) from the `app`
  database (`spec.dbname`).
- The publication is created via the primary of the `freddie` cluster
  (`spec.cluster.name`).

!!! Important
    While `allTables` simplifies configuration, PostgreSQL offers fine-grained
    control for replicating specific tables or targeted data changes. For advanced
    configurations, consult the [PostgreSQL documentation](https://www.postgresql.org/docs/current/logical-replication.html).
    Additionally, refer to the [CloudNativePG API reference](cloudnative-pg.v1.md#postgresql-cnpg-io-v1-PublicationTarget)
    for details on declaratively customizing replication targets.

#### Required Fields in the `Publication` Manifest

The following fields are required for a `Publication` object:

- `metadata.name`: Unique name for the Kubernetes `Publication` object.
- `spec.cluster.name`: Name of the PostgreSQL cluster.
- `spec.dbname`: Database name where the publication is created.
- `spec.name`: Publication name in PostgreSQL.
- `spec.target`: Specifies the tables or changes to include in the publication.

The `Publication` object must reference a specific `Cluster`, determining where
the publication will be created. It is managed by the cluster's primary instance,
ensuring the publication is created or updated as needed.

#### Reconciliation and Status

After creating a `Publication`, CloudNativePG manages it on the primary
instance of the specified cluster. Following a successful reconciliation cycle,
the `Publication` status will reflect the following:

- `applied: true`, indicates the configuration has been successfully applied.
- `observedGeneration` matches `metadata.generation`, confirming the applied
  configuration corresponds to the most recent changes.

If an error occurs during reconciliation, `status.applied` will be `false`, and
an error message will be included in the `status.message` field.

#### Removing a publication

The `publicationReclaimPolicy` field controls the behavior when deleting a
`Publication` object:

- `retain` (default): Leaves the publication in PostgreSQL for manual
  management.
- `delete`: Automatically removes the publication from PostgreSQL.

Consider the following example:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Publication
metadata:
  name: freddie-pub
spec:
  cluster:
    name: freddie
  dbname: app
  name: publisher
  target:
    allTables: true
  publicationReclaimPolicy: delete
```

In this case, deleting the `Publication` object also removes the `publisher`
publication from the `app` database of the `freddie` cluster.

## Subscriptions

TODO

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
