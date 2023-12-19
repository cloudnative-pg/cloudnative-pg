# Rolling updates

The operator allows changing the PostgreSQL version used in a cluster while
applications are running against it.

!!! Important
    Only upgrades for PostgreSQL minor releases are supported.

Rolling upgrades are started:

- When you change the `imageName` attribute of the cluster specification

- When a change in the PostgreSQL configuration requires a restart

- When there's a change in the `Cluster` `.spec.resources` values

- When there's a change in size of the persistent volume claim on AKS

- After the operator is updated, to ensure the pods run the latest instance
  manager (unless [in-place updates are enabled](installation_upgrade.md#in-place-updates-of-the-instance-manager))

The operator starts upgrading all the replicas, one pod at a time, and begins
from the one with the highest serial.

The primary is the last node to be upgraded.

Rolling updates are configurable and can be either entirely automated
(`unsupervised`) or require human intervention (`supervised`).

The upgrade keeps the CloudNativePG identity, without recloning the
data. Pods are deleted and created again with the same PVCs and a new
image, if required.

During the rolling update procedure, each service endpoints move to reflect the
cluster's status, so that applications can ignore the node that's being
updated.

## Automated updates (`unsupervised`)

When `primaryUpdateStrategy` is set to `unsupervised`, the rolling update
process is managed by Kubernetes and is entirely automated. Once the replicas
are upgraded, the selected `primaryUpdateMethod` operation starts
on the primary. This is the default behavior.

The `primaryUpdateMethod` option accepts one of the following values:

- `restart` – If possible, perform an automated restart of the pod where the
  primary instance is running. Otherwise, the restart request is ignored and a
  switchover issued. This is the default behavior.

- `switchover` – A switchover operation is performed, setting the
  most aligned replica as the new target primary and shutting down the former
  primary pod.

There's no one-size-fits-all configuration for the update method, as that
depends on several factors, such as:

- The actual workload of your database
- The requirements in terms of RPO and RTO 
- Whether your PostgreSQL architecture is
  shared or shared nothing

Since PostgreSQL is a primary/standby architecture database management
system, the update process inevitably generates downtime for your
applications. One important aspect to consider for your context is the time it
takes for your pod to download the new PostgreSQL container image, as that
depends on your Kubernetes cluster settings and specifications. The
`switchover` method makes sure that the promoted instance already runs the
target image version of the container. The `restart` method instead might require
downloading the image from the origin registry after the primary pod is
shut down. You must determine whether, for your database, it's best
to use `restart` or `switchover` as part of the rolling update procedure.

## Manual updates (`supervised`)

When `primaryUpdateStrategy` is set to `supervised`, the rolling update process
is suspended immediately after all replicas are upgraded.

This phase can be completed only with either a manual switchover or an in-place
restart. Keep in mind that image upgrades can't be applied with an in-place restart, 
so a switchover is required in such cases.

You can trigger a switchover with:

```bash
kubectl cnpg promote [cluster] [new_primary]
```

You can trigger a restart with:

```bash
kubectl cnpg restart [cluster] [current_primary]
```

For more information, see the [cnpg plugin page](kubectl-plugin.md).
