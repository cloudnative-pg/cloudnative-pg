---
id: samples
sidebar_position: 510
title: Examples
---

# Examples
<!-- SPDX-License-Identifier: CC-BY-4.0 -->

The examples show configuration files for setting up
your PostgreSQL cluster.

:::info[Important]
    These examples are for demonstration and experimentation
    purposes. You can execute them on a personal Kubernetes cluster with Minikube
    or Kind, as described in [Quick start](quickstart.md).
:::

:::note[Reference]
    For a list of available options, see [API reference](cloudnative-pg.v1.md).
:::

## Basics

**Basic cluster**
:  [`cluster-example.yaml`](samples/cluster-example.yaml)
   A basic example of a cluster.

**Custom cluster**
:  [`cluster-example-custom.yaml`](samples/cluster-example-custom.yaml)
   A basic cluster that uses the default storage class and custom parameters for
   the `postgresql.conf` and `pg_hba.conf` files.

**Cluster with customized storage class**
: [`cluster-storage-class.yaml`](samples/cluster-storage-class.yaml):
   A basic cluster that uses a specified storage class of `standard`.

**Cluster with persistent volume claim (PVC) template configured**
: [`cluster-pvc-template.yaml`](samples/cluster-pvc-template.yaml):
   A basic cluster with an explicit persistent volume claim template.

**Extended configuration example**
: [`cluster-example-full.yaml`](samples/cluster-example-full.yaml):
   A cluster that sets most of the available options.

**Bootstrap cluster with SQL files**
: [`cluster-example-initdb-sql-refs.yaml`](samples/cluster-example-initdb-sql-refs.yaml):
   A cluster example that executes a set of queries defined in a secret and a
   `ConfigMap` right after the database is created.

**Sample cluster with customized `pg_hba` configuration**
: [`cluster-example-pg-hba.yaml`](samples/cluster-example-pg-hba.yaml):
  A basic cluster that enables the user app to authenticate using certificates.

**Sample cluster with Secret and ConfigMap mounted using projected volume template**
: [`cluster-example-projected-volume.yaml`](samples/cluster-example-projected-volume.yaml)
  A basic cluster with the existing `Secret` and `ConfigMap` mounted into Postgres
  pod using projected volume mount.

## Backups

**Customized storage class and backups**
:   *Prerequisites*: Bucket storage must be available. The sample config is for AWS.
    Change it to suit your setup.
: [`cluster-storage-class-with-backup.yaml`](samples/cluster-storage-class-with-backup.yaml) 
   A cluster with backups configured.

**Backup**
:   *Prerequisites*: [`cluster-storage-class-with-backup.yaml`](samples/cluster-storage-class-with-backup.yaml)
    applied and healthy.
: [`backup-example.yaml`](samples/backup-example.yaml):
  An example of a backup that runs against the previous sample.

**Simple cluster with backup configured for minio**
:   *Prerequisites*: The configuration assumes minio is running and working.
    Update `backup.barmanObjectStore` with your minio parameters or your cloud solution.
:  [`cluster-example-with-backup.yaml`](samples/cluster-example-with-backup.yaml)
   A basic cluster with backups configured.

**Simple cluster with backup configured for Scaleway Object Storage**
:   *Prerequisites*: The configuration assumes a Scaleway Object Storage bucket exists.
    Update `backup.barmanObjectStore` with your Scaleway parameters.
:  [`cluster-example-with-backup-scaleway.yaml`](samples/cluster-example-with-backup-scaleway.yaml)
   A basic cluster with backups configured to work with Scaleway Object Storage..

## Replica clusters

**Replica cluster by way of backup from an object store**
:   *Prerequisites*:
    [`cluster-storage-class-with-backup.yaml`](samples/cluster-storage-class-with-backup.yaml)
    applied and healthy, and a backup
    [`cluster-example-trigger-backup.yaml`](samples/cluster-example-trigger-backup.yaml)
    applied and completed.
: [`cluster-example-replica-from-backup-simple.yaml`](samples/cluster-example-replica-from-backup-simple.yaml):
   A replica cluster following a cluster with backup configured.

**Replica cluster by way of volume snapshot**
:   *Prerequisites*:
    [`cluster-example-with-volume-snapshot.yaml`](samples/cluster-example-with-volume-snapshot.yaml)
    applied and healthy, and a volume snapshot
    [`backup-with-volume-snapshot.yaml`](samples/backup-with-volume-snapshot.yaml)
    applied and completed.
: [`cluster-example-replica-from-volume-snapshot.yaml`](samples/cluster-example-replica-from-volume-snapshot.yaml):
   A replica cluster following a cluster with volume snapshot configured.

