# Fencing

Fencing in CloudNativePG is the ultimate process of protecting the
data in one, more, or even all instances of a PostgreSQL cluster when they
appear to be malfunctioning. When an instance is fenced, the PostgreSQL server
process (`postmaster`) is guaranteed to be shut down, while the pod is kept running.
This process ensures that, until the fence is lifted, data on the pod isn't modified by
PostgreSQL and that the file system can be investigated for debugging and
troubleshooting purposes.

## How to fence instances

In CloudNativePG you can fence:

- A specific instance
- A list of instances
- An entire Postgres cluster

Fencing is controlled through the content of the `cnpg.io/fencedInstances`
annotation, which expects a JSON-formatted list of instance names.
If the annotation is set to `'["*"]'`, a singleton list with a wildcard, the
whole cluster is fenced.
If the annotation is set to an empty JSON list, the operator behaves as if the
annotation weren't set.

For example:

- `cnpg.io/fencedInstances: '["cluster-example-1"]'` fences just
  the `cluster-example-1` instance.

- `cnpg.io/fencedInstances: '["cluster-example-1","cluster-example-2"]'`
  fences the `cluster-example-1` and `cluster-example-2` instances.

- `cnpg.io/fencedInstances: '["*"]'` fences every instance in
  the cluster.

You can set the annotation on the Kubernetes object manually, for example, by using
the `kubectl annotate` command. Or, you can set it in a transparent way using the
`kubectl cnpg fencing on` subcommand:

```shell
# to fence only one instance
kubectl cnpg fencing on cluster-example 1

# to fence all the instances in a Cluster
kubectl cnpg fencing on cluster-example "*"
```

This example shows a cluster with an instance that was previously fenced:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
    annotations:
      cnpg.io/fencedInstances: '["cluster-example-1"]'
[...]
```

## How to lift fencing

You can lift fencing by clearing the annotation or by setting it to a different value.

As for setting fencing, you can do this either manually with `kubectl annotate` or
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
This process consists of an initial fast shutdown with a timeout set to
`.spec.switchoverDelay`, followed by an immediate shutdown. Then:

- The pod is kept alive.

- The pod isn't marked as Ready.

- All the changes that don't require the Postgres instance to be up are
  reconciled, including:
    - Configuration files
    - Certificates and all the cryptographic material

- Metrics aren't collected, except `cnpg_collector_fencing_on`, which is
  set to 1.

!!! Warning
    If a primary instance is fenced, its postmaster process
    is shut down but no failover is performed, interrupting the operativity of
    the applications. When the fence is lifted, the primary instance 
    starts up again without performing a failover.

    Given that, we recommend fencing primary instances only if strictly required.

If a fenced instance is deleted, the pod is re-created normally, but the
postmaster isn't started. This can be extremely helpful when instances
are crash looping.
