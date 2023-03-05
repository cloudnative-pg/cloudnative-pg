# Declarative hibernation

CloudNativePG is designed to keep PostgreSQL cluster up, running and available
anytime.

There are some kind of workloads that require the database to be up only when
the workload is active. Batch-driven solution are one of that kind.

In that scenario, the database need only to be up when the batch procedure is
running.

The declarative hibernation feature enables saving CPU power by removing the
database Pods while keeping the data PVCs.

## Hibernation

To hibernate a cluster, set the `cnpg.io/hibernation=on` annotation:

```
$ kubectl annotate cluster <cluster-name> --overwrite cnpg.io/hibernation=on
```

An hibernated cluster won't have any running Pods, while the PVCs are retained
in order for the cluster be rehydrated. Replica PVCs will be kept too.

The hibernation procedure will delete the primary Pod and the the replica Pods.
This procedure allows the replicas to be kept in sync.

The hibernation status can be monitored by looking for the `cnpg.io/hibernation`
condition:

```
$ kubectl get cluster <cluster-name> -o "jsonpath={.status.conditions[?(.type==\"cnpg.io/hibernation\")]}" 

{
        "lastTransitionTime":"2023-03-05T16:43:35Z",
        "message":"Cluster has been hibernated",
        "reason":"Hibernated",
        "status":"True",
        "type":"cnpg.io/hibernation"
}
```

Or the `status` command of the the kubectl-cnp plugin:

```
$ kubectl-cnpg status <cluster-name>
Cluster Summary
Name:              cluster-example
Namespace:         default
PostgreSQL Image:  ghcr.io/cloudnative-pg/postgresql:15.2
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

To rehydrate a cluster, set the `cnpg.io/hibernation=off` annotation:

```
$ kubectl annotate cluster <cluster-name> --overwrite cnpg.io/hibernation=on
```

The Pods will be recreated and the cluster operation will be resumed.
