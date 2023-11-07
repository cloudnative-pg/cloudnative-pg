# Custom pod controller

Kubernetes uses the
[controller pattern](https://kubernetes.io/docs/concepts/architecture/controller/)
to align the current cluster state with the desired one.

Stateful applications are usually managed with the
[`StatefulSet`](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/)
controller, which creates and reconciles a set of pods built from the same
specification and assigns them a sticky identity.

Instead of relying on the `StatefulSet` controller, CloudNativePG implements 
its own custom controller to manage PostgreSQL instances.
While bringing more complexity to the implementation, this design choice
provides the operator with more flexibility on how we manage the cluster,
while being transparent on the topology of PostgreSQL clusters.

Like many choices in the design realm, different ones lead to other
compromises. We believe
this design choice has made the implementation of CloudNativePG
more reliable and easier to understand for the reasons that follow.

## PVC resizing

PVC resizing is a well-known limitation of `StatefulSet`, which doesn't support resizing
PVCs. This is inconvenient for a database. Resizing volumes requires
convoluted workarounds.

By contrast, CloudNativePG leverages the configured storage class to
manage the underlying PVCs directly and can handle PVC resizing if
the storage class supports it.

## Primary instances versus replicas

The `StatefulSet` controller is designed to create a set of pods
from just one template. Given that we use one `Pod` per PostgreSQL instance,
we have two kinds of pods:

- Primary instance (only one)
- Replicas (multiple, optional)

This difference is relevant when deciding the correct deployment strategy to
execute for a given operation.

Some operations must be performed on the replicas first
and then on the primary after an updated replica is promoted
as the new primary.
Examples include applying a different PostgreSQL image version
or when you increase configuration parameters like `max_connections` (which are
[treated specially by PostgreSQL because CloudNativePG uses hot standby
replicas](https://www.postgresql.org/docs/current/hot-standby.html)).

While doing that, CloudNativePG considers the PostgreSQL instance's
role and not just its serial number.

Sometimes the operator needs to follow the opposite process: work on the
primary first and then on the replicas. An example is when you
lower `max_connections`. In that case, CloudNativePG:

- Applies the new setting to the primary instance
- Restarts it
- Applies the new setting on the replicas

The `StatefulSet` controller, being application independent, can't
incorporate this behavior, which is specific to PostgreSQL's native
replication technology.

## Coherence of PVCs

PostgreSQL instances can be configured to work with multiple PVCs, which is how
WAL storage can be separated from `PGDATA`.

The two data stores need to be coherent from the PostgreSQL point of view,
as they're used simultaneously. If you delete the PVC corresponding to
the WAL storage of an instance, the PVC where `PGDATA` is stored isn't
usable anymore.

This behavior is specific to PostgreSQL and isn't implemented in the
`StatefulSet` controller, as the latter isn't application specific.

After the user drops a PVC, a `StatefulSet` recreates it, leading
to a corrupted PostgreSQL instance.

CloudNativePG instead classifies the remaining PVC as unusable and
starts creating a new pair of PVCs for another instance to join the cluster
correctly.

## Local storage, remote storage, and database size

Sometimes you need to take down a Kubernetes node to do an upgrade.
After the upgrade, depending on your upgrade strategy, the updated node
might go up again, or a new node might replace it.

Suppose the unavailable node was hosting a PostgreSQL instance.
Depending on your database size and your cloud infrastructure, you
might prefer to choose one of the following actions:

-  Drop the PVC and the pod residing on the downed node.
   Create a new PVC cloning the data from another PVC and then
   schedule a pod for it.

-  Drop the pod, schedule the pod in a different node, and mount
   the PVC from there.

-  Leave the pod and the PVC as they are and wait for the node to
   be back up.

Dropping the PVC and the pod on the downed node is practical when your database size permits, allowing
you to immediately bring back the desired number of replicas.

Dropping the pod is feasible only when you're not using the storage of the
local node and remounting the PVC in another host is possible in a reasonable
amount of time (which only you and your organization know).

Leaving the pod and the PVC is appropriate when the database is big and uses local
node storage for maximum performance and data durability.

The CloudNativePG controller implements all these strategies so that the
user can select the preferred behavior at the cluster level. (See
[Kubernetes upgrade](kubernetes_upgrade.md) for details.)

Being generic, the `StatefulSet` doesn't allow this level of
customization.
