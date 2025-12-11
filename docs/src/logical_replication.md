---
id: logical_replication
sidebar_position: 170
title: Logical Replication
---

# Logical Replication
<!-- SPDX-License-Identifier: CC-BY-4.0 -->

PostgreSQL extends its replication capabilities beyond physical replication,
which operates at the level of exact block addresses and byte-by-byte copying,
by offering [logical replication](https://www.postgresql.org/docs/current/logical-replication.html).
Logical replication replicates data objects and their changes based on a
defined replication identity, typically the primary key.

Logical replication uses a publish-and-subscribe model, where subscribers
connect to publications on a publisher node. Subscribers pull data changes from
these publications and can re-publish them, enabling cascading replication and
complex topologies.

:::info[Important]
    To protect your logical replication subscribers after a failover of the
    publisher cluster in CloudNativePG, ensure that replication slot
    synchronization for logical decoding is enabled. Without this, your logical
    replication clients may lose data and fail to continue seamlessly after a
    failover. For configuration details, see
    ["Replication: Logical Decoding Slot Synchronization"](replication.md#logical-decoding-slot-synchronization).
:::

This flexible model is particularly useful for:

- Online data migrations
- Live PostgreSQL version upgrades
- Data distribution across systems
- Real-time analytics
- Integration with external applications

:::info
    For more details, examples, and limitations, please refer to the
    [official PostgreSQL documentation on Logical Replication](https://www.postgresql.org/docs/current/logical-replication.html).
:::

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

:::info
    Please refer to the [API reference](cloudnative-pg.v1.md#publication)
    for the full list of attributes you can define for each `Publication` object.
:::

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

### Fine-grained control over publication tables

While the `allTables` option provides a convenient way to replicate all tables
in a database, PostgreSQL version 15 and later introduce enhanced flexibility
through the [`CREATE PUBLICATION`](https://www.postgresql.org/docs/current/sql-createpublication.html)
command. This allows you to precisely define which tables, or even which types
of data changes, should be included in a publication.

:::info[Important]
    If you are using PostgreSQL versions earlier than 15, review the syntax and
    options available for `CREATE PUBLICATION` in your specific release. Some
    parameters and features may not be supported.
:::

For complex or tailored replication setups, refer to the
[PostgreSQL logical replication documentation](https://www.postgresql.org/docs/current/logical-replication.html).

Additionally, refer to the [CloudNativePG API reference](cloudnative-pg.v1.md#publicationtarget)
for details on declaratively customizing replication targets.

The following example defines a publication that replicates all tables in the
`portal` schema of the `app` database, along with the `users` table from the
`access` schema:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Publication
metadata:
  name: publisher
spec:
  cluster:
    name: freddie
  dbname: app
  name: publisher
  target:
    objects:
      - tablesInSchema: portal
      - table:
          name: users
          schema: access
```

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

:::info[Important]
    Since schema definitions are not replicated, the subscriber must have the
    corresponding tables already defined before data replication begins.
:::

CloudNativePG simplifies subscription management by enabling you to define them
declaratively using the `Subscription` resource.

:::info
    Please refer to the [API reference](cloudnative-pg.v1.md#subscription)
    for the full list of attributes you can define for each `Subscription` object.
:::

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

:::info
    For more details on configuring the `externalClusters` section, see the
    ["Bootstrap" section](bootstrap.md#the-externalclusters-section) of the
    documentation.
:::

As you can see, a subscription can connect to any PostgreSQL database
accessible over the network. This flexibility allows you to seamlessly migrate
your data into Kubernetes with nearly zero downtime. It’s an excellent option
for transitioning from various environments, including popular cloud-based
Database-as-a-Service (DBaaS) platforms.

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

## Limitations

Logical replication in PostgreSQL has some inherent limitations, as outlined in
the [official documentation](https://www.postgresql.org/docs/current/logical-replication-restrictions.html).
Notably, the following objects are not replicated:

- **Database schema and DDL commands**
- **Sequence data**
- **Large objects**

### Addressing Schema Replication

The first limitation, related to schema replication, can be easily addressed
using CloudNativePG's capabilities. For instance, you can leverage the `import`
bootstrap feature to copy the schema of the tables you need to replicate.
Alternatively, you can manually create the schema as you would for any
PostgreSQL database.

### Handling Sequences

While sequences are not automatically kept in sync through logical replication,
CloudNativePG provides a solution to be used in live migrations.
You can use the [`cnpg` plugin](kubectl-plugin.md#synchronizing-sequences)
to synchronize sequence values, ensuring consistency between the publisher and
subscriber databases.

## Example of live migration and major Postgres upgrade with logical replication

To highlight the powerful capabilities of logical replication, this example
demonstrates how to replicate data from a publisher database (`freddie`)
running PostgreSQL 16 to a subscriber database (`king`) running the latest
PostgreSQL version. This setup can be deployed in your Kubernetes cluster for
evaluation and hands-on learning.

This example illustrates how logical replication facilitates live migrations
and upgrades between PostgreSQL versions while ensuring data consistency. By
combining logical replication with CloudNativePG, you can easily set up,
manage, and evaluate such scenarios in a Kubernetes environment.

### Step 1: Setting Up the Publisher (`freddie`)

The first step involves creating a `freddie` PostgreSQL cluster with version 16.
The cluster contains a single instance and includes an `app` database
initialized with a table, `n`, storing 10,000 numbers. A logical replication
publication named `publisher` is also configured to include all tables in the
database.

Here’s the manifest for setting up the `freddie` cluster and its publication
resource:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: freddie
spec:
  instances: 1

  imageName: ghcr.io/cloudnative-pg/postgresql:16-standard-trixie

  storage:
    size: 1Gi

  bootstrap:
    initdb:
      postInitApplicationSQL:
        - CREATE TABLE n (i SERIAL PRIMARY KEY, m INTEGER)
        - INSERT INTO n (m) (SELECT generate_series(1, 10000))
        - ALTER TABLE n OWNER TO app

  managed:
    roles:
      - name: app
        login: true
        replication: true
---
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

### Step 2: Setting Up the Subscriber (`king`)

Next, create the `king` PostgreSQL cluster, running the latest version of
PostgreSQL. This cluster initializes by importing the schema from the `app`
database on the `freddie` cluster using the external cluster configuration. A
`Subscription` resource, `freddie-to-king-subscription`, is then configured to
consume changes published by the `publisher` on `freddie`.

Below is the manifest for setting up the `king` cluster and its subscription:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: king
spec:
  instances: 1

  imageName: ghcr.io/cloudnative-pg/postgresql:18-standard-trixie

  storage:
    size: 1Gi

  bootstrap:
    initdb:
      import:
        type: microservice
        schemaOnly: true
        databases:
          - app
        source:
          externalCluster: freddie

  externalClusters:
  - name: freddie
    connectionParameters:
      host: freddie-rw.default.svc
      user: app
      dbname: app
    password:
      name: freddie-app
      key: password
---
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

Once the `king` cluster is running, you can verify that the replication is
working by connecting to the `app` database and counting the records in the `n`
table. The following example uses the `psql` command provided by the `cnpg`
plugin for simplicity:

```console
kubectl cnpg psql king -- app -qAt -c 'SELECT count(*) FROM n'
10000
```

This command should return `10000`, confirming that the data from the `freddie`
cluster has been successfully replicated to the `king` cluster.

Using the `cnpg` plugin, you can also synchronize existing sequences to ensure
consistency between the publisher and subscriber. The example below
demonstrates how to synchronize a sequence for the `king` cluster:

```console
kubectl cnpg subscription sync-sequences king --subscription=subscriber
SELECT setval('"public"."n_i_seq"', 10000);

10000
```

This command updates the sequence `n_i_seq` in the `king` cluster to match the
current value, ensuring it is in sync with the source database.
