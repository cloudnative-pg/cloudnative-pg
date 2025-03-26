# End-to-End Tests
<!-- SPDX-License-Identifier: CC-BY-4.0 -->

CloudNativePG is automatically tested after each
commit via a suite of **End-to-end (E2E) tests** (or integration tests)
which ensure that the operator correctly deploys and manages PostgreSQL
clusters.

Kubernetes versions 1.27 through 1.32, and PostgreSQL versions 13 through 17,
are tested for each commit, helping detect bugs at an early stage of the
development process.

For each tested version of Kubernetes and PostgreSQL, a Kubernetes
cluster is created using [kind](https://kind.sigs.k8s.io/), run on the GitHub
Actions platform,
and the following suite of E2E tests are performed on that cluster:

* **Basic:**
     * Installation of the operator
     * Creation of a Cluster
     * Usage of a persistent volume for data storage

* **Service connectivity:**
     * Connection via services, including read-only
     * Connection via user-provided server and/or client certificates
     * PgBouncer

* **Self-healing:**
     * Failover
     * Switchover
     * Primary endpoint switch in case of failover in less than 10 seconds
     * Primary endpoint switch in case of switchover in less than 20 seconds
     * Recover from a degraded state in less than 60 seconds
     * PVC Deletion
     * Corrupted PVC

* **Backup and Restore:**
     * Backup and restore from Volume Snapshots
     * Backup and ScheduledBackups execution using Barman Cloud on S3
     * Backup and ScheduledBackups execution using Barman Cloud on Azure
    blob storage
     * Restore from backup using Barman Cloud on S3
     * Restore from backup using Barman Cloud on Azure blob storage
     * Point-in-time recovery (PITR) on Azure, S3 storage
     * Wal-Restore (sequential / parallel)

* **Operator:**
     * Operator Deployment
     * Operator configuration via ConfigMap
     * Operator pod deletion
     * Operator pod eviction
     * Operator upgrade
     * Operator High Availability

* **Observability:**
     * Metrics collection
     * PgBouncer Metrics
     * JSON log format

* **Replication:**
     * Replication Slots
     * Synchronous replication
     * Scale-up and scale-down of a Cluster
     * Logical replication via declarative Publication / Subscription

* **Replica clusters**
     * Bootstrapping a replica cluster from backup
     * Bootstrapping a replica cluster via streaming
     * Bootstrapping via volume snapshots
     * Detaching a replica cluster

* **Plugin:**
     * Cluster Hibernation using CNPG plugin
     * Fencing
     * Creation of a connection certificate

* **Postgres Configuration:**
     * Manage PostgreSQL configuration changes
     * Rolling updates when changing PostgreSQL images
     * Rolling updates when changing ImageCatalog/ClusterImageCatalog images
     * Rolling updates on hot standby sensitive parameter changes
     * Database initialization via InitDB

* **Pod Scheduling:**
     * Tolerations and taints
     * Pod affinity using `NodeSelector`
     * Rolling updates on PodSpec drift detection
     * In-place upgrades
     * Multi-Arch availability

* **Cluster Metadata:**
     * ConfigMap for Cluster Labels and Annotations
     * Object metadata

* **Recovery:**
     * Data corruption
     * pg_basebackup

* **Importing Databases:**
     * Microservice approach
     * Monolith approach

* **Storage:**
     * Storage expansion
     * Dedicated PG_WAL persistent volume

* **Security:**
     * AppArmor annotation propagation. Executed only on Azure environment

* **Maintenance:**
     * Node Drain with maintenance window
     * Node Drain with single-instance cluster with/without Pod Disruption Budgets

* **Hibernation**
     * Declarative hibernation / rehydration

* **Volume snapshots**
     * Backup/restore for cold and online snapshots
     * Point-in-time recovery (PITR) for cold and online snapshots
     * Backups via plugin for cold and online snapshots
     * Declarative backups for cold and online snapshots

* **Managed Roles**
     * Creation and update of managed roles
     * Password maintenance using Kubernetes secrets

* **Tablespaces**
     * Declarative creation of tablespaces
     * Declarative creation of temporary tablespaces
     * Backup / recovery from object storage
     * Backup / recovery from volume snapshots

* **Declarative databases**
  * Declarative creation of databases with default (retain) reclaim policy
  * Declarative creation of databases with delete reclaim policy

* **Major version upgrade**
  * Upgrade to the latest major version
