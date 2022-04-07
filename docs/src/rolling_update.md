# Rolling Updates

The operator allows changing the PostgreSQL version used in a cluster while
applications are running against it.

!!! Important
    Only upgrades for PostgreSQL minor releases are supported.

Rolling upgrades are started when:

- the user changes the `imageName` attribute of the cluster specification;

- a change in the PostgreSQL configuration requires a restart to be
  applied;

- a change on the `Cluster` `.spec.resources` values

- a change in size of the persistent volume claim on AKS

- after the operator is updated, to ensure the Pods run the latest instance
  manager (unless [in-place updates are enabled](installation_upgrade.md#in-place-updates-of-the-instance-manager)).

The operator starts upgrading all the replicas, one Pod at a time, starting
from the one with the highest serial.

The primary is the last node to be upgraded. This operation is configurable and
managed by the `primaryUpdateStrategy` and `primaryUpdateMethod` options as follows:

- `primaryUpdateStrategy` accepts the two following values:
  - `unsupervised`: the rolling update process is managed by Kubernetes
    and is entirely automated, with the selected `primaryUpdateMethod` operation
    starting once all the replicas have been upgraded
  - `supervised`: the rolling update process is suspended immediately
    after all replicas have been upgraded and can only be completed
    with a manual switchover with `kubectl cnp promote [cluster] [new_primary]` or
    an in-place restart with `kubectl cnp restart [cluster] [old_primary]` triggered 
    by an administrator. The plugin can be downloaded from the
    [`kubectl-cnp` project page](https://github.com/EnterpriseDB/kubectl-cnp)
    on GitHub.
- `primaryUpdateMethod` accepts the two following values which will be taken into
  consideration if `primaryUpdateStrategy` is set to `unsupervised`:
  - `switchover`: once only the primary instance is missing to be updated, first a
    switchover will be performed to another already updated instance and then the
    old primary will be restarted.
  - `restart`: once only the primary instance is missing to be updated, the primary
    instance will be restarted in-place, without requiring a switchover. In case the
    change requires a switchover because the changes cannot be applied without it,
    the restart in-place will be ignored and the switchover will take precedence.

The default and recommended values are respectively `unsupervised` and `switchover`.

The upgrade keeps the Cloud Native PostgreSQL identity and does not
re-clone the data. Pods will be deleted and created again with the same PVCs.

During the rolling update procedure, the services endpoints move to reflect
the cluster's status, so the applications ignore the node that
is updating.
