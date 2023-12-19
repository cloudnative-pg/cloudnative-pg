# Replication

Physical replication is one of the strengths of PostgreSQL. It's also one of the
reasons why some of the largest organizations in the world have chosen
it to manage their data in business continuity contexts.
Primarily used to achieve high availability, physical replication also allows
scale-out of read-only workloads and offloading of some work from the primary.

!!! Important
    This content is about replication in the same `Cluster` resource
    managed in the same Kubernetes cluster. For information about how to
    replicate with another Postgres `Cluster` resource, even across different
    Kubernetes clusters, see [Replica clusters](replica_cluster.md).

## Application-level replication

After contributing through the years to the replication feature in PostgreSQL,
we decided to build high availability in CloudNativePG on top of
the native physical replication technology and integrate it
directly in the Kubernetes API.

In Kubernetes terms, this is referred to as *application-level replication*, in
contrast with *storage-level replication*.

## A very mature technology

PostgreSQL has a very robust and mature native framework for replicating data
from the primary instance to one or more replicas. It's built around the
concept of transactional changes continuously stored in the write-ahead log (WAL).

Started as the evolution of crash recovery and point-in-time recovery
technologies, physical replication was first introduced in PostgreSQL 8.2
(2006) through WAL shipping from the primary to a warm standby in
continuous recovery.

PostgreSQL 9.0 (2010) enhanced it with WAL streaming and read-only replicas by way of
*hot standby*, while 9.1 (2011) introduced synchronous replication at the
transaction level (for RPO=0 clusters). Cascading replication was released with
PostgreSQL 9.2 (2012). The foundations of logical replication were laid in
PostgreSQL 9.4, while version 10 (2017) introduced native support for the
publisher/subscriber pattern to replicate data from an origin to a destination.

## Streaming replication support

Currently, CloudNativePG natively and transparently manages
physical streaming replicas in a cluster in a declarative way, based on
the number of provided `instances` in the `spec`:

```
replicas = instances - 1 (where  instances > 0)
```

Immediately after initializing a cluster, the operator creates a user
called `streaming_replica` as follows:

```sql
CREATE USER streaming_replica WITH REPLICATION;
   -- NOSUPERUSER INHERIT NOCREATEROLE NOCREATEDB NOBYPASSRLS
```


Out of the box, the operator sets up streaming replication in
the cluster over an encrypted channel and enforces TLS client certificate
authentication for the `streaming_replica` user, as highlighted in the following
excerpt from `pg_hba.conf`:

```
# Require client certificate authentication for the streaming_replica user
hostssl postgres streaming_replica all cert
hostssl replication streaming_replica all cert
```

