# End-to-end tests

A suite of end-to-end (E2E) tests run on CloudNativePG after each
commit. These tests ensure that
the operator correctly deploys and manages PostgreSQL clusters.

Also, the following Kubernetes versions are tested for each commit.
This testing helps to detect bugs at an early stage of the development process:

* 1.27
* 1.26
* 1.25
* 1.24
* 1.23

The following PostgreSQL versions are tested:

* PostgreSQL 15
* PostgreSQL 14
* PostgreSQL 13
* PostgreSQL 12
* PostgreSQL 11

For each tested version of Kubernetes and PostgreSQL, a Kubernetes
cluster is created using [kind](https://kind.sigs.k8s.io/),
and the following suite of E2E tests are performed on that cluster:

- **Basic:**
    * Installation of the operator
    * Creation of a cluster
    * Usage of a persistent volume for data storage
- **Service connectivity:**
    * Connection by way of services, including read-only
    * Connection by way of user-provided server or client certificates
    * PgBouncer
- **Self-healing:**
    * Failover
    * Switchover
    * Primary endpoint switch in case of failover in less than 10 seconds
    * Primary endpoint switch in case of switchover in less than 20 seconds
    * Recover from a degraded state in less than 60 seconds
    * PVC deletion
- **Backup and restore:**
    * Backup and ScheduledBackups execution using Barman Cloud on S3
    * Backup and ScheduledBackups execution using Barman Cloud on Azure
    blob storage
    * Restore from backup using Barman Cloud on S3
    * Restore from backup using Barman Cloud on Azure blob storage
    * Wal-Restore
- **Operator:**
    * Operator deployment
    * Operator configuration using ConfigMap
    * Operator pod deletion
    * Operator pod eviction
    * Operator upgrade
    * Operator high availability
- **Observability:**
    * Metrics collection
    * PgBouncer metrics
    * JSON log format
- **Replication:**
    * Physical replica clusters
    * Replication slots
    * Synchronous replication
    * Scale-up and scale-down of a cluster
- **Plugin:**
    * Cluster hibernation using CNPG plugin
    * Fencing
    * Creation of a connection certificate
- **Postgres configuration:
    * Manage PostgreSQL configuration changes
    * Rolling updates when changing PostgreSQL images
- **Pod scheduling:**
    * Tolerations and taints
    * Pod affinity using `NodeSelector`
- **Cluster metadata:**
    * ConfigMap for cluster labels and annotations
    * Object metadata
- **Recovery:**
    * Data corruption
    * pg_basebackup
- **Importing databases:**
    * Microservice approach
    * Monolith approach
- **Storage:**
    * Storage expansion
- **Security:**
    * AppArmor annotation propagation executed only on Azure environment
- **Maintenance:**
    * Node drain
