---
id: rolling_update
sidebar_position: 150
title: Rolling updates
---

# Rolling updates
<!-- SPDX-License-Identifier: CC-BY-4.0 -->

The operator allows you to change the PostgreSQL version used in a cluster
while applications continue running against it.

Rolling upgrades are triggered when:

- you change the `imageName` attribute in the cluster specification;

- the [image catalog](image_catalog.md) is updated with a new image for the
  major version used by the cluster;

- a change in the PostgreSQL configuration requires a restart to apply;

- you change the `Cluster` `.spec.resources` values;

- you resize the persistent volume claim on AKS;

- the operator is updated, ensuring Pods run the latest instance manager
  (unless [in-place updates are enabled](installation_upgrade.md#in-place-updates-of-the-instance-manager)).

During a rolling upgrade, the operator upgrades all replicas one Pod at a time,
starting from the one with the highest serial.

The primary is always the last node to be upgraded.

Rolling updates are configurable and can be either entirely automated
(`unsupervised`) or requiring human intervention (`supervised`).

The upgrade keeps the CloudNativePG identity, without re-cloning the
data. Pods will be deleted and created again with the same PVCs and a new
image, if required.

During the rolling update procedure, each service endpoints move to reflect the
cluster's status, so that applications can ignore the node that is being
updated.

## Automated updates (`unsupervised`)

When `primaryUpdateStrategy` is set to `unsupervised`, the rolling update
process is managed by Kubernetes and is entirely automated. Once the replicas
have been upgraded, the selected `primaryUpdateMethod` operation will initiate
on the primary. This is the default behavior.

The `primaryUpdateMethod` option accepts one of the following values:

- `restart`: if possible, perform an automated restart of the pod where the
  primary instance is running. Otherwise, the restart request is ignored and a
  switchover issued. This is the default behavior.

- `switchover`: a switchover operation is automatically performed, setting the
  most aligned replica as the new target primary, and shutting down the former
  primary pod.

There's no one-size-fits-all configuration for the update method, as that
depends on several factors like the actual workload of your database, the
requirements in terms of [RPO](before_you_start.md#postgresql-terminology) and
[RTO](before_you_start.md#postgresql-terminology), whether your PostgreSQL architecture is shared
or shared nothing, and so on.

Indeed, being PostgreSQL a primary/standby architecture database management
system, the update process inevitably generates a downtime for your
applications. One important aspect to consider for your context is the time it
takes for your pod to download the new PostgreSQL container image, as that
depends on your Kubernetes cluster settings and specifications. The
`switchover` method makes sure that the promoted instance already runs the
target image version of the container. The `restart` method instead might require
to download the image from the origin registry after the primary pod has been
shut down. It is up to you to determine whether, for your database, it is best
to use `restart` or `switchover` as part of the rolling update procedure.

## Manual updates (`supervised`)

When `primaryUpdateStrategy` is set to `supervised`, the rolling update process
is suspended immediately after all replicas have been upgraded.

This phase can only be completed with either a manual switchover or an in-place
restart. Keep in mind that image upgrades can not be applied with an in-place restart, 
so a switchover is required in such cases.

You can trigger a switchover with:

```bash
kubectl cnpg promote [cluster] [new_primary]
```

You can trigger a restart with:

```bash
kubectl cnpg restart [cluster] [current_primary]
```

You can find more information in the [`cnpg` plugin page](kubectl-plugin.md).