!!! Seealso "Certificates"
    For details about how CloudNativePG manages certificates,
    see [Certificates](certificates.md#client-streaming_replica-certificate).

If configured for it, the operator manages replication slots for all the replicas in the
HA cluster. It ensures that WAL files required by each standby are retained on
the primary's storage, even after a failover or switchover.

!!! Seealso "Replication slots for high availability"
    For details on how CloudNativePG manages replication slots for the
    high-availability replicas, see
    [Replication slots for high availability](#replication-slots-for-high-availability).

### Continuous backup integration

If continuous backup is configured in the cluster, CloudNativePG
transparently configures replicas to take advantage of `restore_command` when
in continuous recovery. As a result, PostgreSQL can use the WAL archive
as a fallback option whenever pulling WALs by way of streaming replication fails.

## Synchronous replication

CloudNativePG supports configuring *quorum-based synchronous
streaming replication* by way of two configuration options called `minSyncReplicas`
and `maxSyncReplicas`. These are the minimum and the maximum number of expected
synchronous standby replicas available at any time.
For self-healing purposes, the operator always compares these two values with
the number of available replicas to determine the quorum.

!!! Important
    By default, synchronous replication selects among all the available
    replicas indistinctively. You can limit on which nodes your synchronous
    replicas can be scheduled by working on node labels using the
    `syncReplicaElectionConstraint` option. See [Select nodes for synchronous replication](#select-nodes-for-synchronous-replication).

Synchronous replication is disabled by default. (`minSyncReplicas` and
`maxSyncReplicas` aren't defined.)
If both `minSyncReplicas` and `maxSyncReplicas` are set, CloudNativePG
updates the `synchronous_standby_names` option in
PostgreSQL to the following value:

```
ANY q (pod1, pod2, ...)
```

Where:

- `q` is an integer calculated by the operator to be  
  `1 <= minSyncReplicas <= q <= maxSyncReplicas <= readyReplicas`.
- `pod1, pod2, ...` is the list of all PostgreSQL pods in the cluster.

!!! Warning
    To provide self-healing capabilities, the operator can ignore
    `minSyncReplicas` if this value is higher than the currently available
    number of replicas. Synchronous replication is disabled
    when `readyReplicas` is `0`.

As stated in the
[PostgreSQL documentation](https://www.postgresql.org/docs/current/warm-standby.html#SYNCHRONOUS-REPLICATION),
the method `ANY` specifies a quorum-based synchronous replication. It makes
transaction commits wait until their WAL records are replicated to at least the
requested number of synchronous standbys in the list.

!!! Important
    Even though the operator chooses self-healing over enforcement of
    synchronous replication settings, we recommend planning for
    synchronous replication only in clusters with three or more instances or,
    more generally, when `maxSyncReplicas < (instances - 1)`.

### Select nodes for synchronous replication

CloudNativePG enables you to select which PostgreSQL instances are eligible to
participate in a quorum-based synchronous replication. This replication is set through anti-affinity
rules based on the node labels where the PVC holding the PGDATA and the
Postgres pod are.

!!! Seealso "Scheduling"
    For more information on the general pod affinity and anti-affinity rules,
    see [Scheduling](scheduling.md).

An example use case for this feature is in a cluster with a single sync replica.
We can ensure the sync replica will be in a different availability
zone from the primary instance, usually identified by the `topology.kubernetes.io/zone`
[label on a node](https://kubernetes.io/docs/reference/labels-annotations-taints/#topologykubernetesiozone).
This increases the robustness of the cluster in case of an outage in a single
availability zone, especially in terms of recovery-point objective (RPO).

The idea of anti-affinity is to ensure that sync replicas that participate in
the quorum are chosen from pods running on particular nodes. These nodes must have different values for
the selected labels (in this case, the availability zone label) from the node
where the primary is currently in execution. If no node matches the criteria,
the replicas are eligible for synchronous replication.

!!! Important
    The self-healing enforcement still applies while defining additional
    constraints for synchronous replica election
    (see [Synchronous replication](replication.md#synchronous-replication)).

The example shows how this can be done through the
`syncReplicaElectionConstraint` section in `.spec.postgresql`.
`nodeLabelsAntiAffinity` allows you to specify those node labels that need to
be evaluated to make sure that synchronous replication is dynamically
configured by the operator between the current primary and the replicas. The primary and replicas
are located on nodes having a value of the availability zone label different
from that of the node where the primary is.


``` yaml
spec:
  instances: 3
  postgresql:
    syncReplicaElectionConstraint:
      enabled: true
      nodeLabelsAntiAffinity:
      - topology.kubernetes.io/zone
```

The availability zone is just an example, but you can
customize this behavior based on other labels that describe the node, such
as storage, CPU, or memory.

## Replication slots for high availability

[Replication slots](https://www.postgresql.org/docs/current/warm-standby.html#STREAMING-REPLICATION-SLOTS)
are a native PostgreSQL feature introduced in 9.4 that provides an automated way
to ensure that the primary doesn't remove WAL segments until all the attached
streaming replication clients have received them. It also ensures that the primary
doesn't remove rows that can cause a recovery conflict even when the
standby is temporarily disconnected.

A replication slot exists solely on the instance that created it, and PostgreSQL
doesn't replicate it on the standby servers. As a result, after a failover
or a switchover, the new primary doesn't contain the replication slot from
the old primary. This condition can create problems for
the streaming replication clients that were connected to the old
primary and have lost their slot.

CloudNativePG fills this gap by introducing the concept of cluster-managed
replication slots, starting with high-availability clusters. This feature
manages physical replication slots for each hot standby replica
in the high-availability cluster, both in the primary and the standby.

In CloudNativePG, we use these terms:

- **Primary HA slot** – A physical replication slot whose lifecycle is entirely
  managed by the current primary of the cluster and whose purpose is to map to
  a specific standby in streaming replication. This slot lives on the primary
  only.
- **Standby HA slot** – A physical replication slot for a standby whose
  lifecycle is entirely managed by another standby in the cluster. It's based on the
  content of the `pg_replication_slots` view in the primary and updated at regular
  intervals using `pg_replication_slot_advance()`.

This feature, introduced in CloudNativePG 1.18, is now enabled by default and
can be disabled by way of configuration. For details, see
[`replicationSlots` in the API reference](cloudnative-pg.v1.md#postgresql-cnpg-io-v1-ReplicationSlotsConfiguration).
The following is a brief description of the main options:

`.spec.replicationSlots.highAvailability.enabled`
: If true, the feature is enabled (`true` is the default since 1.21).

`.spec.replicationSlots.highAvailability.slotPrefix`
: The prefix that identifies replication slots managed by the operator
  for this feature (default: `_cnpg_`).

`.spec.replicationSlots.updateInterval`
: How often the standby synchronizes the position of the local copy of the
  replication slots with the position on the current primary, expressed in
  seconds (default: 30).

!!! Important
    This capability requires PostgreSQL 11 or later, as it relies on the
    [`pg_replication_slot_advance()` administration function](https://www.postgresql.org/docs/current/functions-admin.html)
    to directly manipulate the position of a replication slot.

!!! Warning
    In PostgreSQL 11, enabling replication slots, if initially disabled, or
    disabling them if initially enabled, requires a rolling update of the
    cluster. The requirement is due to the presence of the `recovery.conf` file that's read only
    at startup.

Although we don't recommend it, if you want different behavior, you can
customize the `replicationSlots` options.

For example, the following manifest creates a cluster with replication
slots disabled.

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-example
spec:
  instances: 3
  # Disable replication slots for HA in the cluster
  replicationSlots:
    highAvailability:
      enabled: false

  storage:
    size: 1Gi
```

You can also control the frequency with which a standby queries the
`pg_replication_slots` view on the primary and updates its local copy of
the replication slots, like in this example:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-example
spec:
  instances: 3
  # Reduce the frequency of standby HA slots updates to once every 5 minutes
  replicationSlots:
    highAvailability:
      enabled: true
    updateInterval: 300

  storage:
    size: 1Gi
```

You must carefully monitor replication slots in your infrastructure. By default,
we provide the `pg_replication_slots` metric in our Prometheus exporter with
key information such as the name of the slot, the type, whether it's active, and
the lag from the primary.

!!! Seealso "Monitoring"
    See [Monitoring](monitoring.md) for details on
    how to monitor a CloudNativePG deployment.

### Capping the WAL size retained for replication slots

When replication slots is enabled, you might end up running out of disk
space due to PostgreSQL trying to retain WAL files requested by a replication
slot. This might happen due to a standby that's (temporarily) down,
lagging, or simply an orphan replication slot.

Starting with PostgreSQL 13, you can take advantage of the
[`max_slot_wal_keep_size`](https://www.postgresql.org/docs/current/runtime-config-replication.html#GUC-MAX-SLOT-WAL-KEEP-SIZE)
configuration option controlling the maximum size of WAL files that replication
slots are allowed to retain in the `pg_wal` directory at checkpoint time.
By default, in PostgreSQL, `max_slot_wal_keep_size` is set to `-1`, meaning that
replication slots can retain an unlimited amount of WAL files.
As a result, we recommend explicitly setting `max_slot_wal_keep_size`
when replication slots support is enabled. For example:

```ini
  # ...
  postgresql:
    parameters:
      max_slot_wal_keep_size: "10GB"
  # ...
```
