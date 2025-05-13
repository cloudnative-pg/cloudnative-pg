# PostgreSQL Upgrades
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
For example, PostgreSQL 17.1 is compatible with 17.0 and 17.5.

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
container image with a higher PostgreSQL major version is declaratively
requested for a cluster.

You can trigger the upgrade in one of two ways:

- By updating the major version in the image tag via the `.spec.imageName`
  option.
- Using an [image catalog](image_catalog.md) to manage version changes.

For details on supported image tags, see
["Image Tag Requirements"](container_images.md#image-tag-requirements).

!!! Warning
    CloudNativePG is not responsible for PostgreSQL extensions. You must ensure
    that extensions in the source PostgreSQL image are compatible with those in the
    target image and that upgrade paths are supported. Thoroughly test the upgrade
    process in advance to avoid unexpected issues.
    The [extensions management feature](declarative_database_management.md#managing-extensions-in-a-database)
    can help manage extension upgrades declaratively.

### Upgrade Process

1. Shuts down all cluster pods to ensure data consistency.
2. Records the previous PostgreSQL version in the cluster’s status under
   `.status.majorVersionUpgradeFromImage`.
3. Initiates a new upgrade job, which:
   - Verifies that the binaries in the image and the data files align with a
     major upgrade request.
   - Creates new directories for `PGDATA`, and where applicable, WAL files and
     tablespaces.
   - Performs the upgrade using `pg_upgrade` with the `--link` option.
   - Upon successful completion, replaces the original directories with their
     upgraded counterparts.

!!! Warning
    During the upgrade process, the entire PostgreSQL cluster, including
    replicas, is unavailable to applications. Ensure that your system can
    tolerate this downtime before proceeding.

!!! Warning
    Performing an in-place upgrade is an exceptional operation that carries inherent
    risks. It is strongly recommended to take a full backup of the cluster before
    initiating the upgrade process.

!!! Info
    For detailed guidance on `pg_upgrade`, refer to the official
    [PostgreSQL documentation](https://www.postgresql.org/docs/current/pgupgrade.html).

### Post-Upgrade Actions

If the upgrade is successful, CloudNativePG:

- Destroys the PVCs of replicas (if available).
- Scales up replicas as required.

!!! Warning
    Re-cloning replicas can be time-consuming, especially for very large
    databases. Plan accordingly to accommodate potential delays. After completing
    the upgrade, it is strongly recommended to take a full backup. Existing backup
    data (namely base backups and WAL files) is only available for the previous
    minor PostgreSQL release.

!!! Warning
    `pg_upgrade` doesn't transfer optimizer statistics. After the upgrade, you
    may want to run `ANALYZE` on your databases to update them.

If the upgrade fails, you must manually revert the major version change in the
cluster's configuration and delete the upgrade job, as CloudNativePG cannot
automatically decide the rollback.

!!! Important
    This process **protects your existing database from data loss**, as no data
    is modified during the upgrade. If the upgrade fails, a rollback is
    usually possible, without having to perform a full recovery from a backup.
    Ensure you monitor the process closely and take corrective action if needed.

### Example: Performing a Major Upgrade

Consider the following PostgreSQL cluster running version 16:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-example
spec:
  imageName: ghcr.io/cloudnative-pg/postgresql:16-minimal-bookworm
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
  imageName: ghcr.io/cloudnative-pg/postgresql:17-minimal-bookworm
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
