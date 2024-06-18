# Service Management

A PostgreSQL cluster should only be accessed via standard Kubernetes network
services directly managed by CloudNativePG. For more details, refer to the
["Service" page of the Kubernetes Documentation](https://kubernetes.io/docs/concepts/services-networking/service/#virtual-ips-and-service-proxies).

CloudNativePG defines three types of services for each `Cluster` resource:

* `rw`: Points to the primary instance of the cluster (read/write).
* `ro`: Points to the replicas, where available (read-only).
* `r`: Points to any PostgreSQL instance in the cluster (read).

By default, CloudNativePG creates all of the above services for a `Cluster`
resource, with the following conventions:

- The name of the service follows this format: `<CLUSTER_NAME>-<SERVICE_NAME>`.
- All services are of type `ClusterIP`.

!!! Important
    Default service names are reserved for CloudNativePG usage.

While this setup covers most use cases for accessing PostgreSQL within the same
Kubernetes cluster, CloudNativePG offers flexibility to:

- Disable the creation of any or all default services.
- Define your own services using the standard `Service` API provided by
  Kubernetes.

A common scenario arises when using CloudNativePG in database-as-a-service
(DBaaS) contexts, where access to the database from outside the Kubernetes
cluster is required. In such cases, you can disable the default services and
create your own service of type `LoadBalancer`, if available in your Kubernetes
environment.

## Disabling Default Services

You can disable any or all default services through the
[`managed.services.disabledDefaultServices` option](cloudnative-pg.v1.md#postgresql-cnpg-io-v1-ManagedServices).

For example, if you want to remove both the `ro` (read-only) and `r` (read)
services, you can use this configuration:

```yaml
# <snip>
managed:
  services:
    disabledDefaultServices: ["ro", "r"]
```

## Adding Your Own Services

!!! Important
    When defining your own services, you cannot use any of the default reserved
    service names that follow the convention `<CLUSTER_NAME>-<SERVICE_NAME>`.
    It is your responsibility to pick a unique name for the service in the
    Kubernetes namespace.

You can define a list of additional services through the
[`managed.services.additional` stanza](cloudnative-pg.v1.md#postgresql-cnpg-io-v1-ManagedService)
by specifying the service type (e.g., `rw`) in the `selectorType` field.

The `serviceTemplate` field gives you access to the standard Kubernetes API for
the network `Service` resource, allowing you to define both the `metadata` and
the `spec` sections as you like.

<!--
TODO: shall we mention that CloudNativePG manages the `selector` or `ports` parts of the service?
-->

For example, if you want to have a single `LoadBalancer` service for your
PostgreSQL database primary, you can use the following excerpt:

```yaml
managed:
  services:
    # Disable all default services
    disabledDefaultServices: ["rw", "ro", "r"]
    additional:
      - selectorType: rw
        serviceTemplate:
          metadata:
            name: "mydb-lb"
            labels:
              test-label: "true"
            annotations:
              test-annotation: "true"
          spec:
            type: LoadBalancer
```

The above example also shows how to set metadata such as annotations and labels
for the created service.

### About Exposing Postgres Services

There are primarily three use cases for exposing your PostgreSQL service
outside your Kubernetes cluster:

- Temporarily, for testing.
- Permanently, for **DBaaS purposes**.
- Prolonged period/permanently, for **legacy applications** that cannot be
  easily or sustainably containerized and need to reside in a virtual machine
or physical machine outside Kubernetes. This use case is very similar to DBaaS.

Be aware that allowing access to a database from the public network could
expose your database to potential attacks from malicious users.

!!! Warning
    Ensure you secure your database before granting external access, or make
    sure your Kubernetes cluster is only reachable from a private network.

