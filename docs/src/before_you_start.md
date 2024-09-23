# Before You Start

Before we get started, it is essential to go over some terminology that is
specific to Kubernetes and PostgreSQL.

## Kubernetes terminology

[Node](https://kubernetes.io/docs/concepts/architecture/nodes/)
: A *node* is a worker machine in Kubernetes, either virtual or physical, where
  all services necessary to run pods are managed by the control plane node(s).

[Postgres Node](architecture.md#reserving-nodes-for-postgresql-workloads)
: A *Postgres node* is a Kubernetes worker node dedicated to running PostgreSQL
  workloads. This is achieved by applying the `node-role.kubernetes.io` label and
  taint, as [proposed by CloudNativePG](architecture.md#reserving-nodes-for-postgresql-workloads).
  It is also referred to as a `postgres` node.

[Pod](https://kubernetes.io/docs/concepts/workloads/pods/pod/)
: A *pod* is the smallest computing unit that can be deployed in a Kubernetes
  cluster and is composed of one or more containers that share network and
  storage.

[Service](https://kubernetes.io/docs/concepts/services-networking/service/)
: A *service* is an abstraction that exposes as a network service an
  application that runs on a group of pods and standardizes important features
  such as service discovery across applications, load balancing, failover, and so
  on.

[Secret](https://kubernetes.io/docs/concepts/configuration/secret/)
: A *secret* is an object that is designed to store small amounts of sensitive
  data such as passwords, access keys, or tokens, and use them in pods.

[Storage Class](https://kubernetes.io/docs/concepts/storage/storage-classes/)
: A *storage class* allows an administrator to define the classes of storage in
  a cluster, including provisioner (such as AWS EBS), reclaim policies, mount
  options, volume expansion, and so on.

[Persistent Volume](https://kubernetes.io/docs/concepts/storage/persistent-volumes/)
: A *persistent volume* (PV) is a resource in a Kubernetes cluster that
  represents storage that has been either manually provisioned by an
  administrator or dynamically provisioned by a *storage class* controller. A PV
  is associated with a pod using a *persistent volume claim* and its lifecycle is
  independent of any pod that uses it. Normally, a PV is a network volume,
  especially in the public cloud. A [*local persistent volume*
  (LPV)](https://kubernetes.io/docs/concepts/storage/volumes/#local) is a
  persistent volume that exists only on the particular node where the pod that
  uses it is running.

[Persistent Volume Claim](https://kubernetes.io/docs/concepts/storage/persistent-volumes/#persistentvolumeclaims)
: A *persistent volume claim* (PVC) represents a request for storage, which
  might include size, access mode, or a particular storage class. Similar to how
  a pod consumes node resources, a PVC consumes the resources of a PV.

[Namespace](https://kubernetes.io/docs/concepts/overview/working-with-objects/namespaces/)
: A *namespace* is a logical and isolated subset of a Kubernetes cluster and
  can be seen as a *virtual cluster* within the wider physical cluster.
  Namespaces allow administrators to create separated environments based on
  projects, departments, teams, and so on.

[RBAC](https://kubernetes.io/docs/reference/access-authn-authz/rbac/)
: *Role Based Access Control* (RBAC), also known as *role-based security*, is a
  method used in computer systems security to restrict access to the network and
  resources of a system to authorized users only. Kubernetes has a native API to
  control roles at the namespace and cluster level and associate them with
  specific resources and individuals.

[CRD](https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/custom-resources/)
: A *custom resource definition* (CRD) is an extension of the Kubernetes API
  and allows developers to create new data types and objects, *called custom
  resources*.

[Operator](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/)
: An *operator* is a custom resource that automates those steps that are
  normally performed by a human operator when managing one or more applications
  or given services. An operator assists Kubernetes in making sure that the
  resource's defined state always matches the observed one.

[`kubectl`](https://kubernetes.io/docs/reference/kubectl/overview/)
: `kubectl` is the command-line tool used to manage a Kubernetes cluster.

CloudNativePG requires a Kubernetes version supported by the community. Please refer to the
["Supported releases"](supported_releases.md) page for details.

## PostgreSQL terminology

Instance
: A Postgres server process running and listening on a pair "IP address(es)"
  and "TCP port" (usually 5432).

Primary
: A PostgreSQL instance that can accept both read and write operations.

Replica
: A PostgreSQL instance replicating from the only primary instance in a
  cluster and is kept updated by reading a stream of Write-Ahead Log (WAL)
  records. A replica is also known as *standby* or *secondary* server. PostgreSQL
  relies on physical streaming replication (async/sync) and file-based log
  shipping (async).

Hot Standby
: PostgreSQL feature that allows a *replica* to accept read-only workloads.

Cluster
: To be intended as High Availability (HA) Cluster: a set of PostgreSQL
  instances made up by a single primary and an optional arbitrary number of
  replicas.

Replica Cluster
: A CloudNativePG `Cluster` that is in continuous recovery mode from a selected
  PostgreSQL cluster, normally residing outside the Kubernetes cluster. It is a
  feature that enables multi-cluster deployments in private, public, hybrid, and
  multi-cloud contexts.

Designated Primary
: A PostgreSQL standby instance in a replica cluster that is in continuous
  recovery from another PostgreSQL cluster and that is designated to become
  primary in case the replica cluster becomes primary.

Superuser
: In PostgreSQL a *superuser* is any role with both `LOGIN` and `SUPERUSER`
  privileges. For security reasons, CloudNativePG performs administrative tasks
  by connecting to the `postgres` database as the `postgres` user via `peer`
  authentication over the local Unix Domain Socket.

[WAL](https://www.postgresql.org/docs/current/wal-intro.html)
: Write-Ahead Logging (WAL) is a standard method for ensuring data integrity in
  database management systems.

PVC group
: A PVC group in CloudNativePG's terminology is a group of related PVCs
  belonging to the same PostgreSQL instance, namely the main volume containing
  the PGDATA (`storage`) and the volume for WALs (`walStorage`).


## Cloud terminology

Region
: A *region* in the Cloud is an isolated and independent geographic area
  organized in *availability zones*. Zones within a region have very little
  round-trip network latency.

Zone
: An *availability zone* in the Cloud (also known as *zone*) is an area in a
  region where resources can be deployed. Usually, an availability zone
  corresponds to a data center or an isolated building of the same data center.

## What to do next

Now that you have familiarized with the terminology, you can decide to
[test CloudNativePG on your laptop using a local cluster](quickstart.md) before
deploying the operator in your selected cloud environment.
