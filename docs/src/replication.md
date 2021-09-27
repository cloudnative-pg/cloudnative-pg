# Replication

Physical replication is one of the strengths of PostgreSQL and one of the
reasons why some of the world's largest organizations in the world have chosen
it for the management of their data in business continuity contexts.
Primarily used to achieve high availability, physical replication also allows
scale-out of read-only workloads and offloading some work from the primary.

## Application-level replication

Having contributed throughout the years to the replication feature in PostgreSQL,
we have decided to build high availability in Cloud Native PostgreSQL on top of
the native physical replication technology and integrate it
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

## Replication within a PostgreSQL cluster

### Streaming replication support

At the moment, Cloud Native PostgreSQL natively and transparently manages
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

!!! Note
    Due to a `pg_rewind` requirement, in PostgreSQL 10 the `streaming_replica`
    user is created with `SUPERUSER` privileges.

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
    For details on how Cloud Native PostgreSQL manages certificates, please refer
    to the ["Certificates" section](certificates.md#client-streaming_replica-certificate)
    in the documentation.


### Continuous backup integration

In case continuous backup is configured in the cluster, Cloud Native PostgreSQL
transparently configures replicas to take advantage of `restore_command` when
in continuous recovery. As a result, PostgreSQL is able to use the WAL archive
as a fallback option everytime pulling WALs via streaming replication fails.

### Synchronous replication

Cloud Native PostgreSQL supports configuration of **quorum-based synchronous
streaming replication** via two configuration options called `minSyncReplicas`
and `maxSyncReplicas` which are the minimum and maximum number of expected
synchronous standby replicas available at any time.
For self-healing purposes, the operator always weights these two values with
the available number of replicas in order to determine the quorum.

Synchronous replication is disabled by default (`minSyncReplicas` and
`maxSyncReplicas` are not defined).
In case both `minSyncReplicas` and `maxSyncReplicas` are set, Cloud Native
PostgreSQL automatically updates the `synchronous_standby_names` option in
PostgreSQL to the following value:

```
ANY q (pod1, pod2, ...)
```

Where:

- `q` is an integer automatically calculated by the operator to be:  
  `1 <= minSyncReplicas <= q <= maxSyncReplicas <= readyReplicas`
- `pod1, pod2, ...` is the list of all PostgreSQL pods in the cluster

!!! Warning
    To provide self-healing capabilities, the operator has the power
    to ignore `minSyncReplicas` in case such value is higher than the currently
    available number of replicas. Synchronous replication is automatically disabled
    when `readyReplicas` is `0`.

As stated in the
[PostgreSQL documentation](https://www.postgresql.org/docs/current/warm-standby.html#SYNCHRONOUS-REPLICATION),
the *method `ANY` specifies a quorum-based synchronous replication and makes
transaction commits wait until their WAL records are replicated to at least the
requested number of synchronous standbys in the list*.

!!! Important
    Even though the operator privileges self-healing over enforcement of
    synchronous replication settings, our recommendation is to plan for
    synchronous replication only in clusters with 3+ instances or,
    more generally, when `maxSyncReplicas < (instances - 1)`.

## Replication from an external PostgreSQL cluster

Cloud Native PostgreSQL relies on the foundations of the PostgreSQL replication
framework even when a PostgreSQL cluster is created from an existing one (source)
and kept synchronized through the
[replica cluster](architecture.md#multi-cluster-deployments) feature. The source
can be a primary cluster or another replica cluster (cascading replica cluster).

The available options in terms of replication, both at bootstrap and continuous
recovery level, are:

- use streaming replication between the replica cluster and the source
  (this will certainly require some administrative and security related
  work to be done to make sure that the network connection between the
  two clusters is correctly setup)
- use a Barman Cloud object store for recovery of the base backups and
  the WAL files that are regularly shipped from the source to the object
  store and pulled by `barman-cloud-wal-restore` in the replica cluster
- any of the two

All you have to do is actually define an external cluster.
Please refer to the ["Bootstrap" section](bootstrap.md#bootstrap-from-another-cluster)
for information on how to clone a PostgreSQL server using either
`pg_basebackup` (streaming) or `recovery` (object store).

If the external cluster contains a `barmanObjectStore` section:

- you'll be able to boostrap the replica cluster from an object store
  using the `recovery` section
- Cloud Native PostgreSQL will automatically set the `restore_command`
  in the designated primary instance

If the external cluster contains a `connectionParameters` section:

- you'll be able to boostrap the replica cluster via streaming replication
  using the `pg_basebackup` section
- Cloud Native PostgreSQL will automatically set the `primary_conninfo`
  option in the designated primary instance, so that a WAL receiver
  process is started to connect to the source cluster and receive data

The created replica cluster can perform backups in a reserved object store from
the designated primary, enabling symmetric architectures in a distributed
fashion.

You have full flexibility and freedom to decide your favourite
distributed architecture for a PostgreSQL database, by choosing:

- a private cloud spanning over multiple Kubernetes clusters in different data
  centers
- a public cloud spanning over multiple Kubernetes clusters in different
  regions
- a mix of the previous two (hybrid)
- a public cloud spanning over multiple Kubernetes clusters in different
  regions and on different Cloud Service Providers

