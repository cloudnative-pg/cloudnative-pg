# End-to-End Tests

Cloud Native PostgreSQL operator is automatically tested after each
commit via a suite of **End-to-end (E2E) tests**. It ensures that
the operator correctly deploys and manages the PostgreSQL clusters.

Moreover, the following Kubernetes versions are tested for each commit,
ensuring failure and bugs detection at an early stage of the development
process:

* 1.19
* 1.18
* 1.17
* 1.16
* 1.15

The following PostgreSQL versions are tested:

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
* Scale-up of a `Cluster`;
* Scale-down of a `Cluster`;
* Failover;
* Switchover;
* Rolling updates when changing PostgreSQL images;
* Backup and ScheduledBackups execution;
* Restore from backup;
* Primary endpoint switch in case of failure in less than 5 seconds;
* Recover from a degraded state in less than 30 seconds.

The E2E tests suite is also run for OpenShift 4.6 and the latest Kubernetes
and PostgreSQL releases on clusters created on the following services:

* Google GKE
* Amazon EKS
* Microsoft Azure AKS
