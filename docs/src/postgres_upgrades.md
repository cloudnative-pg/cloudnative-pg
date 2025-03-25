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

CloudNativePG performs an **offline in-place major upgrade** when a new operand
container image with a higher PostgreSQL major version is requested for a
cluster.

You can trigger the upgrade in one of two ways:

- By updating the major version in the image tag via the `.spec.imageName`
  option.
- Using an [image catalog](image_catalog.md) to manage version changes.

For details on supported image tags, see
["Image Tag Requirements"](container_images/#image-tag-requirements).

### Upgrade Process

When CloudNativePG detects a PostgreSQL major version upgrade, it:

1. Shuts down all cluster pods to ensure data consistency.
2. Records the previous PostgreSQL version in the cluster’s status
   (`.status.majorVersionUpgradeFromImage`).
3. Initiates a new upgrade job, which performs the necessary steps to upgrade
   the database via `pg_upgrade` with the `--link` option.

!!! Important
    During the upgrade process, the entire PostgreSQL cluster, including
    replicas, is unavailable to applications. Ensure that your system can
    tolerate this downtime before proceeding.

!!! Info
    For detailed guidance on `pg_upgrade`, refer to the official
    [PostgreSQL documentation](https://www.postgresql.org/docs/current/pgupgrade.html).

### Post-Upgrade Actions

If the upgrade is **successful**, CloudNativePG:

- **Destroys the PVCs of replicas** (if available).
- **Scales up replicas as required**.

!!! Warning
    Re-cloning replicas may take significant time for very large databases.
    Ensure you account for this delay. It is strongly recommended to take a **full
    backup** once the upgrade is completed.

If the upgrade **fails**, you must **revert** the major version change in the
cluster's configuration, as CloudNativePG cannot automatically decide the
rollback.

!!! Important
    This process **protects your existing database from data loss**, as no data
    is modified during the upgrade. If the upgrade fails, a rollback is
    possible, without having to perform a full recovery from a backup. Ensure you
    monitor the process closely and take corrective action if needed.

