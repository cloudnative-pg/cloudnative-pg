---
id: cnpg_i
sidebar_position: 480
title: CNPG-I
---

# CNPG-I
<!-- SPDX-License-Identifier: CC-BY-4.0 -->

The **CloudNativePG Interface** ([CNPG-I](https://github.com/cloudnative-pg/cnpg-i))
is a standard way to extend and customize CloudNativePG without modifying its
core codebase.

## Why CNPG-I?

CloudNativePG supports a wide range of use cases, but sometimes its built-in
functionality isn’t enough, or adding certain features directly to the main
project isn’t practical.

Before CNPG-I, users had two main options:

- Fork the project to add custom behavior, or
- Extend the upstream codebase by writing custom components on top of it.

Both approaches created maintenance overhead, slowed upgrades, and delayed delivery of critical features.

CNPG-I solves these problems by providing a stable, gRPC-based integration
point for extending CloudNativePG at key points in a cluster’s lifecycle —such
as backups, recovery, and sub-resource reconciliation— without disrupting the
core project.

CNPG-I can extend:

- The operator, and/or
- The instance manager running inside PostgreSQL pods.

## Registering a plugin

CNPG-I is inspired by the Kubernetes
[Container Storage Interface (CSI)](https://kubernetes.io/blog/2019/01/15/container-storage-interface-ga/).
The operator communicates with registered plugins using **gRPC**, following the
[CNPG-I protocol](https://github.com/cloudnative-pg/cnpg-i/blob/main/docs/protocol.md).

CloudNativePG discovers plugins **at startup**. You can register them in one of two ways:

- Sidecar container – run the plugin inside the operator’s Deployment
- Standalone Deployment – run the plugin as a separate workload in the same
  namespace

In both cases, the plugin must be packaged as a container image.

### Sidecar Container

When running as a sidecar, the plugin must expose its gRPC server via a **Unix
domain socket**. This socket must be placed in a directory shared with the
operator container, mounted at the path set in `PLUGIN_SOCKET_DIR` (default:
`/plugin`).

Example:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: controller-manager
spec:
  template:
    spec:
      containers:
      - image: cloudnative-pg:latest
        [...]
        name: manager
        volumeMounts:
        - mountPath: /plugins
          name: cnpg-i-plugins
            
      - image: cnpg-i-plugin-example:latest
        name: cnpg-i-plugin-example
        volumeMounts:
        - mountPath: /plugins
          name: cnpg-i-plugins
      volumes:
      - name: cnpg-i-plugins
        emptyDir: {}
```

### Standalone Deployment (recommended)

Running a plugin as its own Deployment decouples its lifecycle from the
operator’s and allows independent scaling. In this setup, the plugin exposes a
TCP gRPC endpoint behind a Service, with **mTLS** for secure communication.

:::warning
    CloudNativePG does **not** discover plugins dynamically. If you deploy a new
    plugin, you must **restart the operator** to detect it.
:::

Example Deployment:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: cnpg-i-plugin-example
spec:
  template:
    [...]
    spec:
      containers:
      - name: cnpg-i-plugin-example
        image: cnpg-i-plugin-example:latest
        ports:
        - containerPort: 9090
          protocol: TCP
```

The related Service for the plugin must include:

- The label `cnpg.io/plugin: <plugin-name>` — required for CloudNativePG to
  discover the plugin
- The annotation `cnpg.io/pluginPort: <port>` — specifies the port where the
  plugin’s gRPC server is exposed

Example Service:

```yaml
apiVersion: v1
kind: Service
metadata:
  annotations:
    cnpg.io/pluginPort: "9090"
  labels:
    cnpg.io/pluginName: cnpg-i-plugin-example.my-org.io
  name: cnpg-i-plugin-example
spec:
  ports:
  - port: 9090
    protocol: TCP
    targetPort: 9090
  selector:
    app: cnpg-i-plugin-example
```

### Configuring TLS Certificates

When a plugin runs as a `Deployment`, communication with CloudNativePG happens
over the network. To secure it, **mTLS is enforced**, requiring TLS
certificates for both sides.

Certificates must be stored as [Kubernetes TLS Secrets](https://kubernetes.io/docs/concepts/configuration/secret/#tls-secrets)
and referenced in the plugin’s Service annotations
(`cnpg.io/pluginClientSecret` and `cnpg.io/pluginServerSecret`):

```yaml
apiVersion: v1
kind: Service
metadata:
  annotations:
    cnpg.io/pluginClientSecret: cnpg-i-plugin-example-client-tls
    cnpg.io/pluginServerSecret: cnpg-i-plugin-example-server-tls
    cnpg.io/pluginPort: "9090"
  name: barman-cloud
  namespace: postgresql-operator-system
spec:
    [...]
```

:::note
    You can provide your own certificate bundles, but the recommended method is
    to use [Cert-manager](https://cert-manager.io).
:::

#### Customizing the Certificate DNS Name

By default, CloudNativePG uses the Service name as the server name for TLS
verification when connecting to the plugin. If your environment requires the
certificate to have a different DNS name (e.g., `barman-cloud.svc`), you can
customize it using the `cnpg.io/pluginServerName` annotation:

```yaml
apiVersion: v1
kind: Service
metadata:
  annotations:
    cnpg.io/pluginClientSecret: cnpg-i-plugin-example-client-tls
    cnpg.io/pluginServerSecret: cnpg-i-plugin-example-server-tls
    cnpg.io/pluginPort: "9090"
    cnpg.io/pluginServerName: barman-cloud.svc
  name: barman-cloud
  namespace: postgresql-operator-system
spec:
    [...]
```

This allows the operator to verify the plugin's certificate against the
specified DNS name instead of the default Service name. The server certificate
must include this DNS name in its Subject Alternative Names (SAN).

## Using a plugin

To enable a plugin, configure the `.spec.plugins` section in your `Cluster`
resource. Refer to the CloudNativePG API Reference for the full
[PluginConfiguration](https://github.com/cloudnative-pg/cloudnative-pg/blob/main/docs/src/cloudnative-pg.v1.md#pluginconfiguration-----postgresql-cnpg-io-v1-pluginconfiguration)
specification.

Example:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-with-plugins
spec:
  instances: 1
  storage:
    size: 1Gi
  plugins:
  - name: cnpg-i-plugin-example.my-org.io
    enabled: true
    parameters:
      key1: value1
      key2: value2
```

Each plugin may have its own parameters—check the plugin’s documentation for
details. The `name` field in `spec.plugins` depends on how the plugin is
deployed:

- Sidecar container: use the Unix socket file name
- Deployment: use the value from the Service’s `cnpg.io/pluginName` label

## Community plugins

The CNPG-I protocol has quickly become a proven and reliable pattern for
extending CloudNativePG while keeping the core project maintainable.
Over time, the community has built and shared plugins that address real-world
needs and serve as examples for developers.

For a complete and up-to-date list of plugins built with CNPG-I, please refer to the
[CNPG-I GitHub page](https://github.com/cloudnative-pg/cnpg-i?tab=readme-ov-file#projects-built-with-cnpg-i).
