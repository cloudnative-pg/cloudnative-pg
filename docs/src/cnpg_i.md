# CNPG-I
<!-- SPDX-License-Identifier: CC-BY-4.0 -->

The CloudNativePG Interface (**CNPG-I**) provides a powerful standard for extending and customizing CloudNativePG's default 
functionality without altering its core codebase.

## Why CNPG-I?

While CloudNativePG effectively handles a wide range of use cases, there are scenarios where its default capabilities 
may not suffice, or where community support for specific features isn't feasible. Prior to the introduction of CNPG-I, 
users often had to fork the project to implement custom behaviors or attempt to integrate their changes directly with 
the upstream codebase. Both approaches posed significant challenges, leading to maintenance overhead and potential 
delays in meeting business requirements.

CNPG-I was developed to address these issues by offering a standardized way to integrate with key CloudNativePG 
operations throughout a Cluster's lifecycle. This includes critical functions like backups, restores, and sub-resource
reconciliation, enabling seamless customization and extending CloudNativePG's capabilities without disrupting the
core project.

CNPG-I can extend the operator and/or the instance manager capabilities.

## How to register a plugin for the operator

The implementation of CNPG-I is heavily inspired by the Kubernetes
[Container Storage Interface](https://kubernetes.io/blog/2019/01/15/container-storage-interface-ga/)
(CSI). 
The Operator issues gRPC calls directly to the registered plugins,  adhering to the interface
defined by the [CNPG-I protocol](https://github.com/cloudnative-pg/cnpg-i/blob/main/docs/protocol.md).

CloudNativePG discovers available plugins during its startup process. Plugins can be registered in one of two ways:

- **Sidecar Container**: Configure the plugin as a 
[Sidecar Container](https://kubernetes.io/docs/concepts/workloads/pods/sidecar-containers/) within the CloudNativePG's
Deployment.

- **Deployment**: Deploy the plugin as an independent 
[Deployment](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/) in the same namespace as the CloudNativePG.

In both cases, the plugin must be packaged as a container image to be deployed as a Kubernetes workload.

### Sidecar Container

When configuring a plugin as a Sidecar Container within the CloudNativePG's Deployment, the plugin must register its gRPC 
server as a **Unix domain socket**. The directory where this socket is created must be shared with the Operator's container 
and mounted to the path specified by the `PLUGIN_SOCKET_DIR` environment variable (which defaults to `/plugin`).

Example of a Plugin as a Sidecar Container:

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

### Deployment

Deploying a plugin as a Deployment is the recommended approach. This method offers several advantages,
including decoupling the plugin's lifecycle from the CloudNativePG Operator's and allowing for independent scaling of
the plugin.

In this setup, the container must expose the gRPC server implementing the CNPG-I protocol to the network via a `TCP` 
port and a Service. Communication between CloudNativePG and the plugin is secured using **mTLS over gRPC**. 
For detailed information on configuring TLS certificates, refer to the
[Configuring TLS Certificates](#configuring-tls-certificates) section below.

!!! Warning
    CloudNativePG does not automatically discover newly deployed plugins after startup.
    To detect and utilize new plugins, you must restart the Operator's Deployment.

Example of a Plugin as a `Deployment`:

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

- The label `cnpg.io/plugin: <plugin-name>`, which is essential for CloudNativePG to discover the plugin.
- The annotation `cnpg.io/pluginPort: <port>`, specifying the port on which the plugin's gRPC server is exposed.

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

When a plugin is deployed as a Deployment, communication with CloudNativePG happens over the network. To
ensure security, **mTLS is enforced**, requiring TLS certificates for both sides of the connection.
These certificates must be stored as
[Kubernetes TLS Secrets](https://kubernetes.io/docs/concepts/configuration/secret/#tls-secrets) and referenced in the 
plugin's Service using the annotations `cnpg.io/pluginClientSecret` and `cnpg.io/pluginServerSecret`:

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

!!! Note
    While providing self-provisioned certificate bundles is supported, the recommended approach for managing certificates 
    is by using [Certmanager](https://cert-manager.io).

## How to use a plugin
Plugins are enabled on a `Cluster` resource by configuring the `.spec.plugins` stanza. Refer to the CloudNativePG 
API Reference for the full 
[PluginConfiguration](https://cloudnative-pg.io/documentation/current/cloudnative-pg.v1/#postgresql-cnpg-io-v1-PluginConfiguration)
specification.

Here's an example of how to enable a plugin on a `Cluster` resource:

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
!!! Note
    Each plugin may support a unique set of parameters. Always consult the plugin's specific documentation to understand 
    the available parameters and their proper usage.

The `name` field in the `spec.plugins` items must be populated based on how the plugin is configured:

- If the plugin is a [Sidecar Container](#sidecar-container), use the Unix socket file name.
- If the plugin is a [Deployment](#deployment), use the value of the Service's
`cnpg.io/pluginName` label.


## Community plugins

The CNPG-I protocol has established as a solid pattern for extending CloudNativePG's functionality
and to improve its maintainability long-term. As a result, the Community itself has used it to address a set
of challenges that have emerged over the years, guiding the development of some plugins and providing them with
direct support.

### Barman Cloud

The [Barman Cloud Plugin](https://github.com/cloudnative-pg/plugin-barman-cloud) implements CNPG-I
to performs Backup, Restore and WAL Archiving operations using
[Barman Cloud](https://docs.pgbarman.org/release/3.12.1/user_guide/barman_cloud.html).

Historically, CloudNativePG integrated Barman Cloud directly (**in-tree**), meaning the Barman utilities had to be installed
and bundled within the [CloudNativePG Postgres container images](https://github.com/cloudnative-pg/postgres-containers).
This approach presented significant challenges for both extensibility and maintainability. It made it difficult to
update Barman Cloud independently of the Postgres containers and limited the ability to add support of other backup
and restore tools.

!!! Important
    The in-tree support for Barman Cloud is **deprecated** as of CloudNativePG version 1.26 and will be **removed in a
    future release**.

### Hello World

The [Hello World Plugin](https://github.com/cloudnative-pg/cnpg-i-hello-world) is a project that serves as a
starting point for developers looking to create their own CNPG-I plugins. It provides a simple implementation
of some of the protocol APIs, allowing users to understand how to create a plugin and how to interact with it.
