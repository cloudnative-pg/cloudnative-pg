---
id: resource_management
sidebar_position: 130
title: Resource management
---

# Resource management
<!-- SPDX-License-Identifier: CC-BY-4.0 -->

In a typical Kubernetes cluster, pods run with unlimited resources. By default,
they might be allowed to use as much CPU and RAM as needed.

CloudNativePG allows administrators to control and manage resource usage by the pods of the cluster,
through the `resources` section of the manifest, with two knobs:

- `requests`: initial requirement
- `limits`: maximum usage, in case of dynamic increase of resource needs

For example, you can request an initial amount of RAM of 32MiB (scalable to 128MiB) and 50m of CPU (scalable to 100m)
as follows:

```yaml
  resources:
    requests:
      memory: "32Mi"
      cpu: "50m"
    limits:
      memory: "128Mi"
      cpu: "100m"
```

Memory requests and limits are associated with containers, but it is useful to think of a pod as having a memory request
and limit. The pod's memory request is the sum of the memory requests for all the containers in the pod.

Pod scheduling is based on requests and not on limits. A pod is scheduled to run on a Node only if the Node has enough
available memory to satisfy the pod's memory request.

For each resource, we divide containers into 3 Quality of Service (QoS) classes, in decreasing order of priority:

- *Guaranteed*
- *Burstable*
- *Best-Effort*

