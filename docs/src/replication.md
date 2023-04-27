# Replication

Physical replication is one of the strengths of PostgreSQL and one of the
reasons why some of the largest organizations in the world have chosen
it for the management of their data in business continuity contexts.
Primarily used to achieve high availability, physical replication also allows
scale-out of read-only workloads and offloading of some work from the primary.

!!! Important
    This section is about replication within the same `Cluster` resource
    managed in the same Kubernetes cluster. For information about how to
    replicate with another Postgres `Cluster` resource, even across different
    Kubernetes clusters, please refer to the ["Replica clusters"](replica_cluster.md)
    section.

## Application-level replication

Having contributed throughout the years to the replication feature in PostgreSQL,
we have decided to build high availability in CloudNativePG on top of
the native physical replication technology, and integrate it
directly in the Kubernetes API.

In Kubernetes terms, this is referred to as **application-level replication**, in
contrast with *storage-level replication*.

## A very mature technology

PostgreSQL has a very robust and mature native framework for replicating data
from the primary instance to one or more replicas, built around the
concept of transactional changes continuously stored in the WAL (Write Ahead Log).

Started as the evolution of crash recovery and point in time recovery
technologies, physical replication was first introduced in PostgreSQL 8.2
(2006) through WAL shipping from the primary to a warm standby in
continuous recovery.

PostgreSQL 9.0 (2010) enhanced it with WAL streaming and read-only replicas via
*hot standby*, while 9.1 (2011) introduced synchronous replication at the
transaction level (for RPO=0 clusters). Cascading replication was released with
PostgreSQL 9.2 (2012). The foundations of logical replication were laid in
PostgreSQL 9.4, while version 10 (2017) introduced native support for the
publisher/subscriber pattern to replicate data from an origin to a destination.

## Streaming replication support

At the moment, CloudNativePG natively and transparently manages
physical streaming replicas within a cluster in a declarative way, based on
the number of provided `instances` in the `spec`:

```
replicas = instances - 1 (where  instances > 0)
```

Immediately after the initialization of a cluster, the operator creates a user
called `streaming_replica` as follows:

```sql
CREATE USER streaming_replica WITH REPLICATION;
   -- NOSUPERUSER INHERIT NOCREATEROLE NOCREATEDB NOBYPASSRLS
```


Out of the box, the operator automatically sets up streaming replication within
the cluster over an encrypted channel and enforces TLS client certificate
authentication for the `streaming_replica` user - as highlighted by the following
excerpt taken from `pg_hba.conf`:

```
# Require client certificate authentication for the streaming_replica user
hostssl postgres streaming_replica all cert
hostssl replication streaming_replica all cert
```

