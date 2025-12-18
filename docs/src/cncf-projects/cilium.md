---
id: cilium
title: Cilium
---

# Cilium

## About

[Cilium](https://cilium.io/) is a CNCF Graduated project that was accepted as
an Incubating project in 2021 and graduated in 2023. It was originally created
by Isovalent. It is an advanced networking, security, and observability
solution for cloud native environments, built on top of
[eBPF](https://ebpf.io/) technology. Cilium manages network traffic in
Kubernetes clusters by dynamically injecting eBPF programs into the Linux
Kernel, enabling low-latency, high-performance communication, and enforcing
fine-grained security policies.

Key features of Cilium:

- Advanced L3-L7 security policies for fine-grained network traffic control
- Efficient, kernel-level traffic management via eBPF
- Service Mesh integration (Cilium Service Mesh)
- Support for both Kubernetes NetworkPolicy and CiliumNetworkPolicy
- Built-in observability and monitoring with Hubble

To install Cilium in your environment, follow the instructions in the documentation:
[https://docs.cilium.io/en/stable/gettingstarted/k8s-install-default/](https://docs.cilium.io/en/stable/gettingstarted/k8s-install-default/)

## Pod-to-Pod Network Security with CloudNativePG and Cilium

Kubernetes’ default behavior is to allow traffic between any two Pods in the cluster network.
Cilium provides advanced L3/L4 network security using the `CiliumNetworkPolicy` resource. This
enables fine-grained control over network traffic between Pods within a Kubernetes cluster. It is
especially useful for securing communication between application workloads and backend
services.

In the following examples, we demonstrate how Cilium can be used to secure a
CloudNativePG PostgreSQL instance by restricting ingress traffic to only
authorized Pods.

:::info[Important]
    Before proceeding, ensure that the `cluster-example` Postgres cluster is up
    and running in your environment.
:::

## Default Deny Behavior in Cilium

By default, Cilium does **not** deny all traffic unless explicitly configured
to do so. In contrast to Kubernetes NetworkPolicy, which uses a deny-by-default
model once a policy is present in a namespace, Cilium provides more flexible
control over default deny behavior.

To enforce a default deny posture with Cilium, you need to explicitly create a
policy that denies all traffic to a set of Pods unless otherwise allowed. This
is commonly achieved by using an **empty `ingress` section** in combination
with `endpointSelector`, or by enabling **`--enable-default-deny`** at the
Cilium agent level for broader enforcement.

A minimal example of a default deny policy:

```yaml
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: default-deny
  namespace: default
spec:
  description: "Default deny all ingress traffic to all Pods in this namespace"
  endpointSelector: {}
  ingress: []
```

## Making Cilium Network Policies work with CloudNativePG Operator

When working with a network policy, Cilium or not, the first step is to make
sure that the operator can reach the Pods in the target namespace. This is
important because the operator needs to be able to perform checks and actions
on the Pods, and one of those actions requires access to the port `8000` on the
Pods to get the current status of the PostgreSQL instance running inside.

The following `CiliumNetworkPolicy` allows the operator to access the Pods in
the target `default` namespace:

```yaml
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: cnpg-operator-access
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
:::info[Important]
    The `cnpg-system` namespace is the default namespace for the operator when
    using the YAML manifests. If the operator was installed using a different
    process (Helm, OLM, etc.), the namespace may be different. Make sure to adjust
    the namespace properly.
:::

## Allowing access between cluster Pods

Since the default policy is "deny all", we need to explicitly allow access
between the cluster Pods in the same namespace. We will improve our previous
policy by adding the required ingress rule:

```yaml
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: cnpg-cluster-internal-access
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

The policy allows access from `cnpg-system` Pods and from `default` namespace
Pods that also belong to `cluster-example`. The `matchLabels` selector requires
Pods to have the complete set of listed labels. Missing even one label means
the Pod will not match.

## Restricting Access to PostgreSQL with Cilium

In this example, we define a `CiliumNetworkPolicy` that allows only Pods
labeled `role=backend` in the `default` namespace to connect to a PostgreSQL
cluster named `cluster-example`. All other ingress traffic is blocked by
default.

```yaml
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: postgres-access-backend-label
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

This `CiliumNetworkPolicy` ensures that only Pods labeled with `role=backend`
can access the PostgreSQL instance managed by CloudNativePG via port 5432 in
the `default` namespace.

In the following policy, we demonstrate how to allow ingress traffic to port
5432 of a PostgreSQL cluster named `cluster-example`, only from Pods with the
label `role=backend` in any namespace.

```yaml
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: postgres-access-backend-any-ns
  namespace: default
spec:
  description: "Allow PostgreSQL access on port 5432 from Pods with role=backend in any namespace"
  endpointSelector:
    matchLabels:
      cnpg.io/cluster: cluster-example
  ingress:
    - fromEndpoints:
        - labelSelector:
            matchLabels:
              role: backend
            matchExpressions:
              - key: io.kubernetes.pod.namespace
                operator: Exists
      toPorts:
        - ports:
          - port: "5432"
            protocol: TCP
```

The following example allows ingress traffic to port 5432 of the
`cluster-example` cluster (located in the `default` namespace) from any Pods in
the `backend` namespace.

```yaml
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: postgres-access-backend-namespace
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

Using Cilium’s L3/L4 policy model, we define a `CiliumNetworkPolicy` that
explicitly allows ingress traffic to cluster Pods only from application Pods in
the `backend` namespace. All other traffic is implicitly denied unless
explicitly permitted by additional policies.

The following example allows ingress traffic to port 5432 of the
`cluster-example` cluster (located in the `default` namespace) from any source
within the Kubernetes cluster.

```yaml
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: postgres-access-cluster-wide
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

You may consider using [editor.networkpolicy.io](https://editor.networkpolicy.io/),
a visual and interactive tool that simplifies the creation and validation of
Cilium Network Policies. It’s especially helpful for avoiding misconfigurations
and understanding traffic rules more clearly by presenting in a visual way.

With these policies, you've established baseline access controls for
PostgreSQL. You can layer additional egress or audit rules using Cilium's
policy language or extend to L7 enforcement with Envoy.
