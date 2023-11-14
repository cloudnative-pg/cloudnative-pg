# Kubernetes upgrade

Kubernetes clusters must be kept updated. Updating becomes even more
important if you're self-managing your Kubernetes clusters, especially
on bare metal.

Planning and executing regular updates is a way for your organization
to clean up the technical debt and reduce the business risks. This is true despite
the introduction in your Kubernetes infrastructure of controlled
downtimes that temporarily take out a node from the cluster for
maintenance reasons (recommended reading:
[Embracing Risk](https://landing.google.com/sre/sre-book/chapters/embracing-risk/)
from the Site Reliability Engineering book).

For example, you might need to apply security updates on the Linux
servers where Kubernetes is installed. Or you might need to replace a malfunctioning
hardware component such as RAM, CPU, or RAID controller or even upgrade
the cluster to the latest version of Kubernetes.

Usually, maintenance operations in a cluster are performed one node
at a time by:

1. Evicting the workloads from the node being updated (`drain`).
2. Performing the actual operation (for example, system update).
3. Rejoining the node to the cluster (`uncordon`).

This process requires workloads to either be stopped for the
entire duration of the upgrade or migrated to another node.

While the latest case is the expected one in terms of service
reliability and self-healing capabilities of Kubernetes, there can
be situations where we recommend operating with a temporarily
degraded cluster and waiting for the upgraded node to be up again.

In particular, if your PostgreSQL cluster relies on *node-local storage*,
that's storage that's local to the Kubernetes worker node where
the PostgreSQL database is running.
Node-local storage (or simply *local storage*) is used to enhance performance.

!!! Note
    If your database files are on shared storage over the network,
    you might not need to define a maintenance window. If the volumes currently
    used by the pods can be reused by pods running on different nodes after
    the drain, the default self-healing behavior of the operator will work
    fine. (You can then skip the rest of these instructions.)

When using local storage for PostgreSQL, we recommend that you temporarily
put the cluster in maintenance mode using the `nodeMaintenanceWindow`
option. This option avoids having standard self-healing procedures kick in
while, for example, enlarging the partition on the physical node or
updating the node itself.

!!! Warning
    Limit the duration of the maintenance window to the shortest
    amount of time possible. In this phase, some of the expected
    behaviors of Kubernetes are either disabled or running with
    some limitations, including self-healing, rolling updates,
    and pod disruption budget.

The `nodeMaintenanceWindow` option of the cluster has two other
settings:

`inProgress`:
Boolean value that states whether the maintenance window for the nodes
is currently in progress. By default, it's set to `off`.
During the maintenance window, the `reusePVC` option is
evaluated by the operator.

`reusePVC`:
Boolean value that defines whether an existing PVC is reused
during the maintenance operation. By default, it's set to `on`.
When enabled, Kubernetes waits for the node to come up
again and then reuses the existing PVC. The `PodDisruptionBudget`
policy is temporarily removed.
When disabled, Kubernetes forces the re-creation of the
pod on a different node with a new PVC by relying on
PostgreSQL's physical streaming replication and then destroys
the old PVC together with the pod. We generally don't recommend this scenario
unless the database's size is small and recloning
the new PostgreSQL instance takes shorter than waiting. This behavior
doesn't apply to clusters with only one instance and
`reusePVC` disabled. See [Single instance clusters with `reusePVC` set to `false`](#single-instance-clusters-with-reusepvc-set-to-false).

!!! Note
    When performing the `kubectl drain` command, you  need
    to add the `--delete-local-data` option.
    This option refers to another volume internally used
    by the operator, not the PostgreSQL data directory.

## Single instance clusters with `reusePVC` set to `false`

!!! Important
    To guarantee high availability, we recommend that you always create clusters with more
    than one instance.

Deleting the only PostgreSQL instance in a single instance cluster with
`reusePVC` set to `false` implies all data being lost.
Therefore, we prevent users from draining nodes such instances might be running
on, even in maintenance mode.

However, in case maintenance is required for such a node, you have two options:

-  Enable `reusePVC`, accepting the downtime.
-  Replicate the instance on a different node and switch over the primary.

As long as a database service downtime is acceptable for your environment,
draining the node is as simple as setting the `nodeMaintenanceWindow` to
`inProgress: true` and `reusePVC: true`. This approach allows the instance to
be deleted and re-created as soon as the original PVC is available
(for example, with node local storage, as soon as the node is back up).

Otherwise, to shut down the original cluster on the node undergoing maintenance,
you have to scale up the cluster. This involves creating a new instance
on a different node and promoting the new instance to primary. The only
downtime in this case is the duration of the switchover.

A possible approach is:

1. Cordon the node on which the current instance is running.
2. Scale up the cluster to two instances, which might take some time depending on the database size.
3. As soon as the new instance is running, the operator
   performs a switchover given that the current primary is running on a cordoned node.
4. Scale back down the cluster to a single instance, which deletes the old instance.
5. The old primary's node can be drained successfully, while leaving the new primary
   running on a new node.
