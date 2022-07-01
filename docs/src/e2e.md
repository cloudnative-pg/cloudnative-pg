# End-to-End Tests

CloudNativePG operator is automatically tested after each
commit via a suite of **End-to-end (E2E) tests**. It ensures that
the operator correctly deploys and manages the PostgreSQL clusters.

Moreover, the following Kubernetes versions are tested for each commit,
ensuring failure and bugs detection at an early stage of the development
process:

* 1.24
* 1.23
* 1.22
* 1.21
* 1.20
* 1.19

The following PostgreSQL versions are tested:

* PostgreSQL 15 beta (**not for production!!!**)
* PostgreSQL 14
* PostgreSQL 13
* PostgreSQL 12
* PostgreSQL 11
* PostgreSQL 10

For each tested version of Kubernetes and PostgreSQL, a Kubernetes
cluster is created using [kind](https://kind.sigs.k8s.io/),
and the following suite of E2E tests are performed on that cluster:

* Installation of the operator;
* Creation of a `Cluster`;
* Usage of a persistent volume for data storage;
* Connection via services, including read-only;
* Connection via user-provided server and/or client certificates;
* Scale-up and scale-down of a `Cluster`;
* Failover;
* Switchover;
* Manage PostgreSQL configuration changes;
* Rolling updates when changing PostgreSQL images;
* Backup and ScheduledBackups execution;
* Backup and ScheduledBackups execution using Barman Cloud on Azure blob storage;
* Synchronous replication;
* Restore from backup;
* Restore from backup using Barman Cloud on Azure blob storage;
* Pod affinity using `NodeSelector`;
* Metrics collection;
* JSON log format;
* Operator configuration via ConfigMap;
* Operator pod deletion;
* Operator pod eviction;
* Operator upgrade;
* Operator High Availability;
* Node drain;
* Primary endpoint switch in case of failover in less than 10 seconds;
* Primary endpoint switch in case of switchover in less than 20 seconds;
* Recover from a degraded state in less than 60 seconds;
* Physical replica clusters;
* Storage expansion;
* Data corruption;
