---
id: fencing
sidebar_position: 420
title: Fencing
---

# Fencing
<!-- SPDX-License-Identifier: CC-BY-4.0 -->

Fencing in CloudNativePG is the ultimate process of protecting the
data in one, more, or even all instances of a PostgreSQL cluster when they
appear to be malfunctioning. When an instance is fenced, the PostgreSQL server
process (`postmaster`) is guaranteed to be shut down, while the pod is kept running.
This makes sure that, until the fence is lifted, data on the pod is not modified by
PostgreSQL and that the file system can be investigated for debugging and
troubleshooting purposes.

## How to fence instances

In CloudNativePG you can fence:

- a specific instance
- a list of instances
- an entire Postgres `Cluster`

Fencing is controlled through the content of the `cnpg.io/fencedInstances`
annotation, which expects a JSON formatted list of instance names.
If the annotation is set to `'["*"]'`, a singleton list with a wildcard, the
whole cluster is fenced.
If the annotation is set to an empty JSON list, the operator behaves as if the
annotation was not set.

For example:

- `cnpg.io/fencedInstances: '["cluster-example-1"]'` will fence just
  the `cluster-example-1` instance

- `cnpg.io/fencedInstances: '["cluster-example-1","cluster-example-2"]'`
  will fence the `cluster-example-1` and `cluster-example-2` instances

- `cnpg.io/fencedInstances: '["*"]'` will fence every instance in
  the cluster.

The annotation can be manually set on the Kubernetes object, for example via
the `kubectl annotate` command, or in a transparent way using the
`kubectl cnpg fencing on` subcommand:

```shell
# to fence only one instance
kubectl cnpg fencing on cluster-example 1

# to fence all the instances in a Cluster
kubectl cnpg fencing on cluster-example "*"
```

Here is an example of a `Cluster` with an instance that was previously fenced:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
    annotations:
      cnpg.io/fencedInstances: '["cluster-example-1"]'
[...]
```

## How to lift fencing

Fencing can be lifted by clearing the annotation, or set it to a different value.

As for fencing, this can be done either manually with `kubectl annotate`, or
using the `kubectl cnpg fencing` subcommand as follows:

```shell
# to lift the fencing only for one instance
# N.B.: at the moment this won't work if the whole cluster was fenced previously,
#       in that case you will have to manually set the annotation as explained above
kubectl cnpg fencing off cluster-example 1

# to lift the fencing for all the instances in a Cluster
kubectl cnpg fencing off cluster-example "*"
```

## How fencing works

Once an instance is set for fencing, the procedure to shut down the
`postmaster` process is initiated, identical to the one of the switchover.
This consists of an initial fast shutdown with a timeout set to
`.spec.switchoverDelay`, followed by an immediate shutdown. Then:

- the Pod will be kept alive

- the Pod won't be marked as *Ready*

- all the changes that don't require the Postgres instance to be up will be
  reconciled, including:
    - configuration files
    - certificates and all the cryptographic material

- metrics will not be collected, except `cnpg_collector_fencing_on` which will be
  set to 1

:::warning
    If a **primary instance** is fenced, its postmaster process
    is shut down but no failover is performed, interrupting the operativity of
    the applications. When the fence will be lifted, the primary instance will be
    started up again without performing a failover.

    Given that, we advise users to fence primary instances only if strictly required.
:::

If a fenced instance is deleted, the pod will be recreated normally, but the
postmaster won't be started. This can be extremely helpful when instances
are `Crashlooping`.
