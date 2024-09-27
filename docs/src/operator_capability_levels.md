# Operator capability levels

These capabilities were implemented by CloudNativePG,
classified using the
[Operator SDK definition of Capability Levels](https://operatorframework.io/operator-capabilities/)
framework.

![Operator Capability Levels](./images/operator-capability-level.png)

!!! Important
    Based on the [Operator Capability Levels model](operator_capability_levels.md),
    you can expect a "Level V - Auto Pilot" set of capabilities from the
    CloudNativePG operator.

Each capability level is associated with a certain set of management features the operator offers:

1. Basic install
2. Seamless upgrades
3. Full lifecycle
4. Deep insights
5. Auto pilot

!!! Note
    We consider this framework as a guide for future work and implementations in the operator.

## Level 1: Basic install

Capability level 1 involves installing and configuring the
operator. This category includes usability and user experience
enhancements, such as improvements in how you interact with the
operator and a PostgreSQL cluster configuration.

!!! Important
    We consider information security part of this level.

### Operator deployment via declarative configuration

The operator is installed in a declarative way using a Kubernetes manifest
that defines four major `CustomResourceDefinition` objects: `Cluster`, `Pooler`,
`Backup`, and `ScheduledBackup`.

### PostgreSQL cluster deployment via declarative configuration

You define a PostgreSQL cluster (operand) using the `Cluster` custom resource
in a fully declarative way. The PostgreSQL version is determined by the
operand container image defined in the CR, which is automatically fetched
from the requested registry. When deploying an operand, the operator also
creates the following resources: `Pod`, `Service`, `Secret`,
`ConfigMap`,`PersistentVolumeClaim`, `PodDisruptionBudget`, `ServiceAccount`,
`RoleBinding`, and `Role`.

### Override of operand images through the CRD

The operator is designed to support any operand container image with
PostgreSQL inside.
By default, the operator uses the latest available minor
version of the latest stable major version supported by the PostgreSQL
community and published on ghcr.io.
You can use any compatible image of PostgreSQL supporting the
primary/standby architecture directly by setting the `imageName`
attribute in the CR. The operator also supports `imagePullSecrets`
to access private container registries, and it supports digests and
tags for finer control of container image immutability.
If you prefer not to specify an image name, you can leverage
[image catalogs](image_catalog.md) by simply referencing the PostgreSQL
major version. Moreover, image catalogs enable you to effortlessly create
custom catalogs, directing to images based on your specific requirements.

### Labels and annotations

You can configure the operator to support inheriting labels and annotations
that are defined in a cluster's metadata. The goal is to improve the organization
of the CloudNativePG deployment in your Kubernetes infrastructure.

### Self-contained instance manager

Instead of relying on an external tool to
coordinate PostgreSQL instances in the Kubernetes cluster pods,
such as Patroni or Stolon, the operator
injects the operator executable inside each pod, in a file named
`/controller/manager`. The application is used to control the underlying
PostgreSQL instance and to reconcile the pod status with the instance
based on the PostgreSQL cluster topology. The instance manager also starts a
web server that's invoked by the `kubelet` for probes. Unix signals invoked
by the `kubelet` are filtered by the instance manager. Where appropriate, they're
forwarded to the `postgres` process for fast and controlled reactions to
external events. The instance manager is written in Go and has no external
dependencies.

### Storage configuration

Storage is a critical component in a database workload. Taking advantage of the
Kubernetes native capabilities and resources in terms of storage, the
operator gives you enough flexibility to choose the right storage for your
workload requirements, based on what the underlying Kubernetes environment
can offer. This implies choosing a particular storage class in
a public cloud environment or fine-tuning the generated PVC through a
PVC template in the CR's `storage` parameter.

For better performance and finer control, you can also choose to host your
cluster's write-ahead log (WAL, also known as `pg_wal`) on a separate volume,
preferably on different storage.
The ["Benchmarking"](benchmarking.md) section of the documentation provides
detailed instructions on benchmarking both storage and the database before
production. It relies on the `cnpg` plugin to ensure optimal performance and
reliability.

### Replica configuration

The operator detects replicas in a cluster
through a single parameter, called `instances`. If set to `1`, the cluster
comprises a single primary PostgreSQL instance with no replica. If higher
than `1`, the operator manages `instances -1` replicas, including high
availability (HA) through automated failover and rolling updates through
switchover operations.

CloudNativePG manages replication slots for all the replicas
in the HA cluster. The implementation is inspired by the previously
proposed patch for PostgreSQL, called
[failover slots](https://wiki.postgresql.org/wiki/Failover_slots), and
also supports user defined physical replication slots on the primary.

### Service Configuration

By default, CloudNativePG creates three Kubernetes [services](service_management.md)
for applications to access the cluster via the network:

- One pointing to the primary for read/write operations.
- One pointing to replicas for read-only queries.
- A generic one pointing to any instance for read operations.

You can disable the read-only and read services via configuration.
Additionally, you can leverage the service template capability
to create custom service resources, including load balancers, to access
PostgreSQL outside Kubernetes. This is particularly useful for DBaaS purposes.

### Database configuration

The operator is designed to manage a PostgreSQL cluster with a single
database. The operator transparently manages access to the database through
three Kubernetes services provisioned and managed for read-write,
read, and read-only workloads.
Using the convention-over-configuration approach, the operator creates a
database called `app`, by default owned by a regular Postgres user with the
same name. You can specify both the database name and the user name, if
required.

Although no configuration is required to run the cluster, you can customize
both PostgreSQL runtime configuration and PostgreSQL host-based
authentication rules in the `postgresql` section of the CR.

### Configuration of Postgres roles, users, and groups

CloudNativePG supports
[management of PostgreSQL roles, users, and groups through declarative configuration](declarative_role_management.md)
using the `.spec.managed.roles` stanza.

### Pod security policies

For InfoSec requirements, the operator doesn't require privileged mode for
any container. It enforces a read-only root filesystem to guarantee containers
immutability for both the operator and the operand pods. It also explicitly
sets the required security contexts.

### Affinity

The cluster's `affinity` section enables fine-tuning of how pods and related
resources, such as persistent volumes, are scheduled across the nodes of a
Kubernetes cluster. In particular, the operator supports:

- Pod affinity and anti-affinity
- Node selector
- Taints and tolerations

### Topology spread constraints

The cluster's `topologySpreadConstraints` section enables additional control of
scheduling pods across topologies, enhancing what affinity and
anti-affinity can offer.

### Command-line interface

CloudNativePG doesn't have its own command-line interface.
It relies on the best command-line interface for Kubernetes, kubectl,
by providing a plugin called `cnpg`. This plugin enhances and simplifies your PostgreSQL
cluster management experience.

### Current status of the cluster

The operator continuously updates the status section of the CR with the
observed status of the cluster. The entire PostgreSQL cluster status is
continuously monitored by the instance manager running in each pod. The
instance manager is responsible for applying the required changes to the
controlled PostgreSQL instance to converge to the required status of
the cluster. (For example, if the cluster status reports that pod `-1` is the
primary, pod `-1` needs to promote itself while the other pods need to follow
pod `-1`.) The same status is used by the `cnpg` plugin for kubectl to provide
details.

### Operator's certification authority

The operator creates a certification authority for itself.
It creates and signs with the operator certification authority a leaf certificate
for the webhook server to use. This certificate ensures safe communication between the
Kubernetes API server and the operator.

### Cluster's certification authority

The operator creates a certification authority for every PostgreSQL
cluster. This certification authority is used to issue and renew TLS certificates for clients' authentication,
including streaming replication standby servers (instead of passwords).
Support for a custom certification authority for client certificates is
available through secrets, which also includes integration with cert-manager.
Certificates can be issued with the `cnpg` plugin for kubectl.

### TLS connections

The operator transparently and natively supports TLS/SSL connections
to encrypt client/server communications for increased security using the
cluster's certification authority.
Support for custom server certificates is available through secrets, which also
includes integration with cert-manager.

### Certificate authentication for streaming replication

To authorize streaming replication connections from the standby servers,
the operator relies on TLS client certificate authentication. This method is used
instead of relying on a password (and therefore a secret).

### Continuous configuration management

The operator enables you to apply changes to the `Cluster` resource YAML
section of the PostgreSQL configuration. Depending on the configuration option,
it also makes sure that all instances are properly reloaded or restarted.

!!! Note Current limitation
    Changes with `ALTER SYSTEM` aren't detected, meaning
    that the cluster state isn't enforced.

### Import of existing PostgreSQL databases

The operator provides a declarative way to import existing
Postgres databases in a new CloudNativePG cluster in Kubernetes, using
offline migrations.
The same feature also covers offline major upgrades of PostgreSQL databases.
Offline means that applications must stop their write operations at the source
until the database is imported.
The feature extends the `initdb` bootstrap method to create a new PostgreSQL
cluster using a logical snapshot of the data available in another PostgreSQL
database. This data can be accessed by way of the network through a superuser
connection. Import is from any supported version of Postgres. It relies on
`pg_dump` and `pg_restore` being executed from the new cluster primary
for all databases that are part of the operation and, if requested, for roles.

### PostGIS clusters

CloudNativePG supports the installation of clusters with the [PostGIS](postgis.md)
open source extension for geographical databases. This extension is one of the most popular
extensions for PostgreSQL.

### Basic LDAP authentication for PostgreSQL

The operator allows you to configure LDAP authentication for your PostgreSQL
clients, using either the *simple bind* or *search+bind* mode, as described in
the [LDAP authentication section of the PostgreSQL documentation](https://www.postgresql.org/docs/current/auth-ldap.html).

### Multiple installation methods

The operator can be installed through a Kubernetes manifest by way of `kubectl
apply`, to be used in a traditional Kubernetes installation in public
and private cloud environments. CloudNativePG also supports
installation by way of a Helm chart or OLM bundle from OperatorHub.io.

### Convention over configuration

The operator supports the convention-over-configuration paradigm, deciding
standard default values while allowing you to override them and customize
them. You can specify a deployment of a PostgreSQL cluster using
the `Cluster` CRD in a couple of lines of YAML code.

## Level 2: Seamless upgrades

Capability level 2 is about enabling updates of the operator and the actual
workload, in this case PostgreSQL servers. This includes PostgreSQL minor
release updates (security and bug fixes normally) as well as major online
upgrades.

### Operator Upgrade

Upgrading the operator is seamless and can be done as a new deployment. After
upgrading the controller, a rolling update of all deployed PostgreSQL clusters
is initiated. You can choose to update all clusters simultaneously or
distribute their upgrades over time.

Thanks to the instance manager's injection, upgrading the operator does not
require changes to the operand, allowing the operator to manage older versions
of it.

Additionally, CloudNativePG supports [in-place updates of the instance manager](installation_upgrade.md#in-place-updates-of-the-instance-manager)
following an operator upgrade. In-place updates do not require a rolling update
or a subsequent switchover of the cluster.

### Upgrade of the managed workload

The operand can be upgraded using a declarative configuration approach as
part of changing the CR and, in particular, the `imageName` parameter. The
operator prevents major upgrades of PostgreSQL while making it possible to go
in both directions in terms of minor PostgreSQL releases within a major
version, enabling updates and rollbacks.

In the presence of standby servers, the operator performs rolling updates
starting from the replicas. It does this by dropping the existing pod and creating a new
one with the new requested operand image that reuses the underlying storage.
Depending on the value of the `primaryUpdateStrategy`, the operator proceeds
with a switchover before updating the former primary (`unsupervised`). Or, it waits
for the user to manually issue the switchover procedure (`supervised`) by way of the
`cnpg` plugin for kubectl.
The setting to use depends on the business requirements, as the operation
might generate some downtime for the applications. This downtime can range from a few seconds to
minutes, based on the actual database workload.

### Display cluster availability status during upgrade

At any time, convey the cluster's high availability status, for example,
`Setting up primary`, `Creating a new replica`, `Cluster in healthy state`,
`Switchover in progress`, `Failing over`, and `Upgrading cluster`.

## Level 3: Full lifecycle

Capability level 3 requires the operator to manage aspects of business
continuity and scalability.

*Disaster recovery* is a business continuity component that requires
that both backup and recovery of a database work correctly. While as a
starting point, the goal is to achieve RPO < 5 minutes, the long-term goal is
to implement RPO=0 backup solutions. *High availability* is the other
important component of business continuity. Through PostgreSQL native
physical replication and hot standby replicas, it allows the operator to perform
failover and switchover operations. This area includes enhancements in:

- Control of PostgreSQL physical replication, such as synchronous replication,
  (cascading) replication clusters, and so on
- Connection pooling, to improve performance and control through a
  connection pooling layer with pgBouncer

### PostgreSQL WAL archive

The operator supports PostgreSQL continuous archiving of WAL files
to an object store (AWS S3 and S3-compatible, Azure Blob Storage, Google Cloud
Storage, and gateways like MinIO).

WAL archiving is defined at the cluster level, declaratively, through the
`backup` parameter in the cluster definition. This is done by specifying an S3 protocol
destination URL (for example, to point to a specific folder in an AWS S3
bucket) and, optionally, a generic endpoint URL.

WAL archiving, a prerequisite for continuous backup, doesn't require any further
user action. The operator transparently sets
the `archive_command` to rely on `barman-cloud-wal-archive` to ship WAL
files to the defined endpoint. You can decide the compression algorithm,
as well as the number of parallel jobs to concurrently upload WAL files
in the archive. In addition, `Instance Manager` checks
the correctness of the archive destination by performing the `barman-cloud-check-wal-archive`
command before beginning to ship the first set of WAL files.

### PostgreSQL backups

The operator was designed to provide application-level backups using
PostgreSQL’s native continuous hot backup technology based on
physical base backups and continuous WAL archiving.
Base backups can be saved on:

- Kubernetes volume snapshots
- Object stores (AWS S3 and S3-compatible, Azure Blob Storage, Google Cloud
  Storage, and gateways like MinIO)

Base backups are defined at the cluster level, declaratively,
through the `backup` parameter in the cluster definition.

You can define base backups in two ways:

- On-demand, through the `Backup` custom resource definition
- Scheduled, through the `ScheduledBackup`custom resource definition, using a cron-like syntax

Volume snapshots rely directly on the Kubernetes API, which delegates this
capability to the underlying storage classes and CSI drivers. Volume snapshot
backups are suitable for very large database (VLDB) contexts.

Object store backups rely on `barman-cloud-backup` for the job (distributed as
part of the application container image) to relay backups in the same endpoint,
alongside WAL files.

Both `barman-cloud-wal-restore` and `barman-cloud-backup` are distributed in
the application container image under GNU GPL 3 terms.

Object store backups and volume snapshot backups are taken while PostgreSQL is
up and running (hot backups). Volume snapshots also support taking consistent
database snapshots with cold backups.

### Backups from a standby

The operator supports offloading base backups onto a standby without impacting
the RPO of the database. This allows resources to be preserved on the primary, in
particular I/O, for standard database operations.

### Full restore from a backup

The operator enables you to bootstrap a new cluster (with its settings)
starting from an existing and accessible backup, either on a volume snapshot
or in an object store.

Once the bootstrap process is completed, the operator initiates the instance in
recovery mode. It replays all available WAL files from the specified archive,
exiting recovery and starting as a primary.
Subsequently, the operator clones the requested number of standby instances
from the primary.
CloudNativePG supports parallel WAL fetching from the archive.

### Point-in-time recovery (PITR) from a backup

The operator enables you to create a new PostgreSQL cluster by recovering
an existing backup to a specific point in time, defined with a timestamp, a
label, or a transaction ID. This capability is built on top of the full restore
one and supports all the options available in
[PostgreSQL for PITR](https://www.postgresql.org/docs/current/runtime-config-wal.html#RUNTIME-CONFIG-WAL-RECOVERY-TARGET).

### Zero-Data-Loss Clusters Through Synchronous Replication

Achieve *zero data loss* (RPO=0) in your local high-availability CloudNativePG
cluster with support for both quorum-based and priority-based synchronous
replication. The operator offers a flexible way to define the number of
expected synchronous standby replicas available at any time, and allows
customization of the `synchronous_standby_names` option as needed.

### Replica clusters

Establish a robust cross-Kubernetes cluster topology for PostgreSQL clusters,
harnessing the power of native streaming and cascading replication. With the
`replica` option, you can configure an autonomous cluster to consistently
replicate data from another PostgreSQL source of the same major version. This
source can be located anywhere, provided you have access to a WAL archive for
fetching WAL files or a direct streaming connection via TLS between the two
endpoints.

Notably, the source PostgreSQL instance can exist outside the Kubernetes
environment, whether in a physical or virtual setting.

Replica clusters can be instantiated through various methods, including volume
snapshots, a recovery object store (using the Barman Cloud backup format),
or streaming using `pg_basebackup`. Both WAL file shipping and WAL streaming
are supported. The deployment of replica clusters significantly elevates the
business continuity posture of PostgreSQL databases within Kubernetes,
extending across multiple data centers and facilitating hybrid and multi-cloud
setups. (While anticipating Kubernetes federation native capabilities, manual
switchover across data centers remains necessary.)

Additionally, the flexibility extends to creating delayed replica clusters
intentionally lagging behind the primary cluster. This intentional lag aims to
minimize the Recovery Time Objective (RTO) in the event of unintended errors,
such as incorrect `DELETE` or `UPDATE` SQL operations.

### Distributed Database Topologies

Leverage replica clusters to
define [distributed database topologies](replica_cluster.md#distributed-topology)
for PostgreSQL that span across various Kubernetes clusters, facilitating hybrid
and multi-cloud deployments. With CloudNativePG, you gain powerful capabilities,
including:

- **Declarative Primary Control**: Easily specify which PostgreSQL cluster acts
  as the primary.
- **Seamless Primary Switchover**: Effortlessly demote the current primary and
  promote another PostgreSQL cluster, typically located in a different region,
  without needing to re-clone the former primary.

This setup can efficiently operate across two or more regions, can rely entirely
on object stores for replication, and guarantees a maximum RPO (Recovery Point
Objective) of 5 minutes. This advanced feature is uniquely provided by
CloudNativePG, ensuring robust data integrity and continuity across diverse
environments.

### Tablespace support

CloudNativePG seamlessly integrates robust support for PostgreSQL tablespaces
by facilitating the declarative definition of individual persistent volumes.
This innovative feature empowers you to efficiently distribute I/O operations
across a diverse array of storage devices. Through the transparent
orchestration of tablespaces, CloudNativePG enhances the performance and
scalability of PostgreSQL databases, ensuring a streamlined and optimized
experience for managing large scale data storage in cloud-native environments.
Support for temporary tablespaces is also included.

### Liveness and readiness probes

The operator defines liveness and readiness probes for the Postgres
containers that are then invoked by the kubelet. They're mapped respectively
to the `/healthz` and `/readyz` endpoints of the web server managed
directly by the instance manager.

The liveness probe is based on the `pg_isready` executable, and the pod is
considered healthy with exit codes 0 (server accepting connections normally)
and 1 (server is rejecting connections, for example, during startup). The
readiness probe issues a simple query (`;`) to verify that the server is
ready to accept connections.

### Rolling deployments

The operator supports rolling deployments to minimize the downtime. If a
PostgreSQL cluster is exposed publicly, the service load-balances the
read-only traffic only to available pods during the initialization or the
update.

### Scale up and down of replicas

The operator allows you to scale up and down the number of instances in a
PostgreSQL cluster. New replicas are started up from the
primary server and participate in the cluster's HA infrastructure.
The CRD declares a "scale" subresource that allows you to use the
`kubectl scale` command.

### Maintenance window and PodDisruptionBudget for Kubernetes nodes

The operator creates a `PodDisruptionBudget` resource to limit the number of
concurrent disruptions to one primary instance. This configuration prevents the
maintenance operation from deleting all the pods in a cluster, allowing the
specified number of instances to be created. The PodDisruptionBudget is
applied during the node-draining operation, preventing any disruption of the
cluster service.

While this strategy is correct for Kubernetes clusters where
storage is shared among all the worker nodes, it might not be the best solution
for clusters using local storage or for clusters installed in a private
cloud. The operator allows you to specify a maintenance window and
configure the reaction to any underlying node eviction. The `ReusePVC` option
in the maintenance window section enables to specify the strategy to use.
Allocate new storage in a different PVC for the evicted instance, or wait
for the underlying node to be available again.

### Fencing

Fencing is the process of protecting the data in one, more, or even all
instances of a PostgreSQL cluster when they appear to be malfunctioning.
When an instance is fenced, the PostgreSQL server process is
guaranteed to be shut down, while the pod is kept running. This ensures
that, until the fence is lifted, data on the pod isn't modified by PostgreSQL
and that you can investigate file system for debugging and troubleshooting
purposes.

### Hibernation (declarative)

CloudNativePG supports [hibernation of a running PostgreSQL cluster](declarative_hibernation.md)
in a declarative manner, through the `cnpg.io/hibernation` annotation.
Hibernation enables saving CPU power by removing the database pods while
keeping the database PVCs. This feature simulates scaling to 0 instances.

### Hibernation (imperative)

CloudNativePG supports [hibernation of a running PostgreSQL cluster](kubectl-plugin.md#cluster-hibernation)
by way of the `cnpg` plugin. Hibernation shuts down all Postgres instances in the
high-availability cluster and keeps a static copy of the PVC group of the
primary. The copy contains `PGDATA` and WALs. The plugin enables you to exit the
hibernation phase by resuming the primary and then recreating all the
replicas, if they exist.

### Reuse of persistent volumes storage in pods

When the operator needs to create a pod that was deleted by the user or
was evicted by a Kubernetes maintenance operation, it reuses the
`PersistentVolumeClaim`, if available. This ability avoids the need
to clone the data from the primary again.

### CPU and memory requests and limits

The operator allows administrators to control and manage resource usage by
the cluster's pods in the `resources` section of the manifest. In
particular, you can set `requests` and `limits` values for both CPU and RAM.

### Connection pooling with PgBouncer

CloudNativePG provides native support for connection pooling with
[PgBouncer](connection_pooling.md), one of the most popular open source
connection poolers for PostgreSQL. From an architectural point of view, the
native implementation of a PgBouncer connection pooler introduces a new layer
to access the database. This optimizes the query flow toward the instances
and makes the use of the underlying PostgreSQL resources more efficient.
Instead of connecting directly to a PostgreSQL service, applications can now
connect to the PgBouncer service and start reusing any existing connection.

## Level 4: Deep insights

Capability level 4 is about *observability*: monitoring,
alerting, trending, and log processing. This might involve the use of external tools,
such as Prometheus, Grafana, and Fluent Bit, as well as extensions in the
PostgreSQL engine for the output of error logs directly in JSON format.

CloudNativePG was designed to provide everything needed
to easily integrate with industry-standard and community-accepted tools for
flexible monitoring and logging.

### Prometheus exporter with configurable queries

The instance manager provides a pluggable framework. By way of its own web server
listening on the `metrics` port (9187), it exposes an endpoint to export metrics
for the [Prometheus](https://prometheus.io/) monitoring and alerting tool.
The operator supports custom monitoring queries defined as `ConfigMap`
or `Secret` objects using a syntax that's compatible with
[`postgres_exporter` for Prometheus](https://github.com/prometheus-community/postgres_exporter).
CloudNativePG provides a set of basic monitoring queries for
PostgreSQL that can be integrated and adapted to your context.

### Grafana dashboard

CloudNativePG comes with a Grafana dashboard that you can use as a base to
monitor all critical aspects of a PostgreSQL cluster, and customize.

### Standard output logging of PostgreSQL error messages in JSON format

Every log message is delivered to standard output in JSON format. The first level is the
definition of the timestamp, the log level, and the type of log entry, such as
`postgres` for the canonical PostgreSQL error message channel.
As a result, every pod managed by CloudNativePG can be easily and directly
integrated with any downstream log processing stack that supports JSON as source
data type.

### Real-time query monitoring

CloudNativePG transparently and natively supports:

- The essential [`pg_stat_statements` extension](https://www.postgresql.org/docs/current/pgstatstatements.html),
  which enables tracking of planning and execution statistics of all SQL
  statements executed by a PostgreSQL server
- The [`auto_explain` extension](https://www.postgresql.org/docs/current/auto-explain.html),
  which provides a means for logging execution plans of slow statements
  automatically, without having to manually run `EXPLAIN` (helpful for tracking
  down un-optimized queries)
- The [`pg_failover_slots` extension](https://github.com/EnterpriseDB/pg_failover_slots),
  which makes logical replication slots usable across a physical failover,
  ensuring resilience in change data capture (CDC) contexts based on PostgreSQL's
  native logical replication

### Audit

CloudNativePG allows database and security administrators, auditors,
and operators to track and analyze database activities using PGAudit for
PostgreSQL.
Such activities flow directly in the JSON log and can be properly routed to the
correct downstream target using common log brokers like Fluentd.

### Kubernetes events

Record major events as expected by the Kubernetes API, such as creating resources,
removing nodes, and upgrading. Events can be displayed by using
the `kubectl describe` and `kubectl get events` commands.

## Level 5: Auto pilot

Capability level 5 is focused on automated scaling, healing, and
tuning through the discovery of anomalies and insights that emerged
from the observability layer.

### Automated failover for self-healing

In case of detected failure on the primary, the operator changes the
status of the cluster by setting the most aligned replica as the new target
primary. As a consequence, the instance manager in each alive pod
initiates the required procedures to align itself with the requested status of
the cluster. It does this by either becoming the new primary or by following it.
In case the former primary comes back up, the same mechanism avoids a
split-brain by preventing applications from reaching it, running `pg_rewind` on
the server and restarting it as a standby.

### Automated recreation of a standby

If the pod hosting a standby is removed, the operator initiates
the procedure to re-create a standby server.
