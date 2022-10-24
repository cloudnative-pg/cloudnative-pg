# Custom Pod Controller

Kubernetes uses the
[Controller pattern](https://kubernetes.io/docs/concepts/architecture/controller/)
to align the current cluster state with the desired one.

Stateful applications are usually managed with the
[`StatefulSet`](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/)
controller, which allows to create and align a set of Pods created with the
same specification and maintaining a sticky identity.

CloudNativePG, instead of relying on the `StatefulSet` controller to manage
the PostgreSQL instances, implements its own custom controller. This design
choice, while bringing more complexity in the implementation, provided the
operator with more flexibility on how we manage the cluster while being
transparent on the topology of PostgreSQL clusters.

PostgreSQL's operators can be certainly designed around the `StatefulSet`
behavior (and many are indeed) but CloudNativePG avoid this tight
coupling. This may surprise new users.

Like many choices in the design realm, different ones lead to different
compromises. The next sections discuss a few of the points where we believe
this design choice has made the implementation of CloudNativePG
more reliable and easy to understand.

## Primary Instances versus Replicas

The `StatefulSet` controller is designed to create a set of Pods
from just one template. Given that we use one `Pod` per PostgreSQL instance,
we have two kinds of Pods:

- one for the primary instance, and
- the other ones for replicas.

While the operator is using the same spec for every Pod and the role
is managed using labels, the rolling deployment mechanism depends on the
operation that triggered it.

There are operations that should be applied on the replicas first
and then on the primary, but only after an updated replica is promoted
as the new primary.
This happens for example when you want to apply a different image version,
or when you lower configuration parameters like `max_connections` (which are
[treated in a special way by PostgreSQL when hot standby replicas are around](https://www.postgresql.org/docs/current/hot-standby.html)).

While doing that, CloudNativePG takes into consideration the role of
the PostgreSQL instance - and not just its serial number.

Sometimes the operator needs to follow the opposite process: work on the
primary first, and then on the replicas. This happens, for example, when you
raise `max_connections`. In that case CloudNativePG will:

- apply the new setting to the primary instance
- restart it
- apply the new setting on the replicas

The `StatefulSet` controller, being application-independent, can't
incorporate this behavior, which is specific to PostgreSQL's native
replication technology.

## Coherence of PVCs

Sometimes the same PostgreSQL instance works on multiple PVCs: this happens
when the WAL storage is separated from `PGDATA`, and in the future when CloudNativePG
will support tablespaces.

The two data stores need to be coherent from the PostgreSQL point of view,
as they're used at the same time. If you delete the PVC corresponding to
the WAL storage of an instance, the PVC where `PGDATA` is stored will not be
useful anymore.

This behavior is specific to PostgreSQL and is not implemented in the
`StatefulSet` controller - the latter not being application specific.

The `StatefulSet` would just recreate the missing PVC and a new Pod for it
after the user drops one of the two PVCs and the corresponding Pod, leading
to a corrupted PostgreSQL instance.

CloudNativePG would instead classify the remaining Pod as unusable and
start creating a new pair of PVCs for another instance to correctly join the
cluster.

## Local storage, remote storage and database size

Sometimes you need to take down a Kubernetes node to be upgraded and,
depending on your environment, a new one will join or the updated one
will be brought up again.

Supposing the unavailable node was hosting a PostgreSQL instance,
depending on your database size and on your cloud infrastructure, you
may prefer to choose one of the following actions:

1. drop the PVC and the Pod of the node which is down and create
   another one cloning the data of an existing instance; after
   that, schedule a Pod for it

2. drop the Pod, schedule the Pod in a different node, and mount
   the PVC from there

3. leave the Pod and the PVC as they are, and wait for the node to
   be back up.

The first solution is useful when your database size permits that, allowing you
to immediately bring back the desired number of replicas.

The second solution is only feasible when you're not using the storage of the
local node and re-mounting the PVC in another host is possible in a reasonable
amount of time (which only you and your organization know).

The third solution is appropriate when the database is big and is using local
node storage for maximum performance and data durability.

The user can select the preferred behavior at cluster level and
this is implemented with different strategies by the CloudNativePG
controller (read the ["Kubernetes upgrade"](kubernetes_upgrade.md) section
for details).

Being generic, the `StatefulSet` doesn't allow this level of
customization.