**Replica cluster by way of streaming (pg_basebackup)**
:   *Prerequisites*: [`cluster-example.yaml`](samples/cluster-example.yaml)
    applied and healthy.
:   [`cluster-example-replica-streaming.yaml`](samples/cluster-example-replica-streaming.yaml): 
   A replica cluster following `cluster-example` with streaming replication.

## PostGIS

**PostGIS example with image volume extensions**
: [`postgis-example.yaml`](samples/postgis-example.yaml):
   An example of a PostGIS cluster using image volume extensions. See [PostGIS](postgis.md) for details.

## Managed roles

**Cluster with declarative role management**
: [`cluster-example-with-roles.yaml`](samples/cluster-example-with-roles.yaml):
  Declares a role with the `managed` stanza. Includes password management with
  Kubernetes secrets.

## Managed services

**Cluster with managed services**
: [`cluster-example-managed-services.yaml`](samples/cluster-example-managed-services.yaml):
  Declares a service with the `managed` stanza. Includes default service disabled and new
  `rw` service template of `LoadBalancer` type defined.

## Declarative tablespaces

**Cluster with declarative tablespaces**
: [`cluster-example-with-tablespaces.yaml`](samples/cluster-example-with-tablespaces.yaml)

**Cluster with declarative tablespaces and backup**
: *Prerequisites*: The configuration assumes minio is running and working.
    Update `backup.barmanObjectStore` with your minio parameters or your cloud solution.
: [`cluster-example-with-tablespaces-backup.yaml`](samples/cluster-example-with-tablespaces-backup.yaml)

**Restored cluster with tablespaces from object store**
: *Prerequisites*: The previous cluster applied and a base backup completed.
    Remember to update `bootstrap.recovery.backup.name` with the backup name.
: [`cluster-restore-with-tablespaces.yaml`](samples/cluster-restore-with-tablespaces.yaml)

For a list of available options, see [API reference](cloudnative-pg.v1.md).

## Pooler configuration

**Pooler with custom service config**
: [`pooler-external.yaml`](samples/pooler-external.yaml)

## Logical replication via declarative Publication and Subscription objects

Two test manifests contain everything needed to set up logical replication:

**Source cluster with a publication**
: [`cluster-example-logical-source.yaml`](samples/cluster-example-logical-source.yaml)

Sets up a cluster, `cluster-example` with some tables created in the `app`
database, and, importantly, *adds replication to the app user*.
A publication is created for the cluster on the `app` database: note that the
publication will be reconciled only after the cluster's primary is up and
running.

**Destination cluster with a subscription**
: *Prerequisites*: The source cluster with publication, defined as above.
: [`cluster-example-logical-destination.yaml`](samples/cluster-example-logical-destination.yaml)

Sets up a cluster `cluster-example-dest` with:

- the source cluster defined in the `externalClusters` stanza. Note that it uses
  the `app` role to connect, which assumes the source cluster grants it
  `replication` privilege.
- a bootstrap import of microservice type, with `schemaOnly` enabled

A subscription is created on the destination cluster: note that the subscription
will be reconciled only after the destination cluster's primary is up and
running.

After both clusters have been reconciled, together with the publication and
subscription objects, you can verify that that tables in the source cluster,
and the data in them, have been replicated in the destination cluster

In addition, there are some standalone example manifests:

**A plain Publication targeting All Tables**
: *Prerequisites*: an existing cluster `cluster-example`.
: [`publication-example.yaml`](samples/publication-example.yaml)

**A Publication with a constrained publication target**
: *Prerequisites*: an existing cluster `cluster-example`.
: [`publication-example-objects.yaml`](samples/publication-example-objects.yaml)

**A plain Subscription**
: Prerequisites: an existing cluster `cluster-example` set up as source, with
    a publication `pub-all`. A cluster `cluster-example-dest` set up as a
    destination cluster, including the `externalClusters` stanza with
    connection parameters to the source cluster, including a role with
    replication privilege.
: [`subscription-example.yaml`](samples/subscription-example.yaml)

All the above manifests create publications or subscriptions on the `app`
database. The Database CRD offers a convenient way to create databases
declaratively. With it, logical replication could be set up for arbitrary
databases.
Which brings us to the next section.

## Declarative management of Postgres databases

**A plain Database**
: *Prerequisites*: an existing cluster `cluster-example`.
: [`database-example.yaml`](samples/database-example.yaml)

**A Database with ICU local specifications**
: *Prerequisites*: an existing cluster `cluster-example` running Postgres 16
  or more advanced.
: [`database-example-icu.yaml`](samples/database-example-icu.yaml)
