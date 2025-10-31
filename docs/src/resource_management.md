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

!!! Info
    When the quality of service is set to "Guaranteed", CloudNativePG sets the
    `PG_OOM_ADJUST_VALUE` for the `postmaster` process to `0`, in line with the
    [PostgreSQL documentation](https://www.postgresql.org/docs/current/kernel-resources.html#LINUX-MEMORY-OVERCOMMIT).
    This allows the `postmaster` to retain its low Out-Of-Memory (OOM) score of
    `-997`, while its child processes run with an OOM score adjustment of `0`. As a
    result, if the OOM killer is triggered, it will terminate the child processes
    before the `postmaster`. This behavior helps keep the PostgreSQL instance
    alive for as long as possible and enables a clean shutdown procedure in the
    event of an eviction.

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
   [“affinityconfiguration"](cloudnative-pg.v1.md#postgresql-cnpg-io-v1-AffinityConfiguration) resource on the API reference page.

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

## In-place Resource Updates

CloudNativePG supports in-place resource updates for CPU and memory changes when the container resize policy allows it. This feature enables you to scale your PostgreSQL cluster resources without recreating pods, improving cluster availability during scaling operations.

### Container Resize Policy

To enable in-place resource updates, you need to configure the `containerResizePolicy` in your cluster specification. This policy defines how resource changes should be handled for each container.

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: postgresql-resize
spec:
  instances: 3
  
  containerResizePolicy:
    - resourceName: cpu
      restartPolicy: NotRequired
    - resourceName: memory
      restartPolicy: NotRequired

  resources:
    requests:
      memory: "1Gi"
      cpu: "500m"
    limits:
      memory: "2Gi"
      cpu: "1000m"
```

### Resize Policy Options

- `NotRequired`: The container can be resized in-place without restarting
- `RestartContainer`: The container will be restarted when resources are changed

### How It Works

1. When you update the `resources` section in your cluster specification, CloudNativePG checks if the change can be applied in-place
2. For CPU and memory changes with `NotRequired` resize policy, the operator applies the resource update using Kubernetes' resize subresource
3. The pod's resource allocation is updated without recreating the container
4. The operator updates the pod's spec annotation to reflect the new desired state

### Benefits

- **Zero downtime**: Resource scaling without pod recreation
- **Improved availability**: No interruption to database connections
- **Faster scaling**: Immediate resource allocation without waiting for pod recreation
- **Better user experience**: Seamless resource adjustments

### Limitations

- Only CPU and memory resources can be updated in-place
- Requires Kubernetes 1.27+ for full support
- Container resize policy must be set to `NotRequired` for in-place updates
- Some resource changes may still require pod recreation (e.g., storage, security contexts)

!!! Seealso "Managing Compute Resources for Containers"
    For more details on resource management, please refer to the
    ["Managing Compute Resources for Containers"](https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/)
    page from the Kubernetes documentation.
