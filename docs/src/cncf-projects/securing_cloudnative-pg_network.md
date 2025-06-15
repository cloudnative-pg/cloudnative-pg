# Securing CloudNativePG Network with CiliumAdd commentMore actions

Cilium is a CNCF Sandbox project, accepted in 2021 under the sponsorship of Isovalent. It is an
advanced networking, security, and observability solution for cloud-native environments, built on
top of eBPF (Extended Berkeley Packet Filter) technology. Cilium manages network traffic in
Kubernetes clusters by dynamically injecting eBPF programs into the Linux kernel, enabling
low-latency, high-performance communication and enforcing fine-grained security policies.

Key features of Cilium:

● Advanced L3-L7 security policies for fine-grained network traffic control
● Efficient, kernel-level traffic management via eBPF
● Service Mesh integration (Cilium Service Mesh)
● Support for both NetworkPolicy and CiliumNetworkPolicy
● Built-in observability and monitoring with Hubble

## Pod-to-Pod Network Security with CloudNativePG and Cilium

Kubernetes’ default behavior is to allow traffic between any two pods in the cluster network.
Cilium provides advanced L3/L4 network security using the CiliumNetworkPolicy resource. This
enables fine-grained control over network traffic between Pods within a Kubernetes cluster. It is
especially useful for securing communication between application workloads and backend
services.

In the following example, we define a CiliumNetworkPolicy that allows only Pods labeled
role=backend in the backend namespace to connect to a CloudNativePG-managed PostgreSQL
cluster named my-postgres. All other ingress traffic is blocked by default.

## Example: Restricting Access to PostgreSQL with Cilium

In this example, we demonstrate how Cilium can be used to secure a CloudNativePG
PostgreSQL instance by restricting ingress traffic to only authorized Pods.

In the following example, we demonstrate how to restrict access to a CloudNativePG cluster
named cluster-example so that only Pods in the backend namespace are allowed to connect.

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

This CiliumNetworkPolicy ensures that only Pods labeled with role: backend can access the
PostgreSQL instance managed by CloudNativePG via port 5432.

Using Cilium’s L3/L4 policy model, we define a CiliumNetworkPolicy that explicitly allows ingress
traffic to CloudNativePG pods only from application pods in the backend namespace. All other
traffic is implicitly denied unless explicitly permitted by additional policies.