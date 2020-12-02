# Rolling Updates

The operator allows changing the PostgreSQL version used in a cluster while
applications are running against it.

!!! Important
    Only upgrades for PostgreSQL minor releases are supported.

To initiate a rolling update, the user can change the `imageName`
attribute of the cluster. The operator starts upgrading all the
replicas, one Pod at a time, starting from the one with the highest
serial.
The primary is the last node to be upgraded. This operation
is configurable and managed by the `primaryUpdateStrategy` option,
accepting these two values:

* `switchover`: the rolling update process is managed by Kubernetes
  and is entirely automated, with the *switchover* operation
  starting once all the replicas have been upgraded
* `manual`: the rolling update process is suspended immediately
  after all replicas have been upgraded and can only be completed
  with a manual switchover triggered by an administrator with:
  `kubectl cnp promote [cluster] [pod]`

The default and recommended value is `switchover`.

Every version of the operator comes with a default PostgreSQL image version.
If a cluster doesn't have `imageName` specified, the operator will upgrade
it to match its default.

The upgrade keeps the Cloud Native PostgreSQL identity and does not
reclone the data.

During the rolling update procedure, the services endpoints move to reflect
the cluster's status, so the applications ignore the node that
is updating.
