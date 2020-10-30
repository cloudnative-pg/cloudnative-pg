# Operator Capability Levels

This section provides a summary of the capabilities implemented by Cloud Native PostgreSQL,
classified using the
["Operator SDK definition of Capability Levels"](https://sdk.operatorframework.io/docs/advanced-topics/operator-capabilities/operator-capabilities/)
framework.

![Operator Capability Levels](./images/operator-capability-level.png)

Each capability level is associated with a certain set of management features the operator offers:

1. Basic Install
2. Seamless Upgrades
3. Full Lifecycle
4. Deep Insights
5. Auto Pilot

!!! Note
    We consider this framework a guide for future work and implementations in the operator.

## Level 1 - Basic Install

Capability level 1 involves **installation** and **configuration** of the
operator. In this category we include usability and user
experience enhancements, such as improvements in how users interact with the
operator and the configuration of a PostgreSQL cluster.

!!! Important
    We consider **Information Security** part of this level.

### Operator deployment via declarative configuration

The operator is installed in a declarative way using a Kubernetes manifest
which defines 3 `CustomResourceDefinition` objects: `Cluster`, `Backup`,
`ScheduledBackup`.

### PostgreSQL cluster deployment via declarative configuration

A PostgreSQL cluster (operand) is defined using the `Cluster` custom resource
in a fully declarative way. The PostgreSQL version is determined by the
operand container image defined in the CR, which is automatically fetched
from the requested registry. When deploying an operand, the operator also
automatically creates the following resources: `Pod`, `Service`, `Secret`,
`ConfigMap`,`PersistentVolumeClaim`, `PodDisruptionBudget`, `ServiceAccount`,
`RoleBinding`, `Role`.

### Override of operand images through the CRD

The operator is designed to support any operand container image with
PostgreSQL inside.
By default, the operator uses the latest available minor
version of the latest stable major version supported by the PostgreSQL
Community, and published on Quay.io by 2ndQuadrant.
Any compatible image of PostgreSQL supporting the primary/standby
architecture directly can be used by setting the `imageName` attribute in the
CR. The operator also supports `imagePullSecretsNames` to access private
container registries.

### Self-contained instance manager

Instead of relying on an external tool such as Patroni or Stolon to
coordinate PostgreSQL instances in the Kubernetes cluster pods, the operator
injects the operator executable inside each pod, in a file named
`/controller/manager`. The application is used to control the underlying
PostgreSQL instance and to reconcile the pod status with the instance itself
based on the PostgreSQL cluster topology. The instance manager also starts a
web server that is invoked by the `kubelet` for probes. Unix signals invoked
by the `kubelet` are filtered by the instance manager and, where appropriate,
forwarded to the `postmaster` process for fast and controlled reactions to
external events. The instance manager is written in Go and has no external
dependencies.

### Storage configuration

Storage is a critical component in a database workload. Taking advantage of
Kubernetes native capabilities and resources in terms of storage, the
operator gives users enough flexibility to choose the right storage for their
workload requirements, based on what the underlying Kubernetes environment
can offer. This implies choosing a particular storage class in a public cloud
environment or fine tuning the generated PVC through a PVC template in the
`storage` parameter of the CR.

### Replica configuration

The operator automatically detects the presence of replicas in a cluster
through a single parameter called `instances`. If set to `1`, the cluster is
made up of a single primary PostgreSQL instance with no replicas. If higher
than `1`, the operator manages `instances -1` replicas, including high
availability through automated failover and rolling updates through
switchover operations.

### Database configuration

The operator is designed to manage a PostgreSQL cluster with a single
database. The operator transparently manages access to the database through
two Kubernetes services automatically provisioned and managed for read-write
and read-only workloads.
Using the convention over configuration approach, the operator creates a
database called `app`, by default owned by a regular Postgres user with the
same name. Both the database name and the user name can be specified if
required.
Although no configuration is required to run the cluster, users can customise
both PostgreSQL run-time configuration and PostgreSQL Host-Based
Authentication rules in the `postgresql` section of the CR.

### Pod Security Policies

For InfoSec requirements, the operator does not need privileged mode for the
execution of containers and access to volumes both in the operator and in the
operand.

### Current status of the cluster

The operator continuously updates the status section of the CR with the
observed status of the cluster. The entire PostgreSQL cluster status is
continuously monitored by the instance manager running in each pod: the
instance manager is responsible for applying the required changes to the
controlled PostgreSQL instance in order to converge to the required status of
the cluster (for example: if the cluster status reports that pod `-1` is the
primary, pod `-1` needs to promote itself while the other pods need to follow
pod `-1`). The same status is used by Kubernetes client applications to
provide details, including the OpenShift dashboard.

### Multiple installation methods

The operator can be installed through a Kubernetes manifest via `kubectl
apply`, to be used in a traditional Kubernetes installation both in public
and private cloud environments. Additionally, it can be deployed on OpenShift
Container Platform via OperatorHub.

### Convention over configuration

The operator supports the convention over configuration paradigm, deciding
standard default values while allowing users to override them and customise
them. A deployment of a PostgreSQL cluster using the `Cluster` CRD can be
specified in a couple of lines of YAML code.

## Level 2 - Seamless Upgrades

Capability level 2 is about enabling **updates of the operator and the actual
workload**, in our case PostgreSQL servers. This includes **PostgreSQL minor
release updates** (security and bug fixes normally) as well as **major online
upgrades**.

### Upgrade of the operator

The operator can be upgraded seamlessly as a new deployment. A change in the
operator does not require a change in the operand - thanks to the injection
of the instance manager. The operator can manage older versions of the
operand.

### Upgrade of the managed workload

The operand can be upgraded using a declarative configuration approach, as
part of changing the CR and in particular the `imageName` parameter. The
operator prevents major upgrades of PostgreSQL while making it possible to go
in both directions in terms of minor PostgreSQL releases within a major
version (enabling updates and rollbacks).

In case of presence of standby servers, the operator performs rolling updates
starting from the replicas, by dropping the existing pod and creating a new
one with the new requested operand image that reuses the underlying storage.
Depending on the value of the `primaryUpdateStrategy`, the operator proceeds
with a switchover before updating the former primary (`unsupervised`) or
waits for the user to issue the switchover procedure manually (`supervised`).
Which setting to use depends on the business requirements as the operation
might generate some downtime for the applications, from a few seconds to
minutes based on the actual database workload.

## Level 3 - Full Lifecycle

Capability level 3 requires the operator to manage aspects of **business
continuity** and **scalability**.
**Disaster recovery** is a component of business continuity which requires
that both backup and recovery of a database work correctly. While as a
starting point the goal is to achieve RPO < 5 minutes, the long term goal is
to implement RPO=0 backup solutions. **High Availability** is the other
important component of business continuity that, through PostgreSQL native
physical replication and hot standby replicas, allows the operator to perform
failover and switchover operations. This area includes enhancements in:

- control of PostgreSQL physical replication, such as synchronous replication,
  (cascading) replication clusters, and so on;
- connection pooling, in order to improve performance and control through a
  connection pooling layer with pgBouncer.

### PostgreSQL Backups

The operator has been designed to provide application-level backups using
PostgreSQL’s native continuous backup technology, which is based on the
concepts of physical base backups and continuous WAL archiving. Specifically,
the operator currently supports only backups on AWS S3 or S3-compatible
object stores and gateways like MinIO.

WAL archiving and base backups are defined at cluster level, in a declarative
way through the `backup` parameter in the cluster definition, by specifying
an S3 protocol destination URL (for example to point to a specific folder in
an AWS S3 bucket) and, optionally, a generic endpoint URL. WAL archiving,
which is a prerequisite for continuous backup, does not require any further
action from the user: the operator will automatically and transparently set
the the `archive_command` to rely on `barman-cloud-wal-archive` to ship WAL
files to the defined endpoint. Users can decide the compression algorithm

Base backups can be defined in two ways: on-demand (through the `Backup`
custom resource definition) or scheduled (through the `ScheduledBackup`
customer resource definition, using a cron-like syntax). They both rely on
`barman-cloud-backup` for the job (distributed as part of the application
container image) to relay backups in the same endpoint, alongside WAL files.

Both `barman-cloud-wal-restore` and `barman-cloud-backup` are distributed in
the application container image under GNU GPL 3 terms.

### Full restore from a backup

The operator enables users to bootstrap a new cluster (with its own settings)
starting from an existing and accessible backup taken using
`barman-cloud-backup`. Once the bootstrap process is completed, the operator
initiates the instance in recovery mode and replays all available WAL files
from the specified archive, then exits recovery and starts as a primary.
Subsequently, the requested number of standby instances will be cloned from
the primary.

### Liveness and readiness probes

The operator defines liveness and readiness probes for the Postgres
Containers that are then invoked by the kubelet. They are mapped respectively
to the `/healthz` and `/readyz` endpoints of the web server that is managed
directly by the instance manager. They both use Go to connect to the cluster
and issue a simple query (`;`) to verify that the server is ready to accept
connections.

### Rolling deployments

The operator supports rolling deployments to minimise the downtime and, if a
PostgreSQL cluster is exposed publicly, the Service will load-balance the
read-only traffic only to available pods during the initialisation or the
update.

### Scale up and down of replicas

The operator allows users to scale up and down the number of instances in a
PostgreSQL cluster. New replicas are automatically started up from the
primary server and will participate in the HA infrastructure of the cluster.
The CRD declares a "scale" subresource that allows the user to use the
`kubectl scale` command.

### Maintenance window and PodDisruptionBudget for Kubernetes nodes

The operator creates a `PodDisruptionBudget` resource to limit the number of
concurrent disruptions to one. This configuration prevents the maintenance
operation from deleting all the pods in a cluster, allowing the specified
number of instances to be created.
The PodDisruptionBudget will be applied during the node draining operation,
preventing any disruption of the cluster service.

While this strategy is correct for most Kubernetes Clusters, where
storage is shared in all the worker nodes, it may not be the best solution
for clusters using Local Storage or for clusters installed in a Private
Cloud. The operator allows users to specify a Maintenance Window and to
configure the reaction to any underlying node eviction. The `ReusePVC` option
in the maintenance window section enables to specify the strategy to be used:
allocate new storage in a different PVC for the evicted instance or just wait
for the underlying node to be available again.

### Reuse of Persistent Volumes storage in Pods

When the operator needs to create a pod that has been deleted by the user or
has been evicted by a Kubernetes maintenance operation, it reuses the
`PersistentVolumeClaim` if available, avoiding the need
to re-clone the data from the primary.

### CPU and memory requests and limits

The operator allows administrators to control and manage resource usage by
the pods of the cluster, through the `resources` section of the manifest. In
particular `requests` and `limits` values can be set for both CPU and RAM.

## Level 4 - Deep Insights

Capability level 4 is about **observability**: in particular, monitoring,
alerting, trending, log processing. This might involve use of external tools
such as Prometheus, Grafana, Fluent Bit, as well as extensions in the
PostgreSQL engine for the output of error logs directly in JSON format.

*No relevant capability implemented yet in the "Deep Insights" level*.

## Level 5 - Auto Pilot

Capability level 5 is focused on **automated scaling**, **healing** and
**tuning** - through the discovery of anomalies and insights emerged
from the observability layer.

### Automated Failover for self-healing

In case of detected failure on the primary, the operator will change the
status of the cluster by setting the most aligned replica as the new target
primary. As a consequence, the instance manager in each alive pod will
initiate the required procedures to align itself with the requested status of
the cluster, by either becoming the new primary or by following it.
In case the former primary comes back up, the same mechanism will avoid a
split-brain by restarting the server as a standby. In case of issues with
standby resynchronisation, manual intervention might be required (due to lack
of support for `pg_rewind`).

### Automated recreation of a standby

In case the pod hosting a standby has been removed, the operator initiates
the procedure to recreate a standby server.
