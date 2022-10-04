# Hibernation

Sometimes you want to suspend the execution of a CNP cluster while retaining
its data and then start it again. CNP calls that feature **hibernation**.

You can hibernate a cluster using the `hibernate on` subcommand of
the [`cnpg` plugin for kubectl](cnpg-plugin.md#hibernate):

```
kubectl cnpg hibernate off cluster-example
```

This will:

1. shutdown every PostgreSQL instance
2. detach the PVCs containing the data of the primary instance, and annotate
   them with the latest database status and the latest cluster configuration
3. delete the Cluster itself and every generated resource, beside the PVCs
   before mentioned

When a CNP cluster is hibernated, it's represented just by a group of PVCs,
where the one containing the PostgreSQL data directory is annotated with what
was the latest Cluster status.

A cluster having fenced instances cannot be hibernated, as fencing is
part of the hibernation procedure too. In case of error the Operator will
not be able to revert the procedure. You can still force the operation
with:

```
kubectl cnpg hibernate off cluster-example --force
```

A hibernated CNP cluster can be resumed with:

```
kubectl cnpg hibernate on cluster-example
```