For more details, please refer to the ["Configure Quality of Service for Pods"](https://kubernetes.io/docs/tasks/configure-pod-container/quality-service-pod/#qos-classes)
section in the Kubernetes documentation.

For a PostgreSQL workload it is recommended to set a "Guaranteed" QoS.

:::info
    When the quality of service is set to "Guaranteed", CloudNativePG sets the
    `PG_OOM_ADJUST_VALUE` for the `postmaster` process to `0`, in line with the
    [PostgreSQL documentation](https://www.postgresql.org/docs/current/kernel-resources.html#LINUX-MEMORY-OVERCOMMIT).
    This allows the `postmaster` to retain its low Out-Of-Memory (OOM) score of
    `-997`, while its child processes run with an OOM score adjustment of `0`. As a
    result, if the OOM killer is triggered, it will terminate the child processes
    before the `postmaster`. This behavior helps keep the PostgreSQL instance
    alive for as long as possible and enables a clean shutdown procedure in the
    event of an eviction.
:::

To avoid resources related issues in Kubernetes, we can refer to the best practices for "out of resource" handling
while creating a cluster:

-  Specify your required values for memory and CPU in the resources section of the manifest file.
   This way, you can avoid the `OOM Killed` and `CPU throttle` or any other
   resource-related issues on running instances.
-  For your cluster's pods to get assigned to the "Guaranteed" QoS class, you
   must set limits and requests
   for both memory and CPU to the same value.
-  Specify your required PostgreSQL memory parameters consistently with the pod resources (as you would do
   in a VM or physical machine scenario - see below).
-  Set up database server pods on a dedicated node using nodeSelector.
   See the "nodeSelector" and "tolerations" fields of the
   [“affinityconfiguration"](cloudnative-pg.v1.md#affinityconfiguration) resource on the API reference page.

You can refer to the following example manifest:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: postgresql-resources
spec:

  instances: 3

  postgresql:
    parameters:
      shared_buffers: "256MB"

  resources:
    requests:
      memory: "1024Mi"
      cpu: 1
    limits:
      memory: "1024Mi"
      cpu: 1

  storage:
    size: 1Gi
```

In the above example, we have specified `shared_buffers` parameter with a value of `256MB` - i.e., how much memory is
dedicated to the PostgreSQL server for caching data (the default value for this parameter is `128MB` in case
it's not defined).

A reasonable starting value for `shared_buffers` is 25% of the memory in your system.
For example: if your `shared_buffers` is 256 MB, then the recommended value for your container memory size is 1 GB,
which means that within a pod all the containers will have a total of 1 GB memory that Kubernetes will always preserve,
enabling our containers to work as expected.
For more details, please refer to the ["Resource Consumption"](https://www.postgresql.org/docs/current/runtime-config-resource.html)
section in the PostgreSQL documentation.

:::note[Managing Compute Resources for Containers]
    For more details on resource management, please refer to the
    ["Managing Compute Resources for Containers"](https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/)
    page from the Kubernetes documentation.
:::

## In-place resource updates

By default, changing the `resources` section of a `Cluster` triggers a rolling
update of the instances: every pod is recreated with the new resources, and the
primary is updated last through a switchover (or a restart, depending on
`primaryUpdateMethod`).

Starting from Kubernetes 1.33, pods can be resized in place through the
[resize subresource](https://kubernetes.io/docs/tasks/configure-pod-container/resize-container-resources/),
without recreating them. CloudNativePG can take advantage of this feature when
you set the `resourcesUpdateStrategy` field to `inPlace`:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: postgresql-resize-inplace
spec:
  instances: 3

  resourcesUpdateStrategy: inPlace

  resources:
    requests:
      memory: "1Gi"
      cpu: 1
    limits:
      memory: "1Gi"
      cpu: 1

  storage:
    size: 1Gi
```

With this strategy, when the only difference between a running instance and
its desired specification is in the container resources, the operator applies
the change through the resize subresource: CPU and memory of the running
containers are updated with no restart, no switchover, and no interruption of
the client connections. This applies to the `postgres` container and to any
sidecar container injected by CNPG-I plugins.

The default value of the field is `recreate`, which preserves the rolling
update behavior described above.

### When a rolling update is still used

The `inPlace` strategy is best-effort by design: whenever the change cannot be
applied in place, the operator falls back to the standard rolling update.
This happens when:

- The Kubernetes cluster does not expose the resize subresource of the pods
  (the feature requires Kubernetes 1.33 or later, and became stable in 1.35).
- A **memory limit is decreased**. The kubelet applies a memory limit
  decrease only when the current usage of the container is below the new
  limit, and PostgreSQL keeps its shared memory (`shared_buffers` above all)
  allocated for the whole life of the instance, so on a warmed-up instance
  such a resize would remain pending indefinitely, without any error being
  reported. Even if the usage momentarily dropped below the target and the
  decrease were applied, the instance would then be running with a memory
  configuration sized for the old limit inside a smaller cgroup, exposed to
  an out-of-memory termination at the next spike of activity. A fresh pod
  starting under the new limit, with memory parameters reviewed accordingly,
  is the deterministic option.
- Resource entries are added or removed, rather than changed: Kubernetes does
  not allow a resize to add or remove `requests` and `limits` entries.
- Resources other than `cpu` and `memory` (for example, hugepages) are
  changed.
- The change would alter the pod QoS class, which Kubernetes forbids: the API
  server rejects the resize and the operator recreates the pod.
- The kubelet reports the resize as *infeasible* because the node can never
  satisfy the new request: the operator recreates the pod, letting the
  scheduler place it on a suitable node.
- The resources change together with any other part of the pod specification:
  the pod is recreated to apply the whole change.

Run-once init containers cannot be resized and have already terminated when a
resize takes place, so their recorded resources are left untouched: they will
pick up the new values the next time the pod is naturally recreated.

:::warning
With `resourcesUpdateStrategy: inPlace`, the operator treats the resources of
the running pods as fully declarative: a resize applied to an instance pod by
any other actor is reverted to match `spec.resources` of the `Cluster`. Do not
point an in-place vertical autoscaler at the instance pods of a cluster using
this strategy.
:::

Remember that PostgreSQL does not adapt its memory configuration to the
container limits: increasing the memory of the instances does not change
`shared_buffers` or the other memory-related parameters, which you should
review and update consistently with the new resources. Note that changing
`shared_buffers` requires a restart of PostgreSQL.

## Integration with the Vertical Pod Autoscaler (VPA)

The `Cluster` CRD exposes the `scale` subresource together with the label
selector for its instance pods. This makes a `Cluster` a valid `targetRef` for the
[Vertical Pod Autoscaler](https://github.com/kubernetes/autoscaler/tree/master/vertical-pod-autoscaler) <!-- wokeignore:rule=master -->
(VPA), so VPA can observe the instance pods and emit CPU and memory
recommendations for them.

We recommend running VPA in **recommendation-only** mode
(`updatePolicy.updateMode: Off`). In this mode VPA only reports its
recommendations, which an operator can then apply to `spec.resources` of the
`Cluster` through a normal manifest update; CloudNativePG performs the rolling
update of the instances using its usual switchover-aware procedure. VPA
produces a recommendation per container: use the one for the `postgres`
container, since that is what `spec.resources` configures.

Example targeting a `Cluster`:

```yaml
apiVersion: autoscaling.k8s.io/v1
kind: VerticalPodAutoscaler
metadata:
  name: cluster-example-vpa
spec:
  targetRef:
    apiVersion: postgresql.cnpg.io/v1
    kind: Cluster
    name: cluster-example
  updatePolicy:
    updateMode: "Off"
```

:::warning
Do not use `updateMode: Auto`, `Recreate`, `InPlaceOrRecreate`, or `Initial`
against a CloudNativePG-managed `Cluster`. The operator owns the pod
specification and treats `spec.resources` of the `Cluster` as the source of
truth: it does not adopt the resources VPA writes onto a running pod, so the
live pod and the declared `spec.resources` silently diverge (and with
`resourcesUpdateStrategy: inPlace` the operator actively reverts them). Any tuning you sized against the
declared resources (for example a `shared_buffers` value the operator
validated against the memory request) no longer matches the pod's actual
limits. VPA also evicts pods through the Kubernetes eviction API, bypassing
the operator's switchover-aware sequencing; the default primary
`PodDisruptionBudget` then blocks eviction of the primary, so VPA stalls
instead of completing. Apply the VPA recommendations to the `Cluster` manifest
manually instead.
:::

## Integration with the Horizontal Pod Autoscaler (HPA)

The `scale` subresource also exposes `spec.instances`, so a
[Horizontal Pod Autoscaler](https://kubernetes.io/docs/tasks/run-application/horizontal-pod-autoscale/)
(HPA) can technically change the number of instances in a `Cluster`.
In practice this is **not recommended** for a PostgreSQL cluster, for several
reasons.

Scaling a `Cluster` only adds or removes standby replicas; it never relieves
write load on the primary. The selector exposed through the scale subresource
matches the primary and all replicas, which have opposite workload profiles
(write-heavy primary, read-only replicas), so the per-pod average that HPA
computes from a CPU or memory metric is not a meaningful signal for any of
them. Adding a replica only dilutes that average further without addressing a
hot primary.

HPA is also unaware of CloudNativePG's own constraints on `spec.instances`. If
you configure synchronous replicas, a scale-down below `maxSyncReplicas + 1`
instances is rejected by the validating webhook, and HPA keeps retrying a
value it cannot satisfy.
If you nonetheless drive `spec.instances` from an HPA, base it on a custom
metric that actually reflects read replica load, and set `minReplicas` above
the synchronous-replica floor. Review the impact on replication and quorum
carefully first.
