# CNPG-I
<!-- SPDX-License-Identifier: CC-BY-4.0 -->

The CloudNativePG Interface (CNPG-I) is a standard for extending and customizing
the Operator's functionality without modifying the core codebase.

## Why CNPG-I?

Although the Operator already covers a wide range of use cases, there are scenarios where
the default functionality may not be sufficient, but cannot be either supported directly by the community.
Prior to the introduction of CNPG-I, users had to fork the project to customise the Operator's behaviour or attempt to 
integrate changes with the upstream codebase, which could lead to maintenance challenges or delays in meeting the
business requirements.

CNPG-I was develop as a standard to integrate with key operations of the Operator during
the lifecycle management of a Cluster, such as backups, restores, or sub-resources reconciliation.

## How to register a plugin

The implementation of CNPG-I is heavily inspired by the Kubernetes
[Container Storage Interface](https://kubernetes.io/blog/2019/01/15/container-storage-interface-ga/)
(CSI). 
The Operator issues gRPC calls directly to the registered plugins, according to the interface
defined by the [CNPG-I protocol](https://github.com/cloudnative-pg/cnpg-i/blob/main/docs/protocol.md).

The Operator performs a discovery of available Plugins during startup. A workload can be registered as a
CNPG-I plugin either by configuring it as a [Sidecar Container](https://kubernetes.io/docs/concepts/workloads/pods/sidecar-containers/) 
in the Operator's Deployment or by deploying it as a standalone 
[Deployment](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/) in the same Operator namespace.
In both cases, the Plugin must be packaged as a container image to be deployed as a Kubernetes workload.

### Sidecar Container

The Plugin can be configured as a [Sidecar Container](https://kubernetes.io/docs/concepts/workloads/pods/sidecar-containers/)
in the Operator's `Deployment`. In this case, the Plugin needs to register the gRPC server as a `unix domain socket`.
The folder where the socket is created must be shared with the Operator's container and mounted in the path exposed by
the environment variable `PLUGIN_SOCKET_DIR` (default is `/plugin`).

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
        ...
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

### Standalone Deployment

Deploying the plugin as a standalone `Deployment` is the recommended approach, as it allows to decouple
the plugin's lifecycle from the Operator's one, and to scale it independently.
The container needs to expose the gRPC server implementing the CNPG-I protocol to the network through
a TCP port and a Kubernetes Service. The Service must define the label `cnpg.io/plugin: <plugin-name>`,
which is required by the Operator to discover the plugin.

The communication between the Operator and the Plugin is done over gRPC, using mTLS for security. See
the section on [Configuring TLS Certificates](#configuring-tls-certificates) for more details.

!!! Warning
    The Operator does not automatically discover new Plugins after startup. To detect and use newly deployed Plugins,
    you must restart the Operator.

Example of a Plugin as a standalone `Deployment`:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: cnpg-i-plugin-example
spec:
  template:
    ...
    spec:
      containers:
      - name: cnpg-i-plugin-example
        image: cnpg-i-plugin-example:latest
        ports:
        - containerPort: 9090
          protocol: TCP
```

The `Service` can be configured as follows:
```yaml
apiVersion: v1
kind: Service
metadata:
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

When a Plugin is configured as a standalone Deployment, the communication with the Operator occurs over the network,
and mTLS is enforced for security. This implies that TLS certificates need to be provided for both sides of
the connection.
The recommended approach to provide the certificates is by using [CertManager](https://cert-manager.io) to create and
manage them, but also providing self-provisioned ones is supported.

## How to use a plugin

Plugins can be enabled on a Cluster resource by configuring the 
[.spec.plugins](https://cloudnative-pg.io/documentation/current/cloudnative-pg.v1/#postgresql-cnpg-io-v1-PluginConfiguration)
stanza as follows:

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
    Each Plugin can support a different set of parameters, so it's recommended to refer to the Plugin's specific documentation
    to understand the available ones and their usage.

Depending on whether the Plugin is configured as a Sidecar Container or as a Deployment, the field `name`
must be populated with the Plugin's unix socket file name or the Service `cnpg.io/plugin` annotation value, respectively.


