---
id: wal_archiving
sidebar_position: 190
title: WAL archiving
---

# WAL archiving
<!-- SPDX-License-Identifier: CC-BY-4.0 -->

Write-Ahead Log (WAL) archiving in CloudNativePG is the process of continuously
shipping WAL files to a designated object store from the PostgreSQL primary.
These archives are essential for enabling Point-In-Time Recovery (PITR) and are
a foundational component for both object store and volume snapshot-based backup
strategies.

## Plugin-Based Architecture

CloudNativePG supports WAL archiving through a **plugin-based mechanism**,
defined via the [`spec.pluginConfiguration`](cloudnative-pg.v1.md#postgresql-cnpg-io-v1-ClusterSpec)
section of the `Cluster` resource.

Only **one plugin at a time** can be responsible for WAL archiving. This is
configured by setting the `isWALArchiver` field to `true` within the plugin
configuration.

## Supported Plugins

Currently, the **Barman Cloud Plugin** is the only officially supported WAL
archiving plugin maintained by the CloudNativePG Community.
For full documentation, configuration options, and best practices, see the
[Barman Cloud Plugin documentation](https://cloudnative-pg.io/plugin-barman-cloud/docs/intro/).

## Deprecation Notice: Native Barman Cloud

CloudNativePG still supports WAL archiving natively through the
`.spec.backup.barmanObjectStore` field. While still functional, **this
interface is deprecated** and will be removed in a future release.

:::info[Important]
    All new deployments are strongly encouraged to adopt the plugin-based
    architecture, which offers a more flexible and maintainable approach.
:::

If you are currently using the native `.spec.backup.barmanObjectStore`
approach, refer to the official guide for a smooth transition:
[Migrating from Built-in CloudNativePG Backup](https://cloudnative-pg.io/plugin-barman-cloud/docs/migration/).

## About the archive timeout

By default, CloudNativePG sets `archive_timeout` to `5min`, ensuring
that WAL files, even in case of low workloads, are closed and archived
at least every 5 minutes, providing a deterministic time-based value for
your Recovery Point Objective ([RPO](before_you_start.md#rpo)).

Even though you change the value of the
[`archive_timeout` setting in the PostgreSQL configuration](https://www.postgresql.org/docs/current/runtime-config-wal.html#GUC-ARCHIVE-TIMEOUT),
our experience suggests that the default value set by the operator is suitable
for most use cases.
