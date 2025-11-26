---
id: service_management
sidebar_position: 210
title: Service management
---

# Service management
<!-- SPDX-License-Identifier: CC-BY-4.0 -->

A PostgreSQL cluster should only be accessed via standard Kubernetes network
services directly managed by CloudNativePG. For more details, refer to the
["Service" page of the Kubernetes Documentation](https://kubernetes.io/docs/concepts/services-networking/service/#virtual-ips-and-service-proxies).

CloudNativePG defines three types of services for each `Cluster` resource:

* `rw`: Points to the primary instance of the cluster (read/write).
* `ro`: Points to the replicas, where available (read-only).
* `r`: Points to any PostgreSQL instance in the cluster (read).

By default, CloudNativePG creates all the above services for a `Cluster`
resource, with the following conventions:

- The name of the service follows this format: `<CLUSTER_NAME>-<SERVICE_NAME>`.
- All services are of type `ClusterIP`.

:::info[Important]
    Default service names are reserved for CloudNativePG usage.
:::

While this setup covers most use cases for accessing PostgreSQL within the same
Kubernetes cluster, CloudNativePG offers flexibility to:

- Disable the creation of the `ro` and/or `r` default services.
- Define your own services using the standard `Service` API provided by
  Kubernetes.

You can mix these two options.

A common scenario arises when using CloudNativePG in database-as-a-service
(DBaaS) contexts, where access to the database from outside the Kubernetes
cluster is required. In such cases, you can create your own service of type
`LoadBalancer`, if available in your Kubernetes environment.

## Disabling Default Services

You can disable any or all of the `ro` and `r` default services through the
[`managed.services.disabledDefaultServices` option](cloudnative-pg.v1.md#postgresql-cnpg-io-v1-ManagedServices).

:::info[Important]
    The `rw` service is essential and cannot be disabled because CloudNativePG
    relies on it to ensure PostgreSQL replication.
:::

For example, if you want to remove both the `ro` (read-only) and `r` (read)
services, you can use this configuration:

```yaml
# <snip>
managed:
  services:
    disabledDefaultServices: ["ro", "r"]
```

## Adding Your Own Services

:::info[Important]
    When defining your own services, you cannot use any of the default reserved
    service names that follow the convention `<CLUSTER_NAME>-<SERVICE_NAME>`. It is
    your responsibility to pick a unique name for the service in the Kubernetes
    namespace.
:::

You can define a list of additional services through the
[`managed.services.additional` stanza](cloudnative-pg.v1.md#postgresql-cnpg-io-v1-ManagedService)
by specifying the service type (e.g., `rw`) in the `selectorType` field
and optionally the `updateStrategy`.

The `serviceTemplate` field gives you access to the standard Kubernetes API for
the network `Service` resource, allowing you to define both the `metadata` and
the `spec` sections as you like.

You must provide a `name` to the service and avoid defining the `selector`
field, as it is managed by the operator.

:::warning
    Service templates give you unlimited possibilities in terms of configuring
    network access to your PostgreSQL database. This translates into greater
    responsibility on your end to ensure that services work as expected.
    CloudNativePG has no control over the service configuration, except honoring
    the selector.
:::

The `updateStrategy` field allows you to control how the operator
updates a service definition. By default, the operator uses the `patch`
strategy, applying changes directly to the service.
Alternatively, the `replace` strategy deletes the existing service and
recreates it from the template.

:::warning
    The `replace` strategy will cause a service disruption with every
    change.  However, it may be necessary for modifying certain
    parameters that can only be set during service creation.
:::

For example, if you want to have a single `LoadBalancer` service for your
PostgreSQL database primary, you can use the following excerpt:

```yaml
# <snip>
managed:
  services:
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

:::warning
    Ensure you secure your database before granting external access, or make
    sure your Kubernetes cluster is only reachable from a private network.
:::