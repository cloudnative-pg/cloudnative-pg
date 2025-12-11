---
id: replication
sidebar_position: 160
title: Replication
---

# Replication
<!-- SPDX-License-Identifier: CC-BY-4.0 -->

Physical replication is one of the strengths of PostgreSQL and one of the
reasons why some of the largest organizations in the world have chosen it for
the management of their data in business continuity contexts. Primarily used to
achieve high availability, physical replication also allows scale-out of
read-only workloads and offloading of some work from the primary.

:::info[Important]
    This section is about replication within the same `Cluster` resource
    managed in the same Kubernetes cluster. For information about how to
    replicate with another Postgres `Cluster` resource, even across different
    Kubernetes clusters, please refer to the
    ["Replica clusters"](replica_cluster.md) section.
:::

## Application-level replication

Having contributed throughout the years to the replication feature in
PostgreSQL, we have decided to build high availability in CloudNativePG on top
of the native physical replication technology, and integrate it directly in the
Kubernetes API.

In Kubernetes terms, this is referred to as **application-level replication**,
in contrast with *storage-level replication*.

## A very mature technology

PostgreSQL has a very robust and mature native framework for replicating data
from the primary instance to one or more replicas, built around the concept of
transactional changes continuously stored in the WAL (Write Ahead Log).

Started as the evolution of crash recovery and point in time recovery
technologies, physical replication was first introduced in PostgreSQL 8.2
(2006) through WAL shipping from the primary to a warm standby in continuous
recovery.

