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
  name: freddie-publisher
spec:
  cluster:
    name: freddie
  dbname: app
  name: publisher
  target:
    allTables: true
```

In the above example:

- The publication object is named `freddie-publisher` (`metadata.name`).
- The publication is created via the primary of the `freddie` cluster
  (`spec.cluster.name`) with name `publisher` (`spec.name`).
- It includes all tables (`spec.target.allTables: true`) from the `app`
  database (`spec.dbname`).

!!! Important
    While `allTables` simplifies configuration, PostgreSQL offers fine-grained
    control for replicating specific tables or targeted data changes. For advanced
    configurations, consult the [PostgreSQL documentation](https://www.postgresql.org/docs/current/logical-replication.html).
    Additionally, refer to the [CloudNativePG API reference](cloudnative-pg.v1.md#postgresql-cnpg-io-v1-PublicationTarget)
    for details on declaratively customizing replication targets.

### Required Fields in the `Publication` Manifest

The following fields are required for a `Publication` object:

- `metadata.name`: Unique name for the Kubernetes `Publication` object.
- `spec.cluster.name`: Name of the PostgreSQL cluster.
- `spec.dbname`: Database name where the publication is created.
- `spec.name`: Publication name in PostgreSQL.
- `spec.target`: Specifies the tables or changes to include in the publication.

The `Publication` object must reference a specific `Cluster`, determining where
the publication will be created. It is managed by the cluster's primary instance,
ensuring the publication is created or updated as needed.

### Reconciliation and Status

After creating a `Publication`, CloudNativePG manages it on the primary
instance of the specified cluster. Following a successful reconciliation cycle,
the `Publication` status will reflect the following:

- `applied: true`, indicates the configuration has been successfully applied.
- `observedGeneration` matches `metadata.generation`, confirming the applied
  configuration corresponds to the most recent changes.

If an error occurs during reconciliation, `status.applied` will be `false`, and
an error message will be included in the `status.message` field.

### Removing a publication

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
  name: freddie-publisher
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

In PostgreSQL's publish-and-subscribe replication model, a
[**subscription**](https://www.postgresql.org/docs/current/logical-replication-subscription.html)
represents the downstream component that consumes data changes.
A subscription establishes the connection to a publisher's database and
specifies the set of publications (one or more) it subscribes to. Subscriptions
can be created on any supported PostgreSQL instance acting as the *subscriber*.

!!! Important
    Since schema definitions are not replicated, the subscriber must have the
    corresponding tables already defined before data replication begins.

CloudNativePG simplifies subscription management by enabling you to define them
declaratively using the `Subscription` resource.

!!! Info
    Please refer to the [API reference](cloudnative-pg.v1.md#postgresql-cnpg-io-v1-Subscription)
    the full list of attributes you can define for each `Subscription` object.

Suppose you want to replicate changes from the `publisher` publication on the
`app` database of the `freddie` cluster (*publisher*) to the `app` database of
the `king` cluster (*subscriber*). Here's an example of a `Subscription`
manifest:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Subscription
metadata:
  name: freddie-to-king-subscription
spec:
  cluster:
    name: king
  dbname: app
  name: subscriber
  externalClusterName: freddie
  publicationName: publisher
```

In the above example:

- The subscription object is named `freddie-to-king-subscriber` (`metadata.name`).
- The subscription is created in the `app` database (`spec.dbname`) of the
  `king` cluster (`spec.cluster.name`), with name `subscriber` (`spec.name`).
- It connects to the `publisher` publication in the external `freddie` cluster,
  referenced by `spec.externalClusterName`.

To facilitate this setup, the `freddie` external cluster must be defined in the
`king` cluster's configuration. Below is an example excerpt showing how to
define the external cluster in the `king` manifest:

```yaml
externalClusters:
  - name: freddie
    connectionParameters:
      host: freddie-rw.default.svc
      user: postgres
      dbname: app
```

!!! Info
    For more details on configuring the `externalClusters` section, see the
    ["Bootstrap" section](bootstrap.md#the-externalclusters-section) of the
    documentation.

### Required Fields in the `Subscription` Manifest

The following fields are mandatory for defining a `Subscription` object:

- `metadata.name`: A unique name for the Kubernetes `Subscription` object
  within its namespace.
- `spec.cluster.name`: The name of the PostgreSQL cluster where the
  subscription will be created.
- `spec.dbname`: The name of the database in which the subscription will be
  created.
- `spec.name`: The name of the subscription as it will appear in PostgreSQL.
- `spec.externalClusterName`: The name of the external cluster, as defined in
  the `spec.cluster.name` cluster's configuration. This references the
  publisher database.
- `spec.publicationName`: The name of the publication in the publisher database
  to which the subscription will connect.

The `Subscription` object must reference a specific `Cluster`, determining
where the subscription will be managed. CloudNativePG ensures that the
subscription is created or updated on the primary instance of the specified
cluster.

### Reconciliation and Status

After creating a `Subscription`, CloudNativePG manages it on the primary
instance of the specified cluster. Following a successful reconciliation cycle,
the `Subscription` status will reflect the following:

- `applied: true`, indicates the configuration has been successfully applied.
- `observedGeneration` matches `metadata.generation`, confirming the applied
  configuration corresponds to the most recent changes.

If an error occurs during reconciliation, `status.applied` will be `false`, and
an error message will be included in the `status.message` field.

### Removing a subscription

The `subscriptionReclaimPolicy` field controls the behavior when deleting a
`Subscription` object:

- `retain` (default): Leaves the subscription in PostgreSQL for manual
  management.
- `delete`: Automatically removes the subscription from PostgreSQL.

Consider the following example:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Subscription
metadata:
  name: freddie-to-king-subscription
spec:
  cluster:
    name: king
  dbname: app
  name: subscriber
  externalClusterName: freddie
  publicationName: publisher
  subscriptionReclaimPolicy: delete
```

In this case, deleting the `Subscription` object also removes the `subscriber`
subscription from the `app` database of the `king` cluster.
