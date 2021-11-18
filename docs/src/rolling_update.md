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

- after the operator is updated, to ensure the Pods run the latest instance
  manager (unless [in-place updates are enabled](installation_upgrade.md#in-place-updates-of-the-instance-manager)).

The operator starts upgrading all the replicas, one Pod at a time, starting
from the one with the highest serial.

The primary is the last node to be upgraded. This operation is configurable and
managed by the `primaryUpdateStrategy` option, accepting these two values:

- `unsupervised`: the rolling update process is managed by Kubernetes
  and is entirely automated, with the *switchover* operation
  starting once all the replicas have been upgraded
- `supervised`: the rolling update process is suspended immediately
  after all replicas have been upgraded and can only be completed
  with a manual switchover triggered by an administrator with
  `kubectl cnp promote [cluster] [pod]`. The plugin can be downloaded from the
  [`kubectl-cnp` project page](https://github.com/EnterpriseDB/kubectl-cnp)
  on GitHub.

The default and recommended value is `unsupervised`.

The upgrade keeps the Cloud Native PostgreSQL identity and does not
re-clone the data. Pods will be deleted and created again with the same PVCs.

During the rolling update procedure, the services endpoints move to reflect
the cluster's status, so the applications ignore the node that
is updating.
