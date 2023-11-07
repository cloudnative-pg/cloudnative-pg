# Declarative hibernation

CloudNativePG is designed to keep PostgreSQL clusters up, running, and always available.

Some kinds of workloads require the database to be up only when
the workload is active. Batch-driven solutions are one such case.
In batch-driven solutions, the database needs to be up only when the batch
process is running.

The declarative hibernation feature enables saving CPU power by removing the
database pods while keeping the database PVCs.

!!! Note
    Declarative hibernation is different from the existing implementation
    of [imperative hibernation by way of the `cnpg` plugin](kubectl-plugin.md#cluster-hibernation).
    Imperative hibernation shuts down all Postgres instances in the high-availability 
    cluster and keeps a static copy of the PVCs of the primary that
    contain `PGDATA` and WALs. The plugin enables you to exit the hibernation phase by
    resuming the primary and then recreating all the replicas, if they exist.

## Hibernation

To hibernate a cluster, set the `cnpg.io/hibernation=on` annotation:

``` sh
$ kubectl annotate cluster <cluster-name> --overwrite cnpg.io/hibernation=on
```

A hibernated cluster doesn't have any running pods, while the PVCs are retained
so that the cluster can be rehydrated at a later time. Replica PVCs are
kept in addition to the primary's PVC.

The hibernation procedure deletes the primary pod and then the replica
pods, avoiding switchover. This approach ensures the replicas are kept in sync.

You can monitor the hibernation status by looking for the `cnpg.io/hibernation`
condition:

``` sh
$ kubectl get cluster <cluster-name> -o "jsonpath={.status.conditions[?(.type==\"cnpg.io/hibernation\")]}" 

{
        "lastTransitionTime":"2023-03-05T16:43:35Z",
        "message":"Cluster has been hibernated",
        "reason":"Hibernated",
        "status":"True",
        "type":"cnpg.io/hibernation"
}
```

You can also read the hibernation status using the `status` subcommand of the
`cnpg` plugin for `kubectl`:

``` sh
$ kubectl cnpg status <cluster-name>
Cluster Summary
Name:              cluster-example
Namespace:         default
PostgreSQL Image:  ghcr.io/cloudnative-pg/postgresql:16.0
Primary instance:  cluster-example-2
Status:            Cluster in healthy state 
Instances:         3
Ready instances:   0

Hibernation
Status   Hibernated
Message  Cluster has been hibernated
Time     2023-03-05 16:43:35 +0000 UTC
[..]
```

## Rehydration

To rehydrate a cluster, you can set the `cnpg.io/hibernation` annotation to `off`:

```
$ kubectl annotate cluster <cluster-name> --overwrite cnpg.io/hibernation=off
```

Alternatively, you can unset it:

```
$ kubectl annotate cluster <cluster-name> cnpg.io/hibernation-
```

The pods are re-created, and the cluster resumes operation.
