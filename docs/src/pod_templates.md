# Pod templates

Pod templates are the most common way in Kubernetes to model the specifications
that each Pod in a workload resource - such as a `Deployment` - must have.  Pod
templates normally help define:

- standard Kubernetes objects *metadata*, such as `label`s and `annotation`s,
  for each of the Pods of the group
- `nodeAffinity` rules to control on which nodes Kubernetes shall schedule
  the requested Pods
- `podAffinity`/`podAntiAffinity` rules to request that Pods be close/distant
  to other Pods (having, for example, some labels)

Similarly, the `Cluster` custom resource definition which is included in the
Cloud Native PostgreSQL operator provides the same behavior.

!!! Warning
    Pod templates are extremely powerful. With the operator's current
    implementation, we recommend using Pod templates only to control node and
    pod affinity, as no validation is performed.
    Therefore, it is your responsibility to verify that the operator then
    applies the requested specifications.

More information on this feature is available in the
["Assigning Pods to Nodes"](https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/)
page from Kubernetes documentation.
