# End-to-End Tests

CloudNativePG is automatically tested after each
commit via a suite of **End-to-end (E2E) tests**, which ensures that
the operator correctly deploys and manages PostgreSQL clusters.

Moreover, the following Kubernetes versions are tested for each commit,
helping detect bugs at an early stage of the development process:

* 1.25
* 1.24
* 1.23
* 1.22

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
    * Installation of the operator;
    * Creation of a Cluster;
    * Usage of a persistent volume for data storage;
- **Service connectivity:**
    * Connection via services, including read-only;
    * Connection via user-provided server and/or client certificates;
    * PgBouncer;
- **Self-healing:**
    * Failover;
    * Switchover;
    * Primary endpoint switch in case of failover in less than 10 seconds;
    * Primary endpoint switch in case of switchover in less than 20 seconds;
    * Recover from a degraded state in less than 60 seconds;
    * PVC Deletion;
- **Backup and Restore:**
    * Backup and ScheduledBackups execution using Barman Cloud on S3;
    * Backup and ScheduledBackups execution using Barman Cloud on Azure
    blob storage;
    * Restore from backup using Barman Cloud on S3;
    * Restore from backup using Barman Cloud on Azure blob storage;
    * Wal-Restore;
- **Operator:**
    * Operator Deployment;
    * Operator configuration via ConfigMap;
    * Operator pod deletion;
    * Operator pod eviction;
    * Operator upgrade;
    * Operator High Availability;
- **Observability:**
    * Metrics collection;
    * PgBouncer Metrics;
    * JSON log format;
- **Replication:**
    * Physical replica clusters;
    * Replication Slots;
    * Synchronous replication;
    * Scale-up and scale-down of a Cluster;
- **Plugin:**
    * Cluster Hibernation using CNPG plugin;
    * Fencing;
    * Creation of a connection certificate;
- **Postgres Configuration:**
    * Manage PostgreSQL configuration changes;
    * Rolling updates when changing PostgreSQL images;
- **Pod Scheduling:**
    * Tolerations and taints;
    * Pod affinity using `NodeSelector`;
- **Cluster Metadata:**
    * ConfigMap for Cluster Labels and Annotations;
    * Object metadata;
- **Recovery:**
    * Data corruption;
    * pg_basebackup;
- **Importing Databases:**
    * Microservice approach;
    * Monolith approach;
- **Storage:**
    * Storage expansion;
- **Security:**
    * AppArmor annotation propagation. Executed only on Azure environment;
- **Maintenance:**
    * Node Drain;
