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
the new PostgreSQL instance takes shorter than waiting.

!!! Note
    When performing the `kubectl drain` command, you will need
    to add the `--delete-local-data` option.
    Don't be afraid: it refers to another volume internally used
    by the operator - not the PostgreSQL data directory.
