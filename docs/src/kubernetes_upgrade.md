---
id: kubernetes_upgrade
sidebar_position: 370
title: Kubernetes upgrade and maintenance
---

# Kubernetes upgrade and maintenance
<!-- SPDX-License-Identifier: CC-BY-4.0 -->

Maintaining an up-to-date Kubernetes cluster is crucial for ensuring optimal
performance and security, particularly for self-managed clusters, especially
those running on bare metal infrastructure. Regular updates help address
technical debt and mitigate business risks, despite the controlled downtimes
associated with temporarily removing a node from the cluster for maintenance
purposes. For further insights on embracing risk in operations, refer to the
["Embracing Risk"](https://landing.google.com/sre/sre-book/chapters/embracing-risk/)
chapter from the Site Reliability Engineering book.

## Importance of Regular Updates

Updating Kubernetes involves planning and executing maintenance tasks, such as
applying security updates to underlying Linux servers, replacing malfunctioning
hardware components, or upgrading the cluster to the latest Kubernetes version.
These activities are essential for maintaining a robust and secure
infrastructure.

## Maintenance Operations in a Cluster

Typically, maintenance operations are carried out on one node at a time, following a [structured process](https://kubernetes.io/docs/tasks/administer-cluster/kubeadm/kubeadm-upgrade/):

1. eviction of workloads (`drain`): workloads are gracefully moved away from
   the node to be updated, ensuring a smooth transition.
2. performing the operation: the actual maintenance operation, such as a
   system update or hardware replacement, is executed.
3. rejoining the node to the cluster (`uncordon`): the updated node is
   reintegrated into the cluster, ready to resume its responsibilities.

This process requires either stopping workloads for the entire upgrade duration
or migrating them to other nodes in the cluster.

## Temporary PostgreSQL Cluster Degradation

While the standard approach ensures service reliability and leverages
Kubernetes' self-healing capabilities, there are scenarios where operating with
a temporarily degraded cluster may be acceptable. This is particularly relevant
for PostgreSQL clusters relying on **node-local storage**, where the storage is
local to the Kubernetes worker node running the PostgreSQL database. Node-local
storage, or simply *local storage*, is employed to enhance performance.

:::note
    If your database files reside on shared storage accessible over the
    network, the default self-healing behavior of the operator can efficiently
    handle scenarios where volumes are reused by pods on different nodes after a
    drain operation. In such cases, you can skip the remaining sections of this
    document.
:::

## Pod Disruption Budgets

By default, CloudNativePG safeguards Postgres cluster operations. If a node is
to be drained and contains a cluster's primary instance, a switchover happens
ahead of the drain. Once the instance in the node is downgraded to replica, the
draining can resume.
For single-instance clusters, a switchover is not possible, so CloudNativePG
will prevent draining the node where the instance is housed.
Additionally, in clusters with 3 or more instances, CloudNativePG guarantees that
only one replica at a time is gracefully shut down during a drain operation.

Each PostgreSQL `Cluster` is equipped with two associated `PodDisruptionBudget`
resources - you can easily confirm it with the `kubectl get pdb` command.

Our recommendation is to leave pod disruption budgets enabled for every
production Postgres cluster. This can be effortlessly managed by toggling the
`.spec.enablePDB` option, as detailed in the
[API reference](cloudnative-pg.v1.md#clusterspec).

## PostgreSQL Clusters used for Development or Testing

For PostgreSQL clusters used for development purposes, often consisting of
a single instance, it is essential to disable pod disruption budgets. Failure
to do so will prevent the node hosting that cluster from being drained.

The following example illustrates how to disable pod disruption budgets for a
1-instance development cluster:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: dev
spec:
  instances: 1
  enablePDB: false

  storage:
    size: 1Gi
```

This configuration ensures smoother maintenance procedures without restrictions
on draining the node during development activities.

## Node Maintenance Window

:::info[Important]
    While CloudNativePG will continue supporting the node maintenance window,
    it is currently recommended to transition to direct control of pod disruption
    budgets, as explained in the previous section. This section is retained
    mainly for backward compatibility.
:::

Prior to release 1.23, CloudNativePG had just one declarative mechanism to manage
Kubernetes upgrades when dealing with local storage: you had to temporarily put
the cluster in **maintenance mode** through the `nodeMaintenanceWindow` option
to avoid standard self-healing procedures to kick in, while, for example,
enlarging the partition on the physical node or updating the node itself.

:::warning
    Limit the duration of the maintenance window to the shortest
    amount of time possible. In this phase, some of the expected
    behaviors of Kubernetes are either disabled or running with
    some limitations, including self-healing, rolling updates,
    and Pod disruption budget.
:::

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

:::note
    When performing the `kubectl drain` command, you will need
    to add the `--delete-emptydir-data` option.
    Don't be afraid: it refers to another volume internally used
    by the operator - not the PostgreSQL data directory.
:::

:::info[Important]
    `PodDisruptionBudget` management can be disabled by setting the
    `.spec.enablePDB` field to `false`. In that case, the operator won't
    create `PodDisruptionBudgets` and will delete them if they were
    previously created.
:::

### Single instance clusters with `reusePVC` set to `false`

:::info[Important]
    We recommend to always create clusters with more
    than one instance in order to guarantee high availability.
:::

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
