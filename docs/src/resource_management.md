# Resource management

In a typical Kubernetes cluster, pods run with unlimited resources. By default,
they might be allowed to use as much CPU and RAM as needed.

CloudNativePG allows administrators to control and manage resource usage by the pods of the cluster,
You accomplish this in the `resources` section of the manifest using two knobs:

- `requests`: Initial requirement
- `limits`: Maximum usage, in case of dynamic increase of resource needs

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

Memory requests and limits are associated with containers, but it's useful to think of a pod as having a memory request
and limit. The pod's memory request is the sum of the memory requests for all the containers in the pod.

Pod scheduling is based on requests and not on limits. A pod is scheduled to run on a node only if the node has enough
available memory to satisfy the pod's memory request.

For each resource, we divide containers into three quality-of-service (QoS) classes, in decreasing order of priority:

- Guaranteed
- Burstable
- Best-effort

For more details, see [Configure Quality of Service for Pods](https://kubernetes.io/docs/tasks/configure-pod-container/quality-service-pod/#qos-classes)
in the Kubernetes documentation.

For a PostgreSQL workload, we recommend setting a Guaranteed QoS.

To avoid resources-related issues in Kubernetes, refer to the best practices for out-of-resource handling
while creating a cluster:

-  Specify your required values for memory and CPU in the resources section of the manifest file.
   This way, you can avoid the `OOM Killed` (where OOM stands for *out of memory*) and `CPU throttle` or any other
   resource-related issues on running instances.
-  For your cluster's pods to get assigned to the Guaranteed QoS class, you must set limits and requests
   for both memory and CPU to the same value.
-  Specify your required PostgreSQL memory parameters consistently with the pod resources (as you would
   in a VM or physical machine scenario).
-  Set up database server pods on a dedicated node using `nodeSelector`.
   See the `nodeSelector` and `tolerations` fields of the
   [`affinityconfiguration`](cloudnative-pg.v1.md#postgresql-cnpg-io-v1-AffinityConfiguration) resource on the API reference page.

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

This example specifies the `shared_buffers` parameter with a value of `256MB`. This number represents how much memory is
dedicated to the PostgreSQL server for caching data. (The default value for this parameter is `128MB`.)

A reasonable starting value for `shared_buffers` is 25% of the memory in your system.
For example, suppose your `shared_buffers` is 256MB. The recommended value for your container memory size is 1GB.
This value means that, within a pod, all the containers have a total of 1GB memory that Kubernetes always preserves.
This behavior enables our containers to work as expected.
For more details, see [Resource Consumption](https://www.postgresql.org/docs/current/runtime-config-resource.html)
in the PostgreSQL documentation.

!!! Seealso "Managing Compute Resources for Containers"
    For more details on resource management, see
    [Managing Compute Resources for Containers](https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/).
