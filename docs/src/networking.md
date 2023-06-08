# Networking

CloudNativePG assumes the underlying Kubernetes cluster has the required
connectivity already set up.
Networking on Kubernetes is an important and extended topic, please refer to
the [Kubernetes documentation](https://kubernetes.io/docs/concepts/services-networking/).

If you're following the quickstart guide to install CloudNativePG on a local KinD or K3d cluster, you should not
encounter any networking issues.

However, when deploying CloudNativePG on existing infrastructure, networking setup might affect its operation.
Specifically, existing [Network Policies](https://kubernetes.io/docs/concepts/services-networking/network-policies/)
might restrict certain types of traffic.

There are several interactions between the operator and the clusters to be aware of.

## Cross-namespace network policy for the operator

Following the quickstart guide or using helm for deployment will install the operator in
a dedicated namespace (`cnpg-system` by default).
We recommend that you create clusters in a different namespace.

The operator *must* be able to connect to cluster pods.
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
Please note, you'll need to adjust the cluster name to match your specific setup.

## Cross-cluster networking

During [bootstrapping](bootstrap.md) from another cluster or when using the `externalClusters` section,
ensure connectivity among all clusters, object stores, and namespaces involved.


Again, we refer you to the [Kubernetes documentation](https://kubernetes.io/docs/concepts/services-networking/)
for setup information.
