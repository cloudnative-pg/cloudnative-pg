The operator can deploy a new version of Cloud Native PostgreSQL
while applications are running against the cluster.

To initiate a rolling update, the user can change the `imageName`
attribute of the cluster. The operator starts upgrading all the
replicas, one Pod at a time, starting from the one with the highest
serial.
The primary is the last node to be upgraded. This operation
is configurable and managed by the `masterUpdateStrategy` option,
accepting these two values:

* `switchover`: the rolling update process is managed by Kubernetes
  and is entirely automated, with the *switchover* operation
  starting once all the replicas have been upgraded
* `wait`: the rolling update process is suspended immediately
  after all replicas have been upgraded, and can only be completed
  with a manual switchover triggered by an administrator with:
  ```sh
  kubectl cnp promote [cluster] [pod]
  ```

The default and recommended value is `switchover`.

Every version of the operator comes with a default PostgreSQL image version.
If a cluster doesn't have `imageName` specified, the operator will upgrade
it to match its default.

If you are using persistent storage, which is a requirement for
a production environment, the upgrade keeps the Cloud Native PostgreSQL
identity and do not reclone the data.

If you are using emptyDir based storage, which is meaningful only for
a development environment, the operator cannot preserve the node
data directory and must create new nodes and part those with
the old version.

During the rolling update procedure, the services endpoints move to reflect
the status of the cluster, so the applications ignore the node that
is updating.
