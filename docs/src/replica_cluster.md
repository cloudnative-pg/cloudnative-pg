# Replica clusters

A replica cluster is an independent CloudNativePG `Cluster` resource. It's
main characteristic is that it's in a replica from another Postgres instance,
ideally also managed by CloudNativePG. Normally, a replica cluster is in another
Kubernetes cluster in another region. Replica clusters can be cascading, too,
and they can solely rely on object stores for replication of the data from
the source.

The diagram shows an example of an architecture
that you can implement with replica clusters. See [Architecture](architecture.md#deployments-across-kubernetes-clusters) for more
information about this capability.

![An example of multi-cluster deployment with a primary and a replica cluster](./images/multi-cluster.png)

## Basic concepts

CloudNativePG relies on the foundations of the PostgreSQL replication
framework even when a PostgreSQL cluster is created from an existing one (source)
and kept synchronized through the
[replica cluster](architecture.md#deployments-across-kubernetes-clusters) feature. The source
can be a primary cluster or another replica cluster (cascading replica cluster).

The first step is to bootstrap the replica cluster, choosing one of the
available methods:

- Streaming replication, by way of `pg_basebackup`
- Recovery from a volume snapshot
- Recovery from a Barman Cloud backup in an object store

See [Bootstrap](bootstrap.md#bootstrap-from-another-cluster)
for information on how to clone a PostgreSQL server using either
`pg_basebackup` (streaming) or `recovery` (volume snapshot or object store).

Once the replica cluster's base backup is available, you need to define how
changes are replicated from the origin, using PostgreSQL continuous recovery.
There are two options:

- Use streaming replication between the replica cluster and the source.
  This requires some administrative and security-related
  work to make sure that the network connection between the
  two clusters are correctly setup.
- Use the WAL archive (on an object store) to fetch the WAL files that are
  regularly shipped from the source to the object store and pulled by
  `barman-cloud-wal-restore` in the replica cluster.
<!-- Check this edit -->

All you have to do is define an external cluster.

If the external cluster contains a `barmanObjectStore` section:

- You can use the WAL archive, and CloudNativePG
  sets the `restore_command` in the designated primary instance.
- If you can't take advantage of
  volume snapshots, you can bootstrap the replica cluster from an object store
  using the `recovery` section.

If the external cluster contains a `connectionParameters` section:

- You can bootstrap the replica cluster by way of streaming replication
  using the `pg_basebackup` section.
- CloudNativePG sets the `primary_conninfo`
  option in the designated primary instance, so that a WAL receiver
  process is started to connect to the source cluster and receive data.

The created replica cluster can perform backups in a reserved object store from
the designated primary, enabling symmetric architectures in a distributed
fashion.

You have full flexibility and freedom to decide your preferred
distributed architecture for a PostgreSQL database by choosing:

- A private cloud spanning multiple Kubernetes clusters in different data
  centers
- A public cloud spanning multiple Kubernetes clusters in different
  regions
- A mix of the previous two (hybrid)
- A public cloud spanning multiple Kubernetes clusters in different
  regions and on different cloud service providers

## Setting up a replica cluster

To set up a replica cluster from a source cluster, you need to create a cluster YAML
file and define the following parts accordingly:

- The `externalClusters` section in the replica cluster.
- The bootstrap part for the replica cluster. You can bootstrap either by way of
  streaming using the `pg_basebackup` section or from a volume snapshot
  or an object store using the `recovery` section.
- The continuous recovery part (`.spec.replica`) in the replica cluster. All
  you need to do is enable the replica mode through option `.spec.replica.enabled`
  and set the `externalClusters` name in the option `.spec.replica.source`.

#### Example using pg_basebackup

This example defines a replica cluster using streaming replication in
both bootstrap and continuous recovery. The replica cluster connects to the
source cluster using TLS authentication.

You can check the [sample YAML](samples/cluster-example-replica-streaming.yaml)
in the `samples/` subdirectory.

Note the `bootstrap` and `replica` sections pointing to the source cluster.

```yaml
  bootstrap:
    pg_basebackup:
      source: cluster-example

  replica:
    enabled: true
    source: cluster-example
```

In the `externalClusters` section, remember to use the right namespace for the
host in the `connectionParameters` subsection.
If the replica cluster is in a separate namespace, copy over the `-replication` and `-ca` secrets.

```yaml
  externalClusters:
  - name: <MAIN-CLUSTER>
    connectionParameters:
      host: <MAIN-CLUSTER>-rw.<NAMESPACE>.svc
      user: streaming_replica
      sslmode: verify-full
      dbname: postgres
    sslKey:
      name: <MAIN-CLUSTER>-replication
      key: tls.key
    sslCert:
      name: <MAIN-CLUSTER>-replication
      key: tls.crt
    sslRootCert:
      name: <MAIN-CLUSTER>-ca
      key: ca.crt
```

#### Example using a backup from an object store

This example defines a replica cluster that bootstraps from an object
store using the `recovery` section and continuous recovery using both streaming
replication and the given object store. For streaming replication, the replica
cluster connects to the source cluster using basic authentication.

You can check the [sample YAML](samples/cluster-example-replica-from-backup-simple.yaml)
for it in the `samples/` subdirectory.

Note the `bootstrap` and `replica` sections pointing to the source cluster.

```yaml
  bootstrap:
    recovery:
      source: cluster-example

  replica:
    enabled: true
    source: cluster-example
```

In the `externalClusters` section, take care to use the right namespace in the
`endpointURL` and the `connectionParameters.host`.
Also ensure that the necessary secrets were copied if necessary, and that
a backup of the source cluster was created already.

```yaml
  externalClusters:
  - name: <MAIN-CLUSTER>
    barmanObjectStore:
      destinationPath: s3://backups/
      endpointURL: http://minio:9000
      s3Credentials:
        …
    connectionParameters:
      host: <MAIN-CLUSTER>-rw.default.svc
      user: postgres
      dbname: postgres
    password:
      name: <MAIN-CLUSTER>-superuser
      key: password
```

!!! Note
    To use streaming replication between the source cluster and the replica
    cluster, make sure there's network connectivity between the two
    clusters and that all the necessary secrets that hold passwords or
    certificates are properly created in advance.

#### Example using a volume snapshot

If you use volume snapshots and your storage class provides
snapshots cross-cluster availability, you can leverage that to
bootstrap a replica cluster through a volume snapshot of the
source cluster.

This example defines a replica cluster that bootstraps
from a volume snapshot using the `recovery` section. It uses
streaming replication, by way of basic authentication, and the object
store to fetch the WAL files.

You can check the [sample YAML](samples/cluster-example-replica-from-volume-snapshot.yaml)
for it in the `samples/` subdirectory.

## Promoting the designated primary in the replica cluster

To promote the designated primary to primary,
disable the replica mode in the replica cluster using the option
`.spec.replica.enabled`:

```yaml
 replica:
   enabled: false
   source: cluster-example
```

Once the replica mode is disabled, the replica cluster and the source cluster
become two separate clusters, and the designated primary in the replica
cluster is promoted to that cluster's primary. You can verify the role
change using the cnpg plugin, checking the status of the cluster that was
previously the replica:

```shell
kubectl cnpg -n <cluster-name-space> status cluster-replica-example
```

!!! Note
    Disabling replication is an irreversible operation: once replication is
    disabled and the designated primary is promoted to primary, the
    replica cluster and the source cluster become two independent clusters
    definitively.
