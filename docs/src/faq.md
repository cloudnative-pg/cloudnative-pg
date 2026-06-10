---
id: faq
sidebar_position: 540
title: Frequently Asked Questions (FAQ)
---

# Frequently Asked Questions (FAQ)
<!-- SPDX-License-Identifier: CC-BY-4.0 -->

## Running PostgreSQL in Kubernetes

**Everyone knows that stateful workloads like PostgreSQL cannot run in
Kubernetes. Why do you say the contrary?**

An [*independent research survey commissioned by the Data on Kubernetes
Community*](https://dok.community/dokc-2021-report/) in September 2021
revealed that half of the respondents run most of their production
workloads on Kubernetes. 90% of them believe that Kubernetes is ready
for stateful workloads, and 70% of them run databases in production.
Databases like Postgres. However, according to them, significant
challenges remain, such as the knowledge gap (Kubernetes and Cloud
Native, in general, have a steep learning curve) and the quality of
Kubernetes operators. The latter is the reason why we believe that an
operator like CloudNativePG highly contributes to the success
of your project.

For database fanatics like us, a real game-changer has been the
introduction of the support for local persistent volumes in
[*Kubernetes 1.14 in April 2019*](https://kubernetes.io/blog/2019/04/04/kubernetes-1.14-local-persistent-volumes-ga/).

**CloudNativePG is built on immutable application containers.
What does it mean?**

According to the microservice architectural pattern, a container is
designed to run a single application or process. As a result, such
container images are built to run the main application as the
single entry point (the so-called PID 1 process).

In Kubernetes terms, the application is referred to as workload.
Workloads can be stateless like a web application server or stateful like a
database. Mapping this concept to PostgreSQL, an immutable application
container is a single "postgres" process that is running and
tied to a single and specific version - the one in the immutable
container image.

No other processes such as SSH or systemd, or syslog are allowed.

Immutable Application Containers are in contrast with Mutable System
Containers, which are still a very common way to interpret and use
containers.

Immutable means that a container won't be modified during its life: no
updates, no patches, no configuration changes. If you must update the
application code or apply a patch, you build a new image and redeploy
it. Immutability makes deployments safer and more repeatable.

For more information, please refer to
[*"Why EDB chose immutable application containers"*](https://www.enterprisedb.com/blog/why-edb-chose-immutable-application-containers).

**What does Cloud Native mean?**

The Cloud Native Computing Foundation defines the term
"[*Cloud Native*](https://github.com/cncf/toc/blob/main/DEFINITION.md)".
However, since the start of the Cloud Native PostgreSQL/CloudNativePG operator
at 2ndQuadrant, the development team has been interpreting Cloud Native
as three main concepts:

1.  An existing, healthy, genuine, and prosperous DevOps culture, founded
    on people, as well as principles and processes, which enables teams
    and organizations (as teams of teams) to continuously change so to
    innovate and accelerate the delivery of outcomes and produce value
    for the business in safer, more efficient, and more engaging ways
2.  A microservice architecture that is based on Immutable Application
    Containers
3.  A way to manage and orchestrate these containers, such as Kubernetes

Currently, the standard de facto for container orchestration is
Kubernetes, which automates the deployment, administration and
scalability of Cloud Native Applications.

Another definition of Cloud Native that resonates with us is the one
defined by Ibryam and Huß in
[*"Kubernetes Patterns", published by O'Reilly*](https://www.oreilly.com/library/view/kubernetes-patterns/9781492050278/):

> Principles, Patterns, Tools to automate containerized microservices at scale

**Can I run CloudNativePG on bare metal Kubernetes?**

Yes, definitely. You can run Kubernetes on bare metal. And you can dedicate one
or more physical worker nodes with locally attached storage to PostgreSQL
workloads for maximum and predictable I/O performance.

The actual Cloud Native PostgreSQL project, from which CloudNativePG
originated, was born after a pilot project in 2019 that benchmarked storage and
PostgreSQL on the same bare metal server, first directly in Linux, and then
inside Kubernetes. As expected, the experiment showed only negligible
performance impact introduced by the container running in Kubernetes through
local persistent volumes, allowing the Cloud Native initiative to continue.

**Why should I use PostgreSQL replication instead of file system
replication?**

Please read the ["Architecture: Synchronizing the state"](architecture.md#synchronizing-the-state)
section.


**Why should I use an operator instead of running PostgreSQL as a
container?**

The most basic approach to running PostgreSQL in Kubernetes is to have a
pod, which is the smallest unit of deployment in Kubernetes, running a
Postgres container with no replica. The volume hosting the Postgres data
directory is mounted on the pod, and it usually resides on network
storage. In this case, Kubernetes restarts the pod in case of a
problem or moves it to another Kubernetes node.

The most sophisticated approach is to run PostgreSQL using an operator.
An operator is an extension of the Kubernetes controller and defines how
a complex application works in business continuity contexts. The
operator pattern is currently state of the art in Kubernetes for
this purpose. An operator simulates the work of a human operator in an
automated and programmatic way.

Postgres is a complex application, and an operator not only needs to
deploy a cluster (the first step), but also properly react after
unexpected events. The typical example is that of a failover.

An operator relies on Kubernetes for capabilities like self-healing,
scalability, replication, high availability, backup, recovery, updates,
access, resource control, storage management, and so on. It also
facilitates the integration of a PostgreSQL cluster in the log
management and monitoring infrastructure.

CloudNativePG enables the definition of the desired state of a
PostgreSQL cluster via declarative configuration. Kubernetes
continuously makes sure that the current state of the infrastructure
matches the desired one through reconciliation loops initiated by the
Kubernetes controller. If the desired state and the actual state don't
match, reconciliation loops trigger self-healing procedures. That's
where an operator like CloudNativePG comes into play.

**Are there any other operators for Postgres out there?**

Yes, of course. And our advice is that you look at all of them and compare
them with CloudNativePG before making your decision. You will see that
most of these operators use an external failover management tool (Patroni
or similar) and rely on StatefulSets.

Here is a non exhaustive list, in chronological order from their
publication on GitHub:

* [Crunchy Data Postgres Operator](https://github.com/CrunchyData/postgres-operator) (2017)
* [Zalando Postgres Operator](https://github.com/zalando/postgres-operator) (2017)
* [Stackgres](https://github.com/ongres/stackgres) (2020)
* [Percona Operator for PostgreSQL](https://github.com/percona/percona-postgresql-operator) (2021)
* [Kubegres](https://github.com/reactive-tech/kubegres) (2021)

[![Star History Chart](https://api.star-history.com/svg?repos=cloudnative-pg/cloudnative-pg,zalando/postgres-operator,CrunchyData/postgres-operator,ongres/stackgres,percona/percona-postgresql-operator,reactive-tech/kubegres&type=Date)](https://star-history.com/#cloudnative-pg/cloudnative-pg&zalando/postgres-operator&CrunchyData/postgres-operator&ongres/stackgres&percona/percona-postgresql-operator&reactive-tech/kubegres&Date)

Feel free to report any relevant missing entry as a PR.

:::info
    The [Data on Kubernetes Community](https://dok.community)
    (which includes some of our maintainers) is working on an independent and
    vendor neutral project to list the operators called
    [Operator Feature Matrix](https://github.com/dokc/operator-feature-matrix).
:::

**You say that CloudNativePG is a fully declarative operator.
What do you mean by that?**

The easiest way is to explain declarative configuration through an
example that highlights the differences with imperative configuration.
In an imperative context, the state is defined as a series of tasks to
be executed in sequence. So, we can get a three-node PostgreSQL cluster
by creating the first instance, configuring the replication, cloning a
second instance, and the third one.

In a declarative approach, the state of a system is defined using
configuration, namely: there's a PostgreSQL 13 cluster with two replicas.
This approach highly simplifies change management operations, and when
these are stored in source control systems like Git, it enables the
Infrastructure as Code capability. And Kubernetes takes it farther than
deployment, as it makes sure that our request is fulfilled at any time.

**What are the required skills to run PostgreSQL on Kubernetes?**

Running PostgreSQL on Kubernetes requires both PostgreSQL and Kubernetes
skills in your DevOps team. The best experience is when database
administrators familiarize themselves with Kubernetes core concepts
and are able to interact with Kubernetes administrators.

Our advice is for everyone that wants to fully exploit Cloud Native
PostgreSQL to acquire the "Certified Kubernetes Administrator (CKA)"
status from the CNCF certification program.

**Why isn't CloudNativePG using StatefulSets?**

CloudNativePG does not rely on `StatefulSet` resources, and
instead manages the underlying PVCs directly by leveraging the selected
storage class for dynamic provisioning. Please refer to the
["Custom Pod Controller"](controller.md) section for details and reasons behind
this decision.

## High availability

**What happens to the PostgreSQL clusters when the operator pod dies or it is
not available for a certain amount of time?**

The CloudNativePG operator, among other things, is responsible for self-healing
capabilities. As such, they might not be available during an outage of the
operator.

However, assuming that the outage does not affect the nodes where PostgreSQL
clusters are running, the database will continue to serve normal operations,
through the relevant Kubernetes services. Moreover, the [instance manager](instance_manager.md),
which runs inside each PostgreSQL pod will still work, making sure that the
database server is up, including accessory services like logging, export of
metrics, continuous archiving of WAL files, etc.

To summarize:

an outage of the operator does not necessarily imply a PostgreSQL
database outage; it's like running a database without a DBA or system
administrator.

**What are the reasons behind CloudNativePG not relying on a failover
management tool like Patroni, repmgr, or Stolon?**

Although part of the team that develops CloudNativePG has been heavily
involved in repmgr in the past, we decided to take a different approach
and directly extend the Kubernetes controller and rely on the Kubernetes API
server to hold the status of a Postgres cluster, and use it as the only source
of truth to:

- control High Availability of a Postgres cluster primarily via automated
  failover and switchover, coordinating itself with the [instance manager](instance_manager.md)
- control the Kubernetes services, that is the entry points for your
  applications

**Should I manually resync a former primary with the new one following a
failover?**

No. The operator does that automatically for you, and relies on `pg_rewind` to
synchronize the former primary with the new one.


<!--
How can I ensure that failover (unplanned) and switchover (planned)
times are within our SLA of 99.995% per year?

TODO


TODO


What happens if all nodes of PostgreSQL have a failure?

If all PostgreSQL nodes in a CloudNativePG cluster fail, the operator will continuously attempt to recover the cluster based on the defined Kubernetes resources and available persistent volumes. If the underlying storage (PersistentVolumeClaims) is intact, new pods will be scheduled and PostgreSQL will recover using the existing data. If the storage is lost, recovery will depend on the availability of backups (physical or logical) configured for the cluster. It is strongly recommended to set up regular backups and test restore procedures to ensure business continuity in such scenarios.

## Backup and restore


Does CloudNativePG support logical backups with pg_dump?

Yes, CloudNativePG supports logical backups using standard PostgreSQL tools such as pg_dump and pg_dumpall. You can run these tools against your PostgreSQL cluster as you would with any native PostgreSQL installation. For automated or scheduled logical backups, consider running pg_dump in a Kubernetes Job or using an external backup management tool.


What is the recommended setup for the best outcomes in terms of disaster recovery?

For optimal disaster recovery, it is recommended to:
- Enable continuous physical backups using CloudNativePG's built-in backup features.
- Schedule regular logical backups with pg_dump for additional safety.
- Store backups in a remote, durable object storage (such as S3, GCS, or Azure Blob Storage).
- Regularly test your restore procedures to ensure backups are valid and recovery is possible.


What happens if the Kubernetes cluster where I was running PostgreSQL is permanently gone?

If the Kubernetes cluster is permanently lost, you can recover your PostgreSQL data by creating a new CloudNativePG cluster in a new Kubernetes environment and restoring from your most recent backup. Ensure that your backup storage is external to the lost cluster (e.g., cloud object storage) so it remains accessible for recovery.


## Miscellaneous


**What are the Kubernetes distributions that CloudNativePG supports? What's the rationale behind this decision?**

CloudNativePG is tested and supported on major Kubernetes distributions, including upstream Kubernetes, Red Hat OpenShift, and Rancher. The rationale is to ensure compatibility with widely used, CNCF-compliant platforms, providing flexibility and reliability for users in different environments.

**Are there performance tests or values for large environments (e.g. \>
4 TB/256 GB/64 cores) ?**

TODO

**Question about LDAP/Active Directory support**

TODO

How can the provisioning of CloudNativePG databases be
automated?

TODO

How can the de-provisioning of CloudNativePG be automated?

TODO

Does CloudNativePG wipe the data directory of PostgreSQL when
a cluster is deleted?

TODO

How are changes of resources (storage/CPU/memory) made to a database
instance during runtime?

TODO

How can I get notified via email after a PostgreSQL cluster has been
successfully created, with details about connection?

TODO

## Updates

How can I perform updates on the underlying Kubernetes nodes?

TODO

Can minor version updates of PostgreSQL be carried out without downtime?

TODO

Can major version upgrades of PostgreSQL be carried out without
downtime?

TODO

-->

## Database management

**Why should I use PostgreSQL?**

We believe that PostgreSQL is the equivalent in the database area of
what Linux represents in the operating system space. The current latest
major version of Postgres is version 16, which ships out of the box:

-   native streaming replication, both physical and logical
-   continuous hot backup and point in time recovery
-   declarative partitioning for horizontal table partitioning, which is
    a very well-known technique in the database area to improve vertical
    scalability on a single instance
-   extensibility, with extensions like [PostGIS](postgis.md) for geographical
    databases
-   parallel queries for vertical scalability
-   JSON support, unleashing the multi-model hybrid database for both
    structured and unstructured data queried via standard SQL

And so on ...

**How many databases should be hosted in a single PostgreSQL instance?**

Our recommendation is to dedicate a single PostgreSQL cluster
(intended as primary and multiple standby servers) to a single database,
entirely managed by a single microservice application. However, by
leveraging the "postgres" superuser, it is possible to create as many
users and databases as desired (subject to the available resources).

The reason for this recommendation lies in the Cloud Native concept,
based on microservices. In a pure microservice architecture, the
microservice itself should own the data it manages exclusively.
These could be flat files, queues, key-value stores, or, in our case, a
PostgreSQL relational database containing both structured and
unstructured data. The general idea is that only the microservice can
access the database, including schema management and migrations.

CloudNativePG has been designed to work this way out of the
box, by default creating an application user and an application database
owned by the aforementioned application user.

Reserving a PostgreSQL instance to a single microservice owned database,
enhances:

-   resource management: in PostgreSQL, CPU, and memory constrained
    resources are generally handled at the instance level, not the
    database level, making it easier to integrate it with Kubernetes
    resource management policies at the pod level
-   physical continuous backup and Point-In-Time-Recovery (PITR): given
    that PostgreSQL handles continuous backup and recovery at the
    instance level, having one database per instance simplifies PITR
    operations, differentiates retention policy management, and
    increases data protection of backups
-   application updates: enable each application to decide their update
    policies without impacting other databases owned by different
    applications
-   database updates: each application can decide which PostgreSQL
    version to use, and independently, when to upgrade to a different
    major version of PostgreSQL and at what conditions (e.g., cutover
    time)

**Is there an upper limit in database size for not considering Kubernetes?**

No, as Kubernetes is no different from virtual machines and bare metal as far
as this is regarded.
Practically, however, it depends on the available resources of your Kubernetes
cluster. Our advice with very large databases (VLDB) is to consider a shared
nothing architecture, where a Kubernetes worker node is dedicated to a single
Postgres instance, with dedicated storage.
We proved that this extreme architectural pattern works when we benchmarked
[running PostgreSQL on bare metal Kubernetes with local persistent
volumes](https://www.2ndquadrant.com/en/blog/local-persistent-volumes-and-postgresql-usage-in-kubernetes/).
Tablespaces and horizontal partitioning are data modeling techniques that you
can use to improve the vertical scalability of you databases.

**How can I specify a time zone in the PostgreSQL cluster?**

PostgreSQL has an extensive support for time zones, as explained in the official
documentation:

- [Date time data types](https://www.postgresql.org/docs/current/datatype-datetime.html)
- [Client connections config options](https://www.postgresql.org/docs/current/runtime-config-client.html#GUC-TIMEZONE)

Although time zones can even be used at session, transaction and even as part
of a query in PostgreSQL, a very common way is to set them up globally. With
CloudNativePG you can configure the cluster level time zone in the
`.spec.postgresql.parameters` section as in the following example:

``` yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: pg-italy
spec:
  instances: 1

  postgresql:
    parameters:
      timezone: "Europe/Rome"

  storage:
    size: 1Gi
```

The time zone can be verified with:

``` console
$ kubectl exec -ti pg-italy-1 -c postgres -- psql -x -c "SHOW timezone"
-[ RECORD 1 ]---------
TimeZone | Europe/Rome
```

**What is the recommended architecture for best business continuity
outcomes?**

As covered in the ["Architecture"](architecture.md) section, the main
recommendation is to adopt shared nothing architectures as much as possible, by
leveraging the native capabilities and resources that Kubernetes provides in a
single cluster, namely:

- availability zones: spread your instances across different availability zones
  in the same Kubernetes cluster
- worker nodes: as a consequence, make sure that your Postgres instances reside
  on different Kubernetes worker nodes
- storage: use dedicated storage for each worker node running Postgres

Use at least one standby, preferably at least two, so that you can configure
synchronous replication in the cluster, introducing [RPO](before_you_start.md#postgresql-terminology)=0
for high availability.

If you do not have availability zones - normally the case of on-premise
installations - separate on worker nodes and storage.

Properly setup continuous backup on a local/regional object store.

The same architecture that is in a single Kubernetes cluster can be replicated
in another Kubernetes cluster (normally in another geographical area or region)
through the [replica cluster](replica_cluster.md) feature, providing disaster
recovery and high availability at global scale.

You can use the WAL archive in the primary object store to feed the replica in
the other region, without having to provide a streaming connection, if the default
maximum RPO of 5 minutes is enough for you.

**How can instances be stopped or started?**

Please look at ["Fencing"](fencing.md) or ["Hibernation"](declarative_hibernation.md).


**What are the global objects such as roles and databases that are
automatically created by CloudNativePG?**

The operator automatically creates a user for the application (by default
called `app`) and a database for the application (by default called `app`)
which is owned by the aforementioned user.

This way, the database is ready for a microservice adoption, as developers
can control migrations using the `app` user, without requiring *superuser*
access.

Teams can then create another user for read-write operations through the
["Declarative role management"](declarative_role_management.md) feature
and assign the required `GRANT` to the tables.

<!--
Q: Support for tablespaces

TODO

Q: GUI

TODO

Q: Monitoring

TODO

Q: Logging

TODO


TODO
-->
