---
id: postgres_upgrades
sidebar_position: 380
title: PostgreSQL upgrades
---

# PostgreSQL upgrades
<!-- SPDX-License-Identifier: CC-BY-4.0 -->

PostgreSQL upgrades fall into two categories:

- [Minor version upgrades](#minor-version-upgrades) (e.g., from 17.0 to 17.1)
- [Major version upgrades](#major-version-upgrades) (e.g., from 16.x to 17.0)

## Minor Version Upgrades

PostgreSQL version numbers follow a `major.minor` format. For instance, in
version 17.1:

- `17` is the major version
- `1` is the minor version

Minor releases are fully compatible with earlier and later minor releases of
the same major version. They include bug fixes and security updates but do not
introduce changes to the internal storage format.

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

:::info[Important]
    We strongly recommend testing all methods in a controlled environment
    before proceeding with a production upgrade.
:::

## Offline In-Place Major Upgrades

CloudNativePG performs an **offline in-place major upgrade** when a new operand
container image with a higher PostgreSQL major version is declaratively
requested for a cluster.

:::info[Important]
    Major upgrades are only supported between images based on the same
    operating system distribution. For example, if your previous version uses a
    `bullseye` image, you cannot upgrade to a `bookworm` image.
:::

:::warning
    There is a bug in PostgreSQL 17.0 through 17.5 that prevents successful upgrades
    if the `max_slot_wal_keep_size` parameter is set to any value other than `-1`.
    The upgrade process will fail with an error related to replication slot configuration.
    This issue has been [fixed in PostgreSQL 17.6 and 18beta2 or later versions](https://github.com/postgres/postgres/commit/f36e5774).
    If you are using PostgreSQL 17.0 through 17.5, ensure that you upgrade to at least
    PostgreSQL 17.6 before attempting a major upgrade, or make sure to temporarily set
    the `max_slot_wal_keep_size` parameter to `-1` in your cluster configuration.
:::

You can trigger the upgrade in one of two ways:

- By updating the major version in the image tag via the `.spec.imageName`
  option.
- Using an [image catalog](image_catalog.md) to manage version changes.

For details on supported image tags, see
["Image Tag Requirements"](container_images.md#image-tag-requirements).

:::warning
    CloudNativePG is not responsible for PostgreSQL extensions. You must ensure
    that extensions in the source PostgreSQL image are compatible with those in the
    target image and that upgrade paths are supported. Thoroughly test the upgrade
    process in advance to avoid unexpected issues.
    The [extensions management feature](declarative_database_management.md#managing-extensions-in-a-database)
    can help manage extension upgrades declaratively.
:::

### Upgrade Process

1. Shuts down all cluster pods to ensure data consistency.
2. Records the previous PostgreSQL version and image in the cluster’s status under
   `.status.pgDataImageInfo`.
3. Initiates a new upgrade job, which:
   - Verifies that the binaries in the image and the data files align with a
     major upgrade request.
   - Creates new directories for `PGDATA`, and where applicable, WAL files and
     tablespaces.
   - Performs the upgrade using `pg_upgrade` with the `--link` option.
   - Upon successful completion, replaces the original directories with their
     upgraded counterparts.

:::warning
    During the upgrade process, the entire PostgreSQL cluster, including
    replicas, is unavailable to applications. Ensure that your system can
    tolerate this downtime before proceeding.
:::

:::warning
    Performing an in-place upgrade is an exceptional operation that carries inherent
    risks. It is strongly recommended to take a full backup of the cluster before
    initiating the upgrade process.
:::

:::info
    For detailed guidance on `pg_upgrade`, refer to the official
    [PostgreSQL documentation](https://www.postgresql.org/docs/current/pgupgrade.html).
:::

### Post-Upgrade Actions

If the upgrade is successful, CloudNativePG:

- Destroys the PVCs of replicas (if available).
- Scales up replicas as required.

:::warning
    Re-cloning replicas can be time-consuming, especially for very large
    databases. Plan accordingly to accommodate potential delays. After completing
    the upgrade, take a new base backup as soon as possible. Pre-upgrade backups
    and WAL files cannot be used for point-in-time recovery (PITR) across major
    version boundaries. See [Backup and WAL Archive Considerations](#backup-and-wal-archive-considerations)
    for more details.
:::

:::warning
    `pg_upgrade` doesn't transfer optimizer statistics. After the upgrade, you
    may want to run `ANALYZE` on your databases to update them.
:::

If the upgrade fails, you must manually revert the major version change in the
cluster's configuration and delete the upgrade job, as CloudNativePG cannot
automatically decide the rollback.

:::info[Important]
    This process **protects your existing database from data loss**, as no data
    is modified during the upgrade. If the upgrade fails, a rollback is
    usually possible, without having to perform a full recovery from a backup.
    Ensure you monitor the process closely and take corrective action if needed.
:::

### Backup and WAL Archive Considerations

When performing a major upgrade, `pg_upgrade` creates a new database system
with a new System ID and resets the PostgreSQL timeline to 1. This has
implications for backup and WAL archiving:

- **Timeline file conflicts**: New timeline 1 files may overwrite timeline 1
  files from the original cluster.
- **Mixed version archives**: Without intervention, the archive will contain
  WAL files and backups from both PostgreSQL versions.

:::warning
Point-in-time recovery (PITR) is not supported across major PostgreSQL version
boundaries. You cannot use pre-upgrade backups to recover to a point in time
after the upgrade. Take a new base backup as soon as possible after upgrading
to establish a recovery baseline for the new major version.
:::

How backup systems handle major upgrades depends on the plugin implementation.
Some plugins may automatically manage archive separation during upgrades, while
others require manual configuration to use different archive paths for each
major version. Consult your backup plugin documentation for its specific
behavior during major upgrades.

**Example: Manual archive path separation with plugin-barman-cloud**

The `plugin-barman-cloud` backup plugin does not automatically separate
archives during major upgrades. To preserve pre-upgrade backups and keep
archives clean, change the `serverName` parameter when you trigger the upgrade.

Before upgrade (PostgreSQL 16):

```yaml
spec:
  imageName: ghcr.io/cloudnative-pg/postgresql:16-minimal-trixie
  plugins:
    - name: plugin-barman-cloud
      enabled: true
      parameters:
        destinationPath: s3://my-bucket/
        serverName: cluster-example-pg16
```

To trigger the upgrade, change both `imageName` and `serverName` together:

```yaml
spec:
  imageName: ghcr.io/cloudnative-pg/postgresql:17-minimal-trixie
  plugins:
    - name: plugin-barman-cloud
      enabled: true
      parameters:
        destinationPath: s3://my-bucket/
        serverName: cluster-example-pg17
```

With this configuration, the old archive at `cluster-example-pg16` remains
intact for pre-upgrade recovery, while the upgraded cluster writes to
`cluster-example-pg17`.

:::info
The deprecated in-tree `barmanObjectStore` implementation also requires manual
`serverName` changes to separate archives during major upgrades. Consider
migrating to backup plugins for better flexibility and ongoing support.
:::

### Example: Performing a Major Upgrade

Consider the following PostgreSQL cluster running version 16:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-example
spec:
  imageName: ghcr.io/cloudnative-pg/postgresql:16-minimal-trixie
  instances: 3
  storage:
    size: 1Gi
```

You can check the current PostgreSQL version using the following command:

```sh
kubectl cnpg psql cluster-example -- -qAt -c 'SELECT version()'
```

This will return output similar to:

```console
PostgreSQL 16.x ...
```

To upgrade the cluster to version 17, update the `imageName` field by changing
the major version tag from `16` to `17`:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-example
spec:
  imageName: ghcr.io/cloudnative-pg/postgresql:17-minimal-trixie
  instances: 3
  storage:
    size: 1Gi
```

### Upgrade Process

1. Cluster shutdown – All cluster pods are terminated to ensure a consistent
   upgrade.
2. Upgrade job execution – A new job is created with the name of the primary
   pod, appended with the suffix `-major-upgrade`. This job runs `pg_upgrade`
   on the primary’s persistent volume group.
3. Post-upgrade steps:
   - The PVC groups of the replicas (`cluster-example-2` and
     `cluster-example-3`) are removed.
   - The primary pod is restarted.
   - Two new replicas (`cluster-example-4` and `cluster-example-5`) are
     re-cloned from the upgraded primary.

Once the upgrade is complete, you can verify the new major version by running
the same command:

```sh
kubectl cnpg psql cluster-example -- -qAt -c 'SELECT version()'
```

This should now return output similar to:

```console
PostgreSQL 17.x ...
```

You can now update the statistics by running `ANALYZE` on the `app` database:

```sh
kubectl cnpg psql cluster-example -- app -c 'ANALYZE'
```
