# Networking

CloudNativePG assumes the underlying Kubernetes cluster has the required
connectivity already set up.
Networking on Kubernetes is an important and extended topic, please refer to
the [Kubernetes documentation](https://kubernetes.io/docs/concepts/services-networking/).

If following the quickstart guide and installing CloudNativePG on a local KinD
or K3d cluster, everything should work for you out of the box.

When installing CloudNativePG on existing infrastructure, it could be impacted
by the the networking setup.
Specifically, [Network Policies](https://kubernetes.io/docs/concepts/services-networking/network-policies/)
might be in place restricting various types of traffic.

There are a some points you should be aware of when deploying the
operator and using it to create clusters.

## Cross-namespace network policy for the operator

If you follow the quickstart guide, or use helm for deployment, the operator
will be installed in a dedicated namespace, (`cnpg-system` by default).
We recommend that you create clusters in a different namespace.

It *must* be possible for the operator to get a connection to cluster pods.
This might be precluded if there is a `NetworkPolicy` restricting
cross-namespace traffic.

For example, the
[kubernetes guide on network policies](https://kubernetes.io/docs/concepts/services-networking/network-policies/)
contains an example policy denying all ingress traffic by default.

If your local kubernetes setup has this kind of restrictive network policy, you
will need to create a `NetworkPolicy` to explicitly allow connection from the
operator into cluster pods. You can find an example in the
[`networkpolicy-example.yaml`](samples/networkpolicy-example.yaml)
file in this repository.
Note that you'll need to configure the cluster name to suit your setup.

## Cross-cluster networking

When [bootstrapping](bootstrap.md) from another cluster, or with the
`externalClusters` section, there needs to be connectivity between the various
clusters, object stores and namespaces involved.

Again, we refer you to the [Kubernetes documentation](https://kubernetes.io/docs/concepts/services-networking/)
for setup information.