PostgreSQL 9.0 (2010) introduced WAL streaming and read-only replicas through
*hot standby*. In 2011, PostgreSQL 9.1 brought synchronous replication at the
transaction level, supporting [RPO](before_you_start.md#rpo)=0 clusters. Cascading
replication was added in PostgreSQL 9.2 (2012). The foundations for
[logical replication](logical_replication.md) were established in PostgreSQL
9.4 (2014), and version 10 (2017) introduced native support for the
publisher/subscriber pattern to replicate data from an origin to a destination. The
table below summarizes these milestones.

| Version | Year | Feature                                                               |
|:-------:|:----:|-----------------------------------------------------------------------|
| 8.2     | 2006 | Warm Standby with WAL shipping                                        |
| 9.0     | 2010 | Hot Standby and physical streaming replication                        |
| 9.1     | 2011 | Synchronous replication (priority-based)                              |
| 9.2     | 2012 | Cascading replication                                                 |
| 9.4     | 2014 | Foundations of logical replication                                    |
| 10      | 2017 | Logical publisher/subscriber and quorum-based synchronous replication |

This table highlights key PostgreSQL replication features and their respective
versions.

## Streaming replication support

At the moment, CloudNativePG natively and transparently manages physical
streaming replicas within a cluster in a declarative way, based on the number of
provided `instances` in the `spec`:

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
authentication for the `streaming_replica` user - as highlighted by the
following excerpt taken from `pg_hba.conf`:

```
# Require client certificate authentication for the streaming_replica user
hostssl postgres streaming_replica all cert
hostssl replication streaming_replica all cert
```

:::note[Certificates]
    For details on how CloudNativePG manages certificates, please refer
    to the ["Certificates" section](certificates.md#client-streaming_replica-certificate)
    in the documentation.
:::

If configured, the operator manages replication slots for all the replicas in the
HA cluster, ensuring that WAL files required by each standby are retained on
the primary's storage, even after a failover or switchover.

:::note[Replication slots for High Availability]
    For details on how CloudNativePG automatically manages replication slots for the
    High Availability replicas, please refer to the
    ["Replication slots for High Availability" section](#replication-slots-for-high-availability)
    below.
:::

### Continuous backup integration

In case continuous backup is configured in the cluster, CloudNativePG
transparently configures replicas to take advantage of `restore_command` when in
continuous recovery. As a result, PostgreSQL can use the WAL archive as a
fallback option whenever pulling WALs via streaming replication fails.

## Synchronous Replication

CloudNativePG supports both
[quorum-based and priority-based synchronous replication for PostgreSQL](https://www.postgresql.org/docs/current/warm-standby.html#SYNCHRONOUS-REPLICATION).

:::warning
    By default, synchronous replication pauses write operations if the required
    number of standby nodes for WAL replication during transaction commits is
    unavailable. This behavior prioritizes data durability and aligns with
    PostgreSQL DBA best practices. However, if self-healing is a higher priority
    than strict data durability in your setup, this setting can be adjusted. For
    details on managing this behavior, refer to the [Data Durability and Synchronous Replication](#data-durability-and-synchronous-replication)
    section.
:::

Direct configuration of the `synchronous_standby_names` option is not
permitted. However, CloudNativePG automatically populates this option with the
names of local pods, while also allowing customization to extend synchronous
replication beyond the `Cluster` resource.
This can be achieved through the
[`.spec.postgresql.synchronous` stanza](cloudnative-pg.v1.md#postgresql-cnpg-io-v1-SynchronousReplicaConfiguration).

Synchronous replication is disabled by default (the `synchronous` stanza is not
defined). When defined, two options are mandatory:

- `method`: either `any` (quorum) or `first` (priority)
- `number`: the number of synchronous standby servers that transactions must
  wait for responses from

### Quorum-based Synchronous Replication

In PostgreSQL, quorum-based synchronous replication ensures that transaction
commits wait until their WAL records are replicated to a specified number of
standbys. To enable this, set the `method` to `any`.

This replication method is the most common setup for a CloudNativePG cluster.

#### Example

The example below, based on a typical `cluster-example` configuration with
three instances, sets up quorum-based synchronous replication with at least one
instance:

```yaml
postgresql:
  synchronous:
    method: any
    number: 1
```

With this configuration, CloudNativePG automatically sets the content of
`synchronous_standby_names` as follows:

```console
ANY 1 (cluster-example-2, cluster-example-3, cluster-example-1)
```

#### Migrating from Deprecated Synchronous Replication Implementation

This section outlines how to migrate from the deprecated quorum-based
synchronous replication format to the newer, more robust implementation in
CloudNativePG.

Given the following manifest:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: angus
spec:
  instances: 3
  minSyncReplicas: 1
  maxSyncReplicas: 1

  storage:
    size: 1Gi
```

You can update it to the new format as follows:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: angus
spec:
  instances: 3

  storage:
    size: 1Gi

  postgresql:
    synchronous:
      method: any
      number: 1
      dataDurability: required
```

To prioritize self-healing over strict data durability, set `dataDurability`
to `preferred` instead.

### Priority-based Synchronous Replication

PostgreSQL's priority-based synchronous replication makes transaction commits
wait until their WAL records are replicated to the requested number of
synchronous standbys chosen based on their priorities. Standbys listed earlier
in the `synchronous_standby_names` option are given higher priority and
considered synchronous. If a current synchronous standby disconnects, it is
immediately replaced by the next-highest-priority standby. To use this method,
set `method` to `first`.

:::info[Important]
    Currently, this method is most useful when extending
    synchronous replication beyond the current cluster using the
    `maxStandbyNamesFromCluster`, `standbyNamesPre`, and `standbyNamesPost`
    options explained below.
:::

### Controlling `synchronous_standby_names` Content

By default, CloudNativePG populates `synchronous_standby_names` with the names
of local pods in a `Cluster` resource, ensuring synchronous replication within
the PostgreSQL cluster. You can customize the content of
`synchronous_standby_names` based on your requirements and replication method
(quorum or priority) using the following optional parameters in the
`.spec.postgresql.synchronous` stanza:

- `maxStandbyNamesFromCluster`: the maximum number of pod names from the local
  `Cluster` object that can be automatically included in the
  `synchronous_standby_names` option in PostgreSQL.
- `standbyNamesPre`: a list of standby names (specifically `application_name`)
  to be prepended to the list of local pod names automatically listed by the
  operator.
- `standbyNamesPost`: a list of standby names (specifically `application_name`)
  to be appended to the list of local pod names automatically listed by the
  operator.

:::warning
    You are responsible for ensuring the correct names in `standbyNamesPre` and
    `standbyNamesPost`. CloudNativePG expects that you manage any standby with
    an `application_name` listed here, ensuring their high availability.
    Incorrect entries can jeopardize your PostgreSQL database uptime.
:::

#### Examples

Here are some examples, all based on a `cluster-example` with three instances:

If you set:

```yaml
postgresql:
  synchronous:
    method: any
    number: 1
    maxStandbyNamesFromCluster: 1
    standbyNamesPre:
      - angus
```

The content of `synchronous_standby_names` will be:

```console
ANY 1 (angus, cluster-example-2)
```

If you set:

```yaml
postgresql:
  synchronous:
    method: any
    number: 1
    maxStandbyNamesFromCluster: 0
    standbyNamesPre:
      - angus
      - malcolm
```

The content of `synchronous_standby_names` will be:

```console
ANY 1 (angus, malcolm)
```

If you set:

```yaml
postgresql:
  synchronous:
    method: first
    number: 2
    maxStandbyNamesFromCluster: 1
    standbyNamesPre:
      - angus
    standbyNamesPost:
      - malcolm
```

The `synchronous_standby_names` option will look like:

```console
FIRST 2 (angus, cluster-example-2, malcolm)
```

### Data Durability and Synchronous Replication

The `dataDurability` option in the `.spec.postgresql.synchronous` stanza
controls the trade-off between data safety and availability for synchronous
replication. It can be set to `required` or `preferred`, with the default being
`required` if not specified.

:::info[Important]
    `preferred` can only be used when `standbyNamesPre` and `standbyNamesPost`
    are unset.
:::

#### Required Data Durability

When `dataDurability` is set to `required`, PostgreSQL only considers
transactions committed once WAL (Write-Ahead Log) records have been replicated
to the specified number of synchronous standbys. This setting prioritizes data
safety over availability, meaning write operations will pause if the required
number of synchronous standbys is unavailable. This ensures zero data loss
(RPO=0) but may reduce database availability during network disruptions or
standby failures.

Synchronous standbys are selected in this priority order:

1. Healthy instances
2. Unhealthy instances
3. Primary

The list is then truncated based on `maxStandbyNamesFromCluster` if this value
is set, prioritizing healthy instances and ensuring `synchronous_standby_names`
is populated.

##### Example

Consider the following example:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: foo
spec:
  instances: 3
  postgresql:
    synchronous:
      method: any
      number: 1
      dataDurability: required
```

1. Initial state. The content of `synchronous_standby_names` is:

    ```
    ANY 1 ("foo-2","foo-3","foo-1")
    ```

2. `foo-2` becomes unavailable. It gets pushed back in priority:

    ```
    ANY 1 ("foo-3","foo-2","foo-1")
    ```

3. `foo-3` also becomes unavailable. The list contains no healthy standbys:

    ```
    ANY 1 ("foo-2","foo-3","foo-1")
    ```

    At this point no write operations will be allowed until at least one of the
    standbys is available again.

4. When the standbys are available again, `synchronous_standby_names` will
   be back to the initial state.

#### Preferred Data Durability

When `dataDurability` is set to `preferred`, the required number of synchronous
instances adjusts based on the number of available standbys. PostgreSQL will
attempt to replicate WAL records to the designated number of synchronous
standbys, but write operations will continue even if fewer than the requested
number of standbys are available.

:::info[Important]
    Make sure you have a clear understanding of what *ready/available* means
    for a replica and set your expectations accordingly. By default, a replica is
    considered ready when it has successfully connected to the source at least
    once. However, CloudNativePG allows you to configure startup and readiness
    probes for replicas based on maximum lag. For more details, please refer to
    the ["Postgres instance manager" section](instance_manager.md).
:::

This setting balances data safety with availability, enabling applications to
continue writing during temporary standby unavailability—hence, it’s also known
as *self-healing mode*.

:::warning
    This mode may result in data loss if all standbys become unavailable.
:::

With `preferred` data durability, **only healthy replicas** are included in
`synchronous_standby_names`.

##### Example

Consider the following example. For demonstration, we’ll use a cluster named
`bar` with 5 instances and 2 synchronous standbys:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: bar
spec:
  instances: 5
  postgresql:
    synchronous:
      method: any
      number: 2
      dataDurability: preferred
```

1. Initial state. The content of `synchronous_standby_names` is:

    ```
    ANY 2 ("bar-2","bar-3", "bar-4", "bar-5")
    ```

2. `bar-2` and `bar-3` become unavailable. They are removed from the list:

    ```
    ANY 2 ("bar-4", "bar-5")
    ```

3. `bar-4` also becomes unavailable. It gets removed from the list. Since the
   number of available standbys is less than the requested number, the requested
   amount gets reduced:

    ```
    ANY 1 ("bar-5")
    ```

4. `bar-5` also becomes unavailable. `synchronous_standby_names` becomes empty,
   disabling synchronous replication completely. Write operations will continue,
   but with the risk of potential data loss in case of a primary failure.
5. When the replicas are back, `synchronous_standby_names` will be back to
   the initial state.

## Synchronous Replication (Deprecated)

:::warning
    Prior to CloudNativePG 1.24, only the quorum-based synchronous replication
    implementation was supported. Although this method is now deprecated, it
    will not be removed anytime soon.
    The new method prioritizes data durability over self-healing and offers more
    robust features, including priority-based synchronous replication and full
    control over the `synchronous_standby_names` option.
    It is recommended to gradually migrate to the new configuration method for
    synchronous replication, as explained in the previous paragraph.
:::

:::info[Important]
    The deprecated method and the new method are mutually exclusive.
:::

CloudNativePG supports the configuration of **quorum-based synchronous
streaming replication** via two configuration options called `minSyncReplicas`
and `maxSyncReplicas`, which are the minimum and the maximum number of expected
synchronous standby replicas available at any time.
For self-healing purposes, the operator always compares these two values with
the number of available replicas to determine the quorum.

:::info[Important]
    By default, synchronous replication selects among all the available
    replicas indistinctively. You can limit on which nodes your synchronous
    replicas can be scheduled, by working on node labels through the
    `syncReplicaElectionConstraint` option as described in the next section.
:::

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
  `1 ≤ minSyncReplicas ≤ q ≤ maxSyncReplicas ≤ readyReplicas`
- `pod1, pod2, ...` is the list of all PostgreSQL pods in the cluster

:::warning
    To provide self-healing capabilities, the operator can ignore
    `minSyncReplicas` if such value is higher than the currently available
    number of replicas. Synchronous replication is automatically disabled
    when `readyReplicas` is `0`.
:::

As stated in the
[PostgreSQL documentation](https://www.postgresql.org/docs/current/warm-standby.html#SYNCHRONOUS-REPLICATION),
the *method `ANY` specifies a quorum-based synchronous replication and makes
transaction commits wait until their WAL records are replicated to at least the
requested number of synchronous standbys in the list*.

:::info[Important]
    Even though the operator chooses self-healing over enforcement of
    synchronous replication settings, our recommendation is to plan for
    synchronous replication only in clusters with 3+ instances or,
    more generally, when `maxSyncReplicas < (instances - 1)`.
:::

### Select nodes for synchronous replication

CloudNativePG enables you to select which PostgreSQL instances are eligible to
participate in a quorum-based synchronous replication set through anti-affinity
rules based on the node labels where the PVC holding the PGDATA and the
Postgres pod are.

:::note[Scheduling]
    For more information on the general pod affinity and anti-affinity rules,
    please check the ["Scheduling" section](scheduling.md).
:::

:::warning
    The `.spec.postgresql.syncReplicaElectionConstraint` option only applies to the
    legacy implementation of synchronous replication
    (see ["Synchronous Replication (Deprecated)"](replication.md#synchronous-replication-deprecated)).
:::

As an example use-case for this feature: in a cluster with a single sync
replica, we would be able to ensure the sync replica will be in a different
availability zone from the primary instance, usually identified by
the `topology.kubernetes.io/zone`
[label on a node](https://kubernetes.io/docs/reference/labels-annotations-taints/#topologykubernetesiozone).
This would increase the robustness of the cluster in case of an outage in a
single availability zone, especially in terms of recovery point objective
([RPO](before_you_start.md#rpo)).

The idea of anti-affinity is to ensure that sync replicas that participate in
the quorum are chosen from pods running on nodes that have different values for
the selected labels (in this case, the availability zone label) then the node
where the primary is currently in execution. If no node matches such criteria,
the replicas are eligible for synchronous replication.

:::info[Important]
    The self-healing enforcement still applies while defining additional
    constraints for synchronous replica election
    (see ["Synchronous replication"](replication.md#synchronous-replication)).
:::

The example below shows how this can be done through the
`syncReplicaElectionConstraint` section within `.spec.postgresql`.
`nodeLabelsAntiAffinity` allows you to specify those node labels that need to be
evaluated to make sure that synchronous replication will be dynamically
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

## Replication slots

[Replication slots](https://www.postgresql.org/docs/current/warm-standby.html#STREAMING-REPLICATION-SLOTS)
are a native PostgreSQL feature introduced in 9.4 that provides an automated way
to ensure that the primary does not remove WAL segments until all the attached
streaming replication clients have received them, and that the primary does not
remove rows which could cause a recovery conflict even when the standby is (
temporarily) disconnected.

A replication slot exists solely on the instance that created it, and PostgreSQL
does not replicate it on the standby servers. As a result, after a failover or a
switchover, the new primary does not contain the replication slot from the old
primary. This can create problems for the streaming replication clients that
were connected to the old primary and have lost their slot.

CloudNativePG provides a turn-key solution to synchronize the content of
physical replication slots from the primary to each standby, addressing two use
cases:

- the replication slots automatically created for the High Availability of the
  Postgres cluster (
  see ["Replication slots for High Availability" below](#replication-slots-for-high-availability)
  for details)
- [user-defined replication slots](#user-defined-replication-slots) created on
  the primary

### Replication slots for High Availability

CloudNativePG fills this gap by introducing the concept of cluster-managed
replication slots, starting with high availability clusters. This feature
automatically manages physical replication slots for each hot standby replica in
the High Availability cluster, both in the primary and the standby.

In CloudNativePG, we use the terms:

- **Primary HA slot**: a physical replication slot whose lifecycle is entirely
  managed by the current primary of the cluster and whose purpose is to map to a
  specific standby in streaming replication. Such a slot lives on the primary
  only.
- **Standby HA slot**: a physical replication slot for a standby whose lifecycle
  is entirely managed by another standby in the cluster, based on the content of
  the `pg_replication_slots` view in the primary, and updated at regular
  intervals using `pg_replication_slot_advance()`.

This feature is enabled by default and can be disabled via configuration. For
details, please refer to the
["replicationSlots" section in the API reference](cloudnative-pg.v1.md#postgresql-cnpg-io-v1-ReplicationSlotsConfiguration).
Here follows a brief description of the main options:

`.spec.replicationSlots.highAvailability.enabled`
: if `true`, the feature is enabled (`true` is the default)

`.spec.replicationSlots.highAvailability.slotPrefix`
: the prefix that identifies replication slots managed by the operator for this
feature (default: `_cnpg_`)

`.spec.replicationSlots.updateInterval`
: how often the standby synchronizes the position of the local copy of the
replication slots with the position on the current primary, expressed in
seconds (default: 30)

Although it is not recommended, if you desire a different behavior, you can
customize the above options.

For example, the following manifest will create a cluster with replication
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

### User-Defined Replication slots

Although CloudNativePG doesn't support a way to declaratively define physical
replication slots, you can still [create your own slots via SQL](https://www.postgresql.org/docs/current/functions-admin.html#FUNCTIONS-REPLICATION).

:::note[Information]
    At the moment, we don't have any plans to manage replication slots
    in a declarative way, but it might change depending on the feedback
    we receive from users. The reason is that replication slots exist
    for a specific purpose and each should be managed by a specific application
    the oversees the entire lifecycle of the slot on the primary.
:::

CloudNativePG can manage the synchronization of any user managed physical
replication slots between the primary and standbys, similarly to what it does
for the HA replication slots explained above (the only difference is that you
need to create the replication slot).

This feature is enabled by default (meaning that any replication slot is
synchronized), but you can disable it or further customize its behavior (for
example by excluding some slots using regular expressions) through the
`synchronizeReplicas` stanza. For example:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-example
spec:
  instances: 3
  replicationSlots:
    synchronizeReplicas:
      enabled: true
      excludePatterns:
      - "^foo"
```

For details, please refer to the
["replicationSlots" section in the API reference](cloudnative-pg.v1.md#postgresql-cnpg-io-v1-ReplicationSlotsConfiguration).
Here follows a brief description of the main options:

`.spec.replicationSlots.synchronizeReplicas.enabled`
: When true or not specified, every user-defined replication slot on the
  primary is synchronized on each standby. If changed to false, the operator will
  remove any replication slot previously created by itself on each standby.

`.spec.replicationSlots.synchronizeReplicas.excludePatterns`
: A list of regular expression patterns to match the names of user-defined
  replication slots to be excluded from synchronization. This can be useful to
  exclude specific slots based on naming conventions.

:::warning
    Users utilizing this feature should carefully monitor user-defined replication
    slots to ensure they align with their operational requirements and do not
    interfere with the failover process.
:::

### Synchronization frequency

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
    updateInterval: 300

  storage:
    size: 1Gi
```

### Capping the WAL size retained for replication slots

When replication slots is enabled, you might end up running out of disk space
due to PostgreSQL trying to retain WAL files requested by a replication slot.
This might happen due to a standby that is (temporarily?) down, or lagging, or
simply an orphan replication slot.

Starting with PostgreSQL 13, you can take advantage of the
[`max_slot_wal_keep_size`](https://www.postgresql.org/docs/current/runtime-config-replication.html#GUC-MAX-SLOT-WAL-KEEP-SIZE)
configuration option controlling the maximum size of WAL files that replication
slots are allowed to retain in the `pg_wal` directory at checkpoint time. By
default, in PostgreSQL `max_slot_wal_keep_size` is set to `-1`, meaning that
replication slots may retain an unlimited amount of WAL files. As a result, our
recommendation is to explicitly set `max_slot_wal_keep_size`
when replication slots support is enabled. For example:

```ini
  # ...
  postgresql:
    parameters:
      max_slot_wal_keep_size: "10GB"
  # ...
```

### Monitoring replication slots

Replication slots must be carefully monitored in your infrastructure. By default,
we provide the `pg_replication_slots` metric in our Prometheus exporter with
key information such as the name of the slot, the type, whether it is active,
the lag from the primary.

:::note[Monitoring]
    Please refer to the ["Monitoring" section](monitoring.md) for details on
    how to monitor a CloudNativePG deployment.
:::
