# Declarative hibernation

CloudNativePG is designed to keep PostgreSQL clusters up, running and available
anytime.

There are some kinds of workloads that require the database to be up only when
the workload is active. Batch-driven solutions are one such case.

In batch-driven solutions, the database needs to be up only when the batch
process is running.

The declarative hibernation feature enables saving CPU power by removing the
database Pods, while keeping the database PVCs.

!!! Note
    Declarative hibernation is different from the existing implementation
    of [imperative hibernation via the `cnpg` plugin](cnpg-plugin.md#cluster-hibernation).
    Imperative hibernation shuts down all Postgres instances in the High
    Availability cluster, and keeps a static copy of the PVCs of the primary that
    contain `PGDATA` and WALs. The plugin enables to exit the hibernation phase, by
    resuming the primary and then recreating all the replicas - if they exist.

## Hibernation

To hibernate a cluster, set the `cnpg.io/hibernation=on` annotation:

``` sh
$ kubectl annotate cluster <cluster-name> --overwrite cnpg.io/hibernation=on
```

A hibernated cluster won't have any running Pods, while the PVCs are retained
so that the cluster can be rehydrated at a later time. Replica PVCs will be
kept in addition to the primary's PVC.

The hibernation procedure will delete the primary Pod and then the replica
Pods, avoiding switchover, to ensure the replicas are kept in sync.

The hibernation status can be monitored by looking for the `cnpg.io/hibernation`
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

The hibernation status can also be read with the `status` sub-command of the
`cnpg` plugin for `kubectl`:

``` sh
$ kubectl cnpg status <cluster-name>
Cluster Summary
Name:              cluster-example
Namespace:         default
PostgreSQL Image:  ghcr.io/cloudnative-pg/postgresql:15.3
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

To rehydrate a cluster, either set the `cnpg.io/hibernation` annotation to `off`:

```
$ kubectl annotate cluster <cluster-name> --overwrite cnpg.io/hibernation=off
```

Or, just unset it altogether:

```
$ kubectl annotate cluster <cluster-name> cnpg.io/hibernation-
```

The Pods will be recreated and the cluster will resume operation.
