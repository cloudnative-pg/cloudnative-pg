# Networking

CloudNativePG assumes the underlying Kubernetes cluster has the required
connectivity to work already set up.
Networking on Kubernetes is an important and extended topic, please refer to
the [Kubernetes documentation](https://kubernetes.io/docs/concepts/services-networking/) in case
you have deeper doubts on this matter.

If you're following the quickstart guide to install CloudNativePG on a local KinD or K3d cluster, is
expected that you don't hit any networking issue, this due to both platform has no network restrictions
in place by default.

However, when deploying CloudNativePG on existing infrastructure, networking setup may be in place, and thus,
affect the communication of the operator with PostgreSQL clusters.
Specifically, existing [Network Policies](https://kubernetes.io/docs/concepts/services-networking/network-policies/)
might restrict certain types of traffic.

The operator needs to reach every cluster pod on port 8000 and 5432, and also, pods needs to be able to talk
between them on the same ports.

## Cross-namespace network policy for the operator

Following the quickstart guide or using helm chart for deployment will install the operator in
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
operator namespace and pod to the cluster namespace and pods. You can find an example in the
[`networkpolicy-example.yaml`](samples/networkpolicy-example.yaml) file in this repository.
Please note, you'll need to adjust the cluster name and cluster namespace to match your specific setup,
also check the operator namespace name if is not the default one.

## Cross-cluster networking

While [bootstrapping](bootstrap.md) from another cluster or when using the `externalClusters` section,
ensure connectivity among all clusters, object stores, and namespaces involved.

Again, we refer you to the [Kubernetes documentation](https://kubernetes.io/docs/concepts/services-networking/)
for setup information.
