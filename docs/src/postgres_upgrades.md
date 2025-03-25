<!-- SPDX-License-Identifier: CC-BY-4.0 -->
# PostgreSQL Upgrades

PostgreSQL upgrades fall into two categories:

- **Minor version upgrades** (e.g., from 17.0 to 17.1)
- **Major version upgrades** (e.g., from 16.x to 17.0)

## Minor Version Upgrades

PostgreSQL version numbers follow a `major.minor` format. For instance, in
version **17.1**:

- `17` is the **major version**
- `1` is the **minor version**

Minor releases are fully compatible with earlier and later minor releases of
the same major version. They include bug fixes and security updates but do not
introduce changes to the internal storage format.
For example, **PostgreSQL 17.1** is compatible with **17.0** and **17.4**.

### Upgrading a Minor Version in CloudNativePG

To upgrade to a newer minor version, simply update the PostgreSQL container
image reference in your cluster definition, either directly or via image catalogs.
CloudNativePG will trigger a [rolling update of the cluster](rolling_update.md),
replacing each instance one by one, starting with the replicas. Once all
replicas have been updated, it will perform either a switchover or a restart of
the primary to complete the process.

## Major Version Upgrades

Major PostgreSQL releases introduce changes to the internal data storage
format, requiring a more structured upgrade process.

CloudNativePG supports three methods for performing major upgrades:

1. [Logical dump/restore](database_import.md) – Blue/green deployment, offline.
2. [Native logical replication](logical_replication.md#example-of-live-migration-and-major-postgres-upgrade-with-logical-replication) – Blue/green deployment, online.
3. Physical with `pg_upgrade` – In-place upgrade, offline (covered in the
   ["Offline In-Place Major Upgrades" section](#offline-in-place-major-upgrades) below).

Each method has trade-offs in terms of downtime, complexity, and data volume
handling. The best approach depends on your upgrade strategy and operational
constraints.

!!! Important
    We strongly recommend testing all methods in a controlled environment
    before proceeding with a production upgrade.

## Offline In-Place Major Upgrades

TODO
