# Cilium

[Cilium](https://cilium.io/) is a CNCF Graduated project that was accepted as an Incubating project in 2021 and graduated in 2023 under the sponsorship of Isovalent. It is an
advanced networking, security, and observability solution for cloud-native environments, built on
top of eBPF (Extended Berkeley Packet Filter) technology. Cilium manages network traffic in
Kubernetes clusters by dynamically injecting eBPF programs into the Linux Kernel, enabling
low-latency, high-performance communication and enforcing fine-grained security policies.

Key features of Cilium:

- Advanced L3-L7 security policies for fine-grained network traffic control
- Efficient, kernel-level traffic management via eBPF
- Service Mesh integration (Cilium Service Mesh)
- Support for both NetworkPolicy and CiliumNetworkPolicy
- Built-in observability and monitoring with Hubble

## Pod-to-Pod Network Security with CloudNativePG and Cilium

Kubernetes’ default behavior is to allow traffic between any two pods in the cluster network.
Cilium provides advanced L3/L4 network security using the CiliumNetworkPolicy resource. This
enables fine-grained control over network traffic between Pods within a Kubernetes cluster. It is
especially useful for securing communication between application workloads and backend
services.

In the following examples, we demonstrate how Cilium can be used to secure a CloudNativePG PostgreSQL instance by restricting ingress traffic to only authorized Pods.

!!! Important
    Before proceeding, ensure that the `cluster-example` Postgres cluster is up and running in your environment.

## Making Cilium Network Policies work with CloudNativePG Operator

When working with a network policy, Cilium or not, the first step is to make sure that the operator can reach the pods
in the target namespace. This is important because the operator needs to be able to perform checks and actions on the pods,
and one of those actions requires to access the port `8000` on the pods to get the current status of the PostgreSQL
instance running inside.

The following CiliumNetworkPolicy allows the operator to access the pods in the target `default` namespace

```yaml
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: cnpg-operator-policy
  namespace: default
spec:
  description: "Allow CloudNativePG operator access to any pod in the target namespace"
  endpointSelector: {}
  ingress:
    - fromEndpoints:
        - matchLabels:
            io.kubernetes.pod.namespace: cnpg-system
      toPorts:
        - ports:
            - port: "8000"
              protocol: TCP
```
!!! Important
    The `cnpg-system` namespace is the default namespace for the operator when using the YAML manifests, if the operator
    was installed using a different process(Helm,OLM, etc.), the namespace may be different. Make sure to adjust the
    namespace properly.

## Allowing access between cluster pods

Since the default policy is deny all, we need to explicitly allow access between the cluster pods in the same namespace.
We will improve our previous policy by adding the required ingress rule

```yaml
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: cnpg-policy
  namespace: default
spec:
  description: "Allow CloudNativePG operator access and connection between pods in the same namespace"
  endpointSelector: {}
  ingress:
    - fromEndpoints:
        - matchLabels:
            io.kubernetes.pod.namespace: cnpg-system
        - matchLabels:
            io.kubernetes.pod.namespace: default
            cnpg.io/cluster: cluster-example
      toPorts:
        - ports:
            - port: "8000"
              protocol: TCP
            - port: "5432"
              protocol: TCP
```

## Example: Restricting Access to PostgreSQL with Cilium

In this example, we define a CiliumNetworkPolicy that allows only Pods labeled `role=backend` in the default namespace
to connect to a PostgreSQL cluster named cluster-example. All other ingress traffic is blocked by default.

```yaml
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: postgres-policy
  namespace: default
spec:
  description: "Allow PostgreSQL access on port 5432 from Pods with role=backend"
  endpointSelector:
    matchLabels:
      cnpg.io/cluster: cluster-example
  ingress:
    - fromEndpoints:
       - matchLabels:
            role: backend
      toPorts:
        - ports:
          - port: "5432"
            protocol: TCP
```

This CiliumNetworkPolicy ensures that only Pods labeled with `role=backend` can access the
PostgreSQL instance managed by CloudNativePG via port 5432 in the default namespace.

In the following example, we demonstrate how to allow ingress traffic to port 5432 of a CloudNativePG cluster named
cluster-example, only from Pods with the label `role=backend` in any namespace.

```yaml
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: postgres-policy
  namespace: default
spec:
  description: "Allow PostgreSQL access on port 5432 from Pods with role=backend in any namespace"
  endpointSelector:
    matchLabels:
      cnpg.io/cluster: cluster-example
  ingress:
    - fromEndpoints:
       - matchLabels:
            role: backend
         matchExpressions:
          - key: io.kubernetes.pod.namespace
            operator: Exists
      toPorts:
        - ports:
          - port: "5432"
            protocol: TCP
```

The following example allows ingress traffic to port 5432 of the cluster-example CloudNativePG cluster (located in the
default namespace) from any Pods in the backend namespace.

```yaml
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: postgres-policy
  namespace: default
spec:
  description: "Allow PostgreSQL access on port 5432 from any Pods in the backend namespace"
  endpointSelector:
    matchLabels:
      cnpg.io/cluster: cluster-example
  ingress:
    - fromEndpoints:
        - matchLabels:
            io.kubernetes.pod.namespace: backend
      toPorts:
        - ports:
            - port: "5432"
              protocol: TCP
```

Using Cilium’s L3/L4 policy model, we define a CiliumNetworkPolicy that explicitly allows ingress
traffic to CloudNativePG pods only from application pods in the backend namespace. All other
traffic is implicitly denied unless explicitly permitted by additional policies.

The following example allows ingress traffic to port 5432 of the cluster-example CloudNativePG cluster (located in the
default namespace) from any source within the Kubernetes cluster.

```yaml
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: postgres-policy
  namespace: default
spec:
  description: "Allow ingress traffic to port 5432 of the cluster-example from any pods within the Kubernetes cluster"
  endpointSelector:
    matchLabels:
      cnpg.io/cluster: cluster-example
  ingress:
    - fromEntities:
        - cluster
      toPorts:
        - ports:
            - port: "5432"
              protocol: TCP
```

You may consider using [editor.networkpolicy.io](https://editor.networkpolicy.io/), a visual and interactive tool that simplifies the creation and
validation of Cilium Network Policies. It’s especially helpful for avoiding misconfigurations and understanding traffic
rules more clearly by presenting in a visual way.
