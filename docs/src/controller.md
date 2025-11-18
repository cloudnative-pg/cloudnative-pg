---
id: controller
sidebar_position: 500
title: Custom Pod Controller
---

# Custom Pod Controller
<!-- SPDX-License-Identifier: CC-BY-4.0 -->

Kubernetes uses the
[Controller pattern](https://kubernetes.io/docs/concepts/architecture/controller/)
to align the current cluster state with the desired one.

Stateful applications are usually managed with the
[`StatefulSet`](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/)
controller, which creates and reconciles a set of Pods built from the same
specification, and assigns them a sticky identity.

CloudNativePG implements its own custom controller to manage PostgreSQL
instances, instead of relying on the `StatefulSet` controller.
While bringing more complexity to the implementation, this design choice
provides the operator with more flexibility on how we manage the cluster,
while being transparent on the topology of PostgreSQL clusters.

Like many choices in the design realm, different ones lead to other
compromises. The following sections discuss a few points where we believe
this design choice has made the implementation of CloudNativePG
more reliable, and easier to understand.

## PVC resizing

This is a well known limitation of `StatefulSet`: it does not support resizing
PVCs. This is inconvenient for a database. Resizing volumes requires
convoluted workarounds.

In contrast, CloudNativePG leverages the configured storage class to
manage the underlying PVCs directly, and can handle PVC resizing if
the storage class supports it.

## Primary Instances versus Replicas

The `StatefulSet` controller is designed to create a set of Pods
from just one template. Given that we use one `Pod` per PostgreSQL instance,
we have two kinds of Pods:

1. primary instance (only one)
2. replicas (multiple, optional)

This difference is relevant when deciding the correct deployment strategy to
execute for a given operation.

Some operations should be performed on the replicas first,
and then on the primary, but only after an updated replica is promoted
as the new primary.
For example, when you want to apply a different PostgreSQL image version,
or when you increase configuration parameters like `max_connections` (which are
[treated specially by PostgreSQL because CloudNativePG uses hot standby
replicas](https://www.postgresql.org/docs/current/hot-standby.html)).

While doing that, CloudNativePG considers the PostgreSQL instance's
role - and not just its serial number.

Sometimes the operator needs to follow the opposite process: work on the
primary first and then on the replicas. For example, when you
lower `max_connections`. In that case, CloudNativePG will:

- apply the new setting to the primary instance
- restart it
- apply the new setting on the replicas

The `StatefulSet` controller, being application-independent, can't
incorporate this behavior, which is specific to PostgreSQL's native
replication technology.

## Coherence of PVCs

PostgreSQL instances can be configured to work with multiple PVCs: this is how
WAL storage can be separated from `PGDATA`.

The two data stores need to be coherent from the PostgreSQL point of view,
as they're used simultaneously. If you delete the PVC corresponding to
the WAL storage of an instance, the PVC where `PGDATA` is stored will not be
usable anymore.

This behavior is specific to PostgreSQL and is not implemented in the
`StatefulSet` controller - the latter not being application specific.

After the user dropped a PVC, a `StatefulSet` would just recreate it, leading
to a corrupted PostgreSQL instance.

CloudNativePG would instead classify the remaining PVC as unusable, and
start creating a new pair of PVCs for another instance to join the cluster
correctly.

## Local storage, remote storage, and database size

Sometimes you need to take down a Kubernetes node to do an upgrade.
After the upgrade, depending on your upgrade strategy, the updated node
could go up again, or a new node could replace it.

Supposing the unavailable node was hosting a PostgreSQL instance,
depending on your database size and your cloud infrastructure, you
may prefer to choose one of the following actions:

1. drop the PVC and the Pod residing on the downed node;
   create a new PVC cloning the data from another PVC;
   after that, schedule a Pod for it

2. drop the Pod, schedule the Pod in a different node, and mount
   the PVC from there

3. leave the Pod and the PVC as they are, and wait for the node to
   be back up.

The first solution is practical when your database size permits, allowing
you to immediately bring back the desired number of replicas.

The second solution is only feasible when you're not using the storage of the
local node, and re-mounting the PVC in another host is possible in a reasonable
amount of time (which only you and your organization know).

The third solution is appropriate when the database is big and uses local
node storage for maximum performance and data durability.

The CloudNativePG controller implements all these strategies so that the
user can select the preferred behavior at the cluster level (read the
["Kubernetes upgrade"](kubernetes_upgrade.md) section for details).

Being generic, the `StatefulSet` doesn't allow this level of
customization.
