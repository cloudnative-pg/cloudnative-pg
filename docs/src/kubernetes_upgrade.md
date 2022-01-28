# Kubernetes Upgrade

Kubernetes clusters must be kept updated. This becomes even more
important if you are self-managing your Kubernetes clusters, especially
on **bare metal**.

Planning and executing regular updates is a way for your organization
to clean up the technical debt and reduce the business risks, despite
the introduction in your Kubernetes infrastructure of controlled
downtimes that temporarily take out a node from the cluster for
maintenance reasons (recommended reading:
["Embracing Risk"](https://landing.google.com/sre/sre-book/chapters/embracing-risk/)
from the Site Reliability Engineering book).

For example, you might need to apply security updates on the Linux
servers where Kubernetes is installed, or to replace a malfunctioning
hardware component such as RAM, CPU, or RAID controller, or even upgrade
the cluster to the latest version of Kubernetes.

Usually, maintenance operations in a cluster are performed one node
at a time by:

1. evicting the workloads from the node to be updated (`drain`)
2. performing the actual operation (for example, system update)
3. re-joining the node to the cluster (`uncordon`)

The above process requires workloads to be either stopped for the
entire duration of the upgrade or migrated to another node.

While the latest case is the expected one in terms of service
reliability and self-healing capabilities of Kubernetes, there can
be situations where it is advised to operate with a temporarily
degraded cluster and wait for the upgraded node to be up again.

In particular, if your PostgreSQL cluster relies on **node-local storage**
\- that is *storage which is local to the Kubernetes worker node where
the PostgreSQL database is running*.
Node-local storage (or simply *local storage*) is used to enhance performance.

!!! Note
    If your database files are on shared storage over the network,
    you may not need to define a maintenance window. If the volumes currently
    used by the pods can be reused by pods running on different nodes after
    the drain, the default self-healing behavior of the operator will work
    fine (you can then skip the rest of this section).

When using local storage for PostgreSQL, you are advised to temporarily
put the cluster in **maintenance mode** through the `nodeMaintenanceWindow`
option to avoid standard self-healing procedures to kick in,
while, for example, enlarging the partition on the physical node or
updating the node itself.

!!! Warning
    Limit the duration of the maintenance window to the shortest
    amount of time possible. In this phase, some of the expected
    behaviors of Kubernetes are either disabled or running with
    some limitations, including self-healing, rolling updates,
    and Pod disruption budget.

The `nodeMaintenanceWindow` option of the cluster has two further
settings:

`inProgress`:
Boolean value that states if the maintenance window for the nodes
is currently in progress or not. By default, it is set to `off`.
During the maintenance window, the `reusePVC` option below is
evaluated by the operator.

`reusePVC`:
Boolean value that defines if an existing PVC is reused or
not during the maintenance operation. By default, it is set to `on`.
When **enabled**, Kubernetes waits for the node to come up
again and then reuses the existing PVC; the `PodDisruptionBudget`
policy is temporarily removed.
When **disabled**, Kubernetes forces the recreation of the
Pod on a different node with a new PVC by relying on
PostgreSQL's physical streaming replication, then destroys
the old PVC together with the Pod. This scenario is generally
not recommended unless the database's size is small, and re-cloning
the new PostgreSQL instance takes shorter than waiting. This behavior
does **not** apply to clusters with only one instance and
reusePVC disabled: see section below.

!!! Note
    When performing the `kubectl drain` command, you will need
    to add the `--delete-local-data` option.
    Don't be afraid: it refers to another volume internally used
    by the operator - not the PostgreSQL data directory.

## Single instance clusters with `reusePVC` set to `false`

!!! Important
    We recommend to always create clusters with more
    than one instance in order to guarantee high availability.

Deleting the only PostgreSQL instance in a single instance cluster with
`reusePVC` set to `false` would imply all data being lost,
therefore we prevent users from draining nodes such instances might be running
on, even in maintenance mode.

However, in case maintenance is required for such a node you have two options:

1. Enable `reusePVC`, accepting the downtime
2. Replicate the instance on a different node and switch over the primary

As long as a database service downtime is acceptable for your environment,
draining the node is as simple as setting the `nodeMaintenanceWindow` to
`inProgress: true` and `reusePVC: true`. This will allow the instance to
be deleted and recreated as soon as the original PVC is available
(e.g. with node local storage, as soon as the node is back up).

Otherwise you will have to scale up the cluster, creating a new instance
on a different node and promoting the new instance to primary in order to
shut down the original one on the node undergoing maintenance. The only
downtime in this case will be the duration of the switchover.

A possible approach could be:

1. Cordon the node on which the current instance is running.
2. Scale up the cluster to 2 instances, could take some time depending on the database size.
3. As soon as the new instance is running, the operator will automatically
   perform a switchover given that the current primary is running on a cordoned node.
4. Scale back down the cluster to a single instance, this will delete the old instance
5. The old primary's node can now be drained successfully, while leaving the new primary
   running on a new node.