!!! Seealso "Certificates"
    For details on how CloudNativePG manages certificates, please refer
    to the ["Certificates" section](certificates.md#client-streaming_replica-certificate)
    in the documentation.

If configured, the operator manages replication slots for all the replicas in the
HA cluster, ensuring that WAL files required by each standby are retained on
the primary's storage, even after a failover or switchover.

!!! Seealso "Replication slots for High Availability"
    For details on how CloudNativePG automatically manages replication slots for the
    High Availability replicas, please refer to the
    ["Replication slots for High Availability" section](#replication-slots-for-high-availability)
    below.

### Continuous backup integration

In case continuous backup is configured in the cluster, CloudNativePG
transparently configures replicas to take advantage of `restore_command` when
in continuous recovery. As a result, PostgreSQL can use the WAL archive
as a fallback option whenever pulling WALs via streaming replication fails.

## Synchronous replication

CloudNativePG supports the configuration of **quorum-based synchronous
streaming replication** via two configuration options called `minSyncReplicas`
and `maxSyncReplicas`, which are the minimum and the maximum number of expected
synchronous standby replicas available at any time.
For self-healing purposes, the operator always compares these two values with
the number of available replicas to determine the quorum.

!!! Important
    By default, synchronous replication selects among all the available
    replicas indistinctively. You can limit on which nodes your synchronous
    replicas can be scheduled, by working on node labels through the
    `syncReplicaElectionConstraint` option as described in the next section.

Synchronous replication is disabled by default (`minSyncReplicas` and
`maxSyncReplicas` are not defined).
In case both `minSyncReplicas` and `maxSyncReplicas` are set, CloudNativePG
automatically updates the `synchronous_standby_names` option in
PostgreSQL to the following value:

```
ANY q (pod1, pod2, ...)
```

Where:

- `q` is an integer automatically calculated by the operator to be:  
  `1 <= minSyncReplicas <= q <= maxSyncReplicas <= readyReplicas`
- `pod1, pod2, ...` is the list of all PostgreSQL pods in the cluster

!!! Warning
    To provide self-healing capabilities, the operator can ignore
    `minSyncReplicas` if such value is higher than the currently available
    number of replicas. Synchronous replication is automatically disabled
    when `readyReplicas` is `0`.

As stated in the
[PostgreSQL documentation](https://www.postgresql.org/docs/current/warm-standby.html#SYNCHRONOUS-REPLICATION),
the *method `ANY` specifies a quorum-based synchronous replication and makes
transaction commits wait until their WAL records are replicated to at least the
requested number of synchronous standbys in the list*.

!!! Important
    Even though the operator chooses self-healing over enforcement of
    synchronous replication settings, our recommendation is to plan for
    synchronous replication only in clusters with 3+ instances or,
    more generally, when `maxSyncReplicas < (instances - 1)`.

### Select nodes for synchronous replication

CloudNativePG enables you to select which PostgreSQL instances are eligible to
participate in a quorum-based synchronous replication set through anti-affinity
rules based on the node labels where the PVC holding the PGDATA and the
Postgres pod are.

!!! Seealso "Scheduling"
    For more information on the general pod affinity and anti-affinity rules,
    please check the ["Scheduling" section](scheduling.md).

As an example use-case for this feature: in a cluster with a single sync replica,
we would be able to ensure the sync replica will be in a different availability
zone from the primary instance, usually identified by the `topology.kubernetes.io/zone`
[label on a node](https://kubernetes.io/docs/reference/labels-annotations-taints/#topologykubernetesiozone).
This would increase the robustness of the cluster in case of an outage in a single
availability zone, especially in terms of recovery point objective (RPO).

The idea of anti-affinity is to ensure that sync replicas that participate in
the quorum are chosen from pods running on nodes that have different values for
the selected labels (in this case, the availability zone label) then the node
where the primary is currently in execution. If no node matches such criteria,
the replicas are eligible for synchronous replication.

!!! Important
    The self-healing enforcement still applies while defining additional
    constraints for synchronous replica election
    (see ["Synchronous replication"](replication.md#synchronous-replication)).

The example below shows how this can be done through the
`syncReplicaElectionConstraint` section within `spec.postgresql`.
`nodeLabelsAntiAffinity` allows you to specify those node labels that need to
be evaluated to make sure that synchronous replication will be dynamically
configured by the operator between the current primary and the replicas which
are located on nodes having a value of the availability zone label different
from that of the node where the primary is:


``` yaml
spec:
  instances: 3
  postgresql:
    syncReplicaElectionConstraint:
      enabled: true
      nodeLabelsAntiAffinity:
      - topology.kubernetes.io/zone
```

As you can imagine, the availability zone is just an example, but you could
customize this behavior based on other labels that describe the node, such
as storage, CPU, or memory.

## Replication slots for High Availability

[Replication slots](https://www.postgresql.org/docs/current/warm-standby.html#STREAMING-REPLICATION-SLOTS)
are a native PostgreSQL feature introduced in 9.4 that provides an automated way
to ensure that the primary does not remove WAL segments until all the attached
streaming replication clients have received them, and that the primary
does not remove rows which could cause a recovery conflict even when the
standby is (temporarily) disconnected.

A replication slot exists solely on the instance that created it, and PostgreSQL
does not replicate it on the standby servers. As a result, after a failover
or a switchover, the new primary does not contain the replication slot from
the old primary. This can create problems for
the streaming replication clients that were connected to the old
primary and have lost their slot.

CloudNativePG fills this gap by introducing the concept of cluster-managed
replication slots, starting with high availability clusters. This feature
automatically manages physical replication slots for each hot standby replica
in the High Availability cluster, both in the primary and the standby.

In CloudNativePG, we use the terms:

- **Primary HA slot**: a physical replication slot whose lifecycle is entirely
  managed by the current primary of the cluster and whose purpose is to map to
  a specific standby in streaming replication. Such a slot lives on the primary
  only.
- **Standby HA slot**: a physical replication slot for a standby whose
  lifecycle is entirely managed by another standby in the cluster, based on the
  content of the `pg_replication_slots` view in the primary, and updated at regular
  intervals using `pg_replication_slot_advance()`.

This feature, introduced in CloudNativePG 1.18, is now enabled by default and
can be disabled via configuration. For details, please refer to the
["replicationSlots" section in the API reference](api_reference.md#ReplicationSlotsConfiguration).
Here follows a brief description of the main options:

`.spec.replicationSlots.highAvailability.enabled`
: if true, the feature is enabled (`true` is the default - since 1.20)

`.spec.replicationSlots.highAvailability.slotPrefix`
: the prefix that identifies replication slots managed by the operator
  for this feature (default: `_cnpg_`)

`.spec.replicationSlots.updateInterval`
: how often the standby synchronizes the position of the local copy of the
  replication slots with the position on the current primary, expressed in
  seconds (default: 30)

!!! Important
    This capability requires PostgreSQL 11 or higher, as it relies on the
    [`pg_replication_slot_advance()` administration function](https://www.postgresql.org/docs/current/functions-admin.html)
    to directly manipulate the position of a replication slot.

!!! Warning
    In PostgreSQL 11, enabling replication slots if initially disabled, or conversely
    disabling them if initially enabled, will require a rolling update of the
    cluster (due to the presence of the `recovery.conf` file that is only read
    at startup).

Although it is not recommended, if you desire a different behavior, you can
customize the above options.

For example, the following manifest will create a cluster without replication
slots enabled.

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
`pg_replication_slots` view on the primary, and updates its local copy of
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

Replication slots must be carefully monitored in your infrastructure. By default,
we provide the `pg_replication_slots` metric in our Prometheus exporter with
key information such as the name of the slot, the type, whether it is active,
the lag from the primary.

!!! Seealso "Monitoring"
    Please refer to the ["Monitoring" section](monitoring.md) for details on
    how to monitor a CloudNativePG deployment.
