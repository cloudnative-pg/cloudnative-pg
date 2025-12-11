---
id: backup
sidebar_position: 180
title: Backup
---

# Backup
<!-- SPDX-License-Identifier: CC-BY-4.0 -->

:::info
    This section covers **physical backups** in PostgreSQL.
    While PostgreSQL also supports logical backups using the `pg_dump` utility,
    these are **not suitable for business continuity** and are **not managed** by
    CloudNativePG. If you still wish to use `pg_dump`, refer to the
    [*Troubleshooting / Emergency backup* section](troubleshooting.md#emergency-backup)
    for guidance.
:::

:::info[Important]
    Starting with version 1.26, native backup and recovery capabilities are
    being **progressively phased out** of the core operator and moved to official
    CNPG-I plugins. This transition aligns with CloudNativePG's shift towards a
    **backup-agnostic architecture**, enabled by its extensible
    interface—**CNPG-I**—which standardizes the management of **WAL archiving**,
    **physical base backups**, and corresponding **recovery processes**.
:::

CloudNativePG currently supports **physical backups of PostgreSQL clusters** in
two main ways:

- **Via [CNPG-I](https://github.com/cloudnative-pg/cnpg-i/) plugins**: the
  CloudNativePG Community officially supports the [**Barman Cloud Plugin**](https://cloudnative-pg.io/plugin-barman-cloud/)
  for integration with object storage services.

- **Natively**, with support for:

    - [Object storage via Barman Cloud](appendixes/backup_barmanobjectstore.md)
      *(although deprecated from 1.26 in favor of the Barman Cloud Plugin)*
    - [Kubernetes Volume Snapshots](appendixes/backup_volumesnapshot.md), if
      supported by the underlying storage class

Before selecting a backup strategy with CloudNativePG, it's important to
familiarize yourself with the foundational concepts covered in the ["Main Concepts"](#main-concepts)
section. These include WAL archiving, hot and cold backups, performing backups
from a standby, and more.

## Main Concepts

PostgreSQL natively provides first class backup and recovery capabilities based
on file system level (physical) copy. These have been successfully used for
more than 15 years in mission critical production databases, helping
organizations all over the world achieve their disaster recovery goals with
Postgres.

In CloudNativePG, the backup infrastructure for each PostgreSQL cluster is made
up of the following resources:

- **WAL archive**: a location containing the WAL files (transactional logs)
  that are continuously written by Postgres and archived for data durability
- **Physical base backups**: a copy of all the files that PostgreSQL uses to
  store the data in the database (primarily the `PGDATA` and any tablespace)

CNPG-I provides a generic and extensible interface for managing WAL archiving
(both archive and restore operations), as well as the base backup and
corresponding restore processes.

### WAL archive

The WAL archive in PostgreSQL is at the heart of **continuous backup**, and it
is fundamental for the following reasons:

- **Hot backups**: the possibility to take physical base backups from any
  instance in the Postgres cluster (either primary or standby) without shutting
  down the server; they are also known as online backups
- **Point in Time recovery** (PITR): the possibility to recover at any point in
  time from the first available base backup in your system

:::warning
    WAL archive alone is useless. Without a physical base backup, you cannot
    restore a PostgreSQL cluster.
:::

In general, the presence of a WAL archive enhances the resilience of a
PostgreSQL cluster, allowing each instance to fetch any required WAL file from
the archive if needed (normally the WAL archive has higher retention periods
than any Postgres instance that normally recycles those files).

This use case can also be extended to [replica clusters](replica_cluster.md),
as they can simply rely on the WAL archive to synchronize across long
distances, extending disaster recovery goals across different regions.

When you [configure a WAL archive](wal_archiving.md), CloudNativePG provides
out-of-the-box an [RPO](before_you_start.md#rpo) ≤ 5 minutes for disaster
recovery, even across regions.

:::info[Important]
    Our recommendation is to always setup the WAL archive in production.
    There are known use cases — normally involving staging and development
    environments — where none of the above benefits are needed and the WAL
    archive is not necessary. RPO in this case can be any value, such as
    24 hours (daily backups) or infinite (no backup at all).
:::

### Cold and Hot backups

Hot backups have already been defined in the previous section. They require the
presence of a WAL archive, and they are the norm in any modern database
management system.

**Cold backups**, also known as offline backups, are instead physical base backups
taken when the PostgreSQL instance (standby or primary) is shut down. They are
consistent per definition, and they represent a snapshot of the database at the
time it was shut down.

As a result, PostgreSQL instances can be restarted from a cold backup without
the need of a WAL archive, even though they can take advantage of it, if
available (with all the benefits on the recovery side highlighted in the
previous section).

In those situations with a higher RPO (for example, 1 hour or 24 hours), and
shorter retention periods, cold backups represent a viable option to be considered
for your disaster recovery plans.

## Comparing Available Backup Options: Object Stores vs Volume Snapshots

CloudNativePG currently supports two main approaches for physical backups:

- **Object store–based backups**, via the [**Barman Cloud
  Plugin**](https://cloudnative-pg.io/plugin-barman-cloud/) or the
  [**deprecated native integration**](appendixes/backup_barmanobjectstore.md)
- [**Volume Snapshots**](appendixes/backup_volumesnapshot.md), using the
  Kubernetes CSI interface and supported storage classes

:::info[Important]
    CNPG-I is designed to enable third parties to build and integrate their own
    backup plugins. Over time, we expect the ecosystem of supported backup
    solutions to grow.
:::

### Object Store–Based Backups

Backups to an object store (e.g. AWS S3, Azure Blob, GCS):

- Always require WAL archiving
- Support hot backups only
- Do not support incremental or differential copies
- Support retention policies

### Volume Snapshots

Native volume snapshots:

- Do not require WAL archiving, though its use is still strongly
  recommended in production
- Support incremental and differential copies, depending on the
  capabilities of the underlying storage class
- Support both hot and cold backups
- Do not support retention policies

### Choosing Between the Two

The best approach depends on your environment and operational requirements.
Consider the following factors:

- **Object store availability**: Ensure your Kubernetes cluster can access a
  reliable object storage solution, including a stable networking layer.
- **Storage class capabilities**: Confirm that your storage class supports CSI
  volume snapshots with incremental/differential features.
- **Database size**: For very large databases (VLDBs), **volume snapshots are
  generally preferred** as they enable faster recovery due to copy-on-write
  technology—this significantly improves your
  [Recovery Time Objective (RTO)](before_you_start.md#rto).
- **Data mobility**: Object store–based backups may offer greater flexibility
  for replicating or storing backups across regions or environments.
- **Operational familiarity**: Choose the method that aligns best with your
  team's experience and confidence in managing storage.

### Comparison Summary

| Feature                           | Object Store |   Volume Snapshots   |
|-----------------------------------|:------------:|:--------------------:|
| **WAL archiving**                 |   Required   |    Recommended^1^    |
| **Cold backup**                   |      ❌       |          ✅           |
| **Hot backup**                    |      ✅       |          ✅           |
| **Incremental copy**              |      ❌       |         ✅^2^         |
| **Differential copy**             |      ❌       |         ✅^2^         |
| **Backup from a standby**         |      ✅       |          ✅           |
| **Snapshot recovery**             |     ❌^3^     |          ✅           |
| **Retention policies**            |      ✅       |          ❌           |
| **Point-in-Time Recovery (PITR)** |      ✅       | Requires WAL archive |
| **Underlying technology**         | Barman Cloud |    Kubernetes API    |

---

> **Notes:**
>
> 1. WAL archiving must currently use an object store through a plugin (or the
>    deprecated native one).
> 2. Availability of incremental and differential copies depends on the
>    capabilities of the storage class used for PostgreSQL volumes.
> 3. Snapshot recovery can be emulated by using the
>    `bootstrap.recovery.recoveryTarget.targetImmediate` option.

## Scheduled Backups

Scheduled backups are the recommended way to implement a reliable backup
strategy in CloudNativePG. They are defined using the `ScheduledBackup` custom
resource.

:::info
    For a complete list of configuration options, refer to the
    [`ScheduledBackupSpec`](cloudnative-pg.v1.md#scheduledbackupspec)
    in the API reference.
:::

### Cron Schedule

The `schedule` field defines **when** the backup should occur, using a
*six-field cron expression* that includes seconds. This format follows the
[Go `cron` package specification](https://pkg.go.dev/github.com/robfig/cron#hdr-CRON_Expression_Format).

:::warning
    This format differs from the traditional Unix/Linux `crontab`—it includes a
    **seconds** field as the first entry.
:::

Example of a daily scheduled backup:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: ScheduledBackup
metadata:
  name: backup-example
spec:
  schedule: "0 0 0 * * *"  # At midnight every day
  backupOwnerReference: self
  cluster:
    name: pg-backup
  # method: plugin, volumeSnapshot, or barmanObjectStore (default)
```

The schedule `"0 0 0 * * *"` triggers a backup every day at midnight
(00:00:00). In Kubernetes CronJobs, the equivalent expression would be `0 0 * * *`,
since seconds are not supported.

### Backup Frequency and RTO

:::tip[Hint]
    The frequency of your backups directly impacts your **Recovery Time Objective**
    ([RTO](before_you_start.md#rto)).
:::

To optimize your disaster recovery strategy based on continuous backup:

- Regularly test restoring from your backups.
- Measure the time required for a full recovery.
- Account for the size of base backups and the number of WAL files that must be
  retrieved and replayed.

In most cases, a **weekly base backup** is sufficient. It is rare to schedule
full backups more frequently than once per day.

### Immediate Backup

To trigger a backup immediately when the `ScheduledBackup` is created:

```yaml
spec:
  immediate: true
```

### Pause Scheduled Backups

To temporarily stop scheduled backups from running:

```yaml
spec:
  suspend: true
```

### Backup Owner Reference (`.spec.backupOwnerReference`)

Controls which Kubernetes object is set as the owner of the backup resource:

- `none`: No owner reference (legacy behavior)
- `self`: The `ScheduledBackup` object becomes the owner
- `cluster`: The PostgreSQL cluster becomes the owner

## On-Demand Backups

On-demand backups allow you to manually trigger a backup operation at any time
by creating a `Backup` resource.

:::info
    For a full list of available options, see the
    [`BackupSpec`](cloudnative-pg.v1.md#backupspec) in the
    API reference.
:::

### Example: Requesting an On-Demand Backup

To start an on-demand backup, apply a `Backup` request custom resource like the
following:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Backup
metadata:
  name: backup-example
spec:
  method: barmanObjectStore
  cluster:
    name: pg-backup
```

In this example, the operator will orchestrate the backup process using the
`barman-cloud-backup` tool and store the backup in the configured object store.

### Monitoring Backup Progress

You can check the status of the backup using:

```bash
kubectl describe backup backup-example
```

While the backup is in progress, you'll see output similar to:

```text
Name:         backup-example
Namespace:    default
...
Spec:
  Cluster:
    Name:  pg-backup
Status:
  Phase:       running
  Started At:  2020-10-26T13:57:40Z
Events:        <none>
```

Once the backup has successfully completed, the `phase` will be set to
`completed`, and the output will include additional metadata:

```text
Name:         backup-example
Namespace:    default
...
Status:
  Backup Id:         20201026T135740
  Destination Path:  s3://backups/
  Endpoint URL:      http://minio:9000
  Phase:             completed
  S3 Credentials:
    Access Key Id:
      Name:  minio
      Key:   ACCESS_KEY_ID
    Secret Access Key:
      Name:  minio
      Key:   ACCESS_SECRET_KEY
  Server Name:       pg-backup
  Started At:        2020-10-26T13:57:40Z
  Stopped At:        2020-10-26T13:57:44Z
```

---

:::info[Important]
    On-demand backups do **not** include Kubernetes secrets for the PostgreSQL
    superuser or application user. You should ensure these secrets are included in
    your broader Kubernetes cluster backup strategy.
:::

## Backup Methods

CloudNativePG currently supports the following backup methods for scheduled
and on-demand backups:

- `plugin` – Uses a CNPG-I plugin (requires `.spec.pluginConfiguration`)
- `volumeSnapshot` – Uses native [Kubernetes volume snapshots](appendixes/backup_volumesnapshot.md#how-to-configure-volume-snapshot-backups)
- `barmanObjectStore` – Uses [Barman Cloud for object storage](appendixes/backup_barmanobjectstore.md)
  *(deprecated starting with v1.26 in favor of the
  [Barman Cloud Plugin](https://cloudnative-pg.io/plugin-barman-cloud/),
  but still the default for backward compatibility)*

Specify the method using the `.spec.method` field (defaults to
`barmanObjectStore`).

If your cluster is configured to support volume snapshots, you can enable
scheduled snapshot backups like this:

```yaml
spec:
  method: volumeSnapshot
```

To use the Barman Cloud Plugin as the backup method, set `method: plugin` and
configure the plugin accordingly. You can find an example in the
["Performing a Base Backup" section of the plugin documentation](https://cloudnative-pg.io/plugin-barman-cloud/docs/usage/#performing-a-base-backup)

## Backup from a Standby

Taking a base backup involves reading the entire on-disk data set of a
PostgreSQL instance, which can introduce I/O contention and impact the
performance of the active workload.

To reduce this impact, **CloudNativePG supports taking backups from a standby
instance**, leveraging PostgreSQL’s built-in capability to perform backups from
read-only replicas.

By default, backups are performed on the **most up-to-date replica** in the
cluster. If no replicas are available, the backup will fall back to the
**primary instance**.

:::note
    The examples in this section are focused on backup target selection and do not
    take the backup method (`spec.method`) into account, as it is not relevant to
    the scope being discussed.
:::

### How It Works

When `prefer-standby` is the target (the default behavior), CloudNativePG will
attempt to:

1. Identify the most synchronized standby node.
2. Run the backup process on that standby.
3. Fall back to the primary if no standbys are available.

This strategy minimizes interference with the primary’s workload.

:::warning
    Although the standby might not always be up to date with the primary,
    in the time continuum from the first available backup to the last
    archived WAL this is normally irrelevant. The base backup indeed
    represents the starting point from which to begin a recovery operation,
    including PITR. Similarly to what happens with
    [`pg_basebackup`](https://www.postgresql.org/docs/current/app-pgbasebackup.html),
    when backing up from an online standby we do not force a switch of the WAL on the
    primary. This might produce unexpected results in the short term (before
    `archive_timeout` kicks in) in deployments with low write activity.
:::

### Forcing Backup on the Primary

To always run backups on the primary instance, explicitly set the backup target
to `primary` in the cluster configuration:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  [...]
spec:
  backup:
    target: "primary"
```

:::warning
    Be cautious when using `primary` as the target for **cold backups using
    volume snapshots**, as this will require shutting down the primary instance
    temporarily—interrupting all write operations. The same caution applies to
    single-instance clusters, even if you haven't explicitly set the target.
:::

### Overriding the Cluster-Wide Target

You can override the cluster-level target on a per-backup basis, using either
`Backup` or `ScheduledBackup` resources. Here's an example of an on-demand
backup:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Backup
metadata:
  [...]
spec:
  cluster:
    name: [...]
  target: "primary"
```

In this example, even if the cluster’s default target is `prefer-standby`, the
backup will be taken from the primary instance.

## Retention Policies

CloudNativePG is evolving toward a **backup-agnostic architecture**, where
backup responsibilities are delegated to external **CNPG-I plugins**. These
plugins are expected to offer advanced and customizable data protection
features, including sophisticated retention management, that go beyond the
built-in capabilities and scope of CloudNativePG.

As part of this transition, the `spec.backup.retentionPolicy` field in the
`Cluster` resource is **deprecated** and will be removed in a future release.

For more details on available retention features, refer to your chosen plugin’s documentation.
For example: ["Retention Policies" with Barman Cloud Plugin](https://cloudnative-pg.io/plugin-barman-cloud/docs/retention/).

:::info[Important]
    Users are encouraged to rely on the retention mechanisms provided by the
    backup plugin they are using. This ensures better flexibility and consistency
    with the backup method in use.
:::