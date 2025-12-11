---
id: declarative_database_management
sidebar_position: 240
title: PostgreSQL Database management
---

# PostgreSQL Database management
<!-- SPDX-License-Identifier: CC-BY-4.0 -->

CloudNativePG simplifies PostgreSQL database provisioning by automatically
creating an application database named `app` by default. This default behavior
is explained in the ["Bootstrap an Empty Cluster"](bootstrap.md#bootstrap-an-empty-cluster-initdb)
section.

For more advanced use cases, CloudNativePG introduces **declarative database
management**, which empowers users to define and control the lifecycle of
PostgreSQL databases using the `Database` Custom Resource Definition (CRD).
This method seamlessly integrates with Kubernetes, providing a scalable,
automated, and consistent approach to managing PostgreSQL databases.

---

## Key Concepts

### Scope of Management

:::info[Important]
    CloudNativePG manages **global objects** in PostgreSQL clusters, including
    databases, roles, and tablespaces. However, it does **not** manage database content
    beyond extensions and schemas (e.g., tables). To manage database content, use specialized
    tools or rely on the applications themselves.
:::

### Declarative `Database` Manifest

The following example demonstrates how a `Database` resource interacts with a
`Cluster`:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Database
metadata:
  name: cluster-example-one
spec:
  name: one
  owner: app
  cluster:
    name: cluster-example
  extensions:
  - name: bloom
    ensure: present
```

When applied, this manifest creates a `Database` object called
`cluster-example-one` requesting a database named `one`, owned by the `app`
role, in the `cluster-example` PostgreSQL cluster.

:::info
    Please refer to the [API reference](cloudnative-pg.v1.md#databasespec)
    the full list of attributes you can define for each `Database` object.
:::

### Required Fields in the `Database` Manifest

- `metadata.name`: Unique name of the Kubernetes object within its namespace.
- `spec.name`: Name of the database as it will appear in PostgreSQL.
- `spec.owner`: PostgreSQL role that owns the database.
- `spec.cluster.name`: Name of the target PostgreSQL cluster.

The `Database` object must reference a specific `Cluster`, determining where
the database will be created. It is managed by the cluster's primary instance,
ensuring the database is created or updated as needed.

:::info
    The distinction between `metadata.name` and `spec.name` allows multiple
    `Database` resources to reference databases with the same name across different
    CloudNativePG clusters in the same Kubernetes namespace.
:::

## Reserved Database Names

PostgreSQL automatically creates databases such as `postgres`, `template0`, and
`template1`. These names are reserved and cannot be used for new `Database`
objects in CloudNativePG.

:::info[Important]
    Creating a `Database` with `spec.name` set to `postgres`, `template0`, or
    `template1` is not allowed.
:::

## Reconciliation and Status

Once a `Database` object is reconciled successfully:

- `status.applied` will be set to `true`.
- `status.observedGeneration` will match the `metadata.generation` of the last
  applied configuration.

Example of a reconciled `Database` object:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Database
metadata:
  generation: 1
  name: cluster-example-one
spec:
  cluster:
    name: cluster-example
  name: one
  owner: app
status:
  observedGeneration: 1
  applied: true
```

If an error occurs during reconciliation, `status.applied` will be `false`, and
an error message will be included in the `status.message` field.

## Deleting a Database

CloudNativePG supports two methods for database deletion:

1. Using the `delete` reclaim policy
2. Declaratively setting the database's `ensure` field to `absent`

### Deleting via `delete` Reclaim Policy

The `databaseReclaimPolicy` field determines the behavior when a `Database`
object is deleted:

- `retain` (default): The database remains in PostgreSQL for manual management.
- `delete`: The database is automatically removed from PostgreSQL.

Example:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Database
metadata:
  name: cluster-example-two
spec:
  databaseReclaimPolicy: delete
  name: two
  owner: app
  cluster:
    name: cluster-example
```

Deleting this `Database` object will automatically remove the `two` database
from the `cluster-example` cluster.

### Declaratively Setting `ensure: absent`

To remove a database, set the `ensure` field to `absent` like in the following
example:.

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Database
metadata:
  name: cluster-example-database-to-drop
spec:
  cluster:
    name: cluster-example
  name: database-to-drop
  owner: app
  ensure: absent
```

This manifest ensures that the `database-to-drop` database is removed from the
`cluster-example` cluster.

## Managing Extensions in a Database

:::info
    While extensions are database-scoped rather than global objects,
    CloudNativePG provides a declarative interface for managing them. This approach
    is necessary because installing certain extensions may require superuser
    privileges, which CloudNativePG recommends disabling by default. By leveraging
    this API, users can efficiently manage extensions in a scalable and controlled
    manner without requiring elevated privileges.
:::

CloudNativePG simplifies and automates the management of PostgreSQL extensions within the
target database.

To enable this feature, define the `spec.extensions` field
with a list of extension specifications, as shown in the following example:

```yaml
# ...
spec:
  extensions:
  - name: bloom
    ensure: present
# ...
```

Each extension entry supports the following properties:

- `name` *(mandatory)*: The name of the extension.
- `ensure`: Specifies whether the extension should be present or absent in the
  database:
    - `present`: Ensures that the extension is installed (default).
    - `absent`: Ensures that the extension is removed.
- `version`: The specific version of the extension to install or
  upgrade to.
- `schema`: The schema in which the extension should be installed.

:::info
    CloudNativePG manages extensions using the following PostgreSQL’s SQL commands:
    [`CREATE EXTENSION`](https://www.postgresql.org/docs/current/sql-createextension.html),
    [`DROP EXTENSION`](https://www.postgresql.org/docs/current/sql-dropextension.html),
    [`ALTER EXTENSION`](https://www.postgresql.org/docs/current/sql-alterextension.html)
    (limited to `UPDATE TO` and `SET SCHEMA`).
:::

The operator reconciles only the extensions explicitly listed in
`spec.extensions`. Any existing extensions not specified in this list remain
unchanged.

:::warning
    Before the introduction of declarative extension management, CloudNativePG
    did not offer a straightforward way to create extensions through configuration.
    To address this, the ["managed extensions"](postgresql_conf.md#managed-extensions)
    feature was introduced, enabling the automated and transparent management
    of key extensions like `pg_stat_statements`. Currently, it is your
    responsibility to ensure there are no conflicts between extension support in
    the `Database` CRD and the managed extensions feature.
:::

## Managing Schemas in a Database

:::info
    Schema management in PostgreSQL is an exception to CloudNativePG's primary
    focus on managing global objects. Since schemas exist within a database, they
    are typically managed as part of the application development process. However,
    CloudNativePG provides a declarative interface for schema management, primarily
    to complete the support of extensions deployment within schemas.
:::

CloudNativePG simplifies and automates the management of PostgreSQL schemas within the
target database.

To enable this feature, define the `spec.schemas` field
with a list of schema specifications, as shown in the following example:

```yaml
# ...
spec:
  schemas:
  - name: app
    owner: app
# ...
```

Each schema entry supports the following properties:

- `name` *(mandatory)*: The name of the schema.
- `owner`: The owner of the schema.
- `ensure`: Specifies whether the schema should be present or absent in the
  database:
    - `present`: Ensures that the schema is installed (default).
    - `absent`: Ensures that the schema is removed.

:::info
    CloudNativePG manages schemas using the following PostgreSQL’s SQL commands:
    [`CREATE SCHEMA`](https://www.postgresql.org/docs/current/sql-createschema.html),
    [`DROP SCHEMA`](https://www.postgresql.org/docs/current/sql-dropschema.html),
    [`ALTER SCHEMA`](https://www.postgresql.org/docs/current/sql-alterschema.html).
:::

## Limitations and Caveats

### Renaming a database

While CloudNativePG adheres to PostgreSQL’s
[CREATE DATABASE](https://www.postgresql.org/docs/current/sql-createdatabase.html) and
[ALTER DATABASE](https://www.postgresql.org/docs/current/sql-alterdatabase.html)
commands, **renaming databases is not supported**.
Attempting to modify `spec.name` in an existing `Database` object will result
in rejection by Kubernetes.

### Creating vs. Altering a Database

- For new databases, CloudNativePG uses the `CREATE DATABASE` statement.
- For existing databases, `ALTER DATABASE` is used to apply changes.

It is important to note that there are some differences between these two
Postgres commands: in particular, the options accepted by `ALTER` are a subset
of those accepted by `CREATE`.

:::warning
    Some fields, such as encoding and collation settings, are immutable in
    PostgreSQL. Attempts to modify these fields on existing databases will be
    ignored.
:::

### Replica Clusters

Database objects declared on replica clusters cannot be enforced, as replicas
lack write privileges. These objects will remain in a pending state until the
replica is promoted.

### Conflict Resolution

If two `Database` objects in the same namespace manage the same PostgreSQL
database (i.e., identical `spec.name` and `spec.cluster.name`), the second
object will be rejected.

Example status message:

```yaml
status:
  applied: false
  message: 'reconciliation error: database "one" is already managed by Database object "cluster-example-one"'
```

### Postgres Version Differences

CloudNativePG adheres to PostgreSQL's capabilities. For example, features like
`ICU_RULES` introduced in PostgreSQL 16 are unavailable in earlier versions.
Errors from PostgreSQL will be reflected in the `Database` object's `status`.

### Manual Changes

CloudNativePG does not overwrite manual changes to databases. Once reconciled,
a `Database` object will not be reapplied unless its `metadata.generation`
changes, giving flexibility for direct PostgreSQL modifications.
