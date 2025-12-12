---
id: instance_manager
sidebar_position: 110
title: Postgres Instance Manager
---

# Postgres instance manager
<!-- SPDX-License-Identifier: CC-BY-4.0 -->

CloudNativePG does not rely on an external tool for failover management.
It simply relies on the Kubernetes API server and a native key component called:
the **Postgres instance manager**.

The instance manager takes care of the entire lifecycle of the PostgreSQL
server process (also known as `postmaster`).

When you create a new cluster, the operator makes a Pod per instance.
The field `.spec.instances` specifies how many instances to create.

Each Pod will start the instance manager as the parent process (PID 1) for the
main container, which in turn runs the PostgreSQL instance. During the lifetime
of the Pod, the instance manager acts as a backend to handle the
[startup, liveness and readiness probes](https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/#container-probes).

## Startup, Liveness, and Readiness Probes

CloudNativePG leverages [PostgreSQL's `pg_isready`](https://www.postgresql.org/docs/current/app-pg-isready.html)
to implement Kubernetes startup, liveness, and readiness probes.

### Startup Probe

The startup probe ensures that a PostgreSQL instance, whether a primary or
standby, has fully started according to `pg_isready`.
While the startup probe is running, the liveness and readiness probes remain
disabled. Following Kubernetes standards, if the startup probe fails, the
kubelet will terminate the container, which will then be restarted.

The startup probe provided by CloudNativePG is configurable via the
parameter `.spec.startDelay`, which specifies the maximum time, in seconds,
allowed for the startup probe to succeed. At a minimum, the probe requires
`pg_isready` to return `0` or `1`.

By default, the `startDelay` is set to `3600` seconds. It is recommended to
adjust this setting based on the time PostgreSQL needs to fully initialize in
your specific environment.

:::warning
    Setting `.spec.startDelay` too low can cause the liveness probe to activate
    prematurely, potentially resulting in unnecessary Pod restarts if PostgreSQL
    hasn’t fully initialized.
:::

CloudNativePG configures the startup probe with the following default parameters:

```yaml
failureThreshold: FAILURE_THRESHOLD
periodSeconds: 10
successThreshold: 1
timeoutSeconds: 5
```

The `failureThreshold` value is automatically calculated by dividing
`startDelay` by `periodSeconds`.

You can customize any of the probe settings in the `.spec.probes.startup`
section of your configuration.

:::warning
    Be sure that any custom probe settings are tailored to your cluster's
    operational requirements to avoid unintended disruptions.
:::

:::info
    For more details on probe configuration, refer to the
    [probe API documentation](cloudnative-pg.v1.md#probe).
:::

If you manually specify `.spec.probes.startup.failureThreshold`, it will
override the default behavior and disable the automatic use of `startDelay`.

For example, the following configuration explicitly sets custom probe
parameters, bypassing `startDelay`:

```yaml
# ... snip
spec:
  probes:
    startup:
      periodSeconds: 3
      timeoutSeconds: 3
      failureThreshold: 10
```

### Liveness Probe

The liveness probe begins after the startup probe successfully completes. Its
primary role is to ensure the PostgreSQL instance—whether primary or standby—is
operating correctly. This is achieved using the `pg_isready` utility. Both exit
codes `0` (indicating the server is accepting connections) and `1` (indicating
the server is rejecting connections, such as during startup or a smart
shutdown) are treated as valid outcomes.
Following Kubernetes standards, if the liveness probe fails, the
kubelet will terminate the container, which will then be restarted.

The amount of time before a Pod is classified as not alive is configurable via
the `.spec.livenessProbeTimeout` parameter.

CloudNativePG configures the liveness probe with the following default
parameters:

```yaml
failureThreshold: FAILURE_THRESHOLD
periodSeconds: 10
successThreshold: 1
timeoutSeconds: 5
```

The `failureThreshold` value is automatically calculated by dividing
`livenessProbeTimeout` by `periodSeconds`.

By default, `.spec.livenessProbeTimeout` is set to `30` seconds. This means the
liveness probe will report a failure if it detects three consecutive probe
failures, with a 10-second interval between each check.

You can customize any of the probe settings in the `.spec.probes.liveness`
section of your configuration.

:::warning
    Be sure that any custom probe settings are tailored to your cluster's
    operational requirements to avoid unintended disruptions.
:::

:::info
    For more details on probe configuration, refer to the
    [probe API documentation](cloudnative-pg.v1.md#probe).
:::

If you manually specify `.spec.probes.liveness.failureThreshold`, it will
override the default behavior and disable the automatic use of
`livenessProbeTimeout`.

For example, the following configuration explicitly sets custom probe
parameters, bypassing `livenessProbeTimeout`:

```yaml
# ... snip
spec:
  probes:
    liveness:
      periodSeconds: 3
      timeoutSeconds: 3
      failureThreshold: 10
```

### Readiness Probe

The readiness probe begins once the startup probe has successfully completed.
Its purpose is to check whether the PostgreSQL instance is ready to accept
traffic and serve requests.
For streaming replicas, it also requires that they have connected to the source
at least once. Following Kubernetes standards, if the readiness probe fails,
the pod will be marked unready and will not receive traffic from any services.

CloudNativePG uses the following default configuration for the readiness probe:

```yaml
failureThreshold: 3
periodSeconds: 10
successThreshold: 1
timeoutSeconds: 5
```

If the default settings do not suit your requirements, you can fully customize
the readiness probe by specifying parameters in the `.spec.probes.readiness`
stanza. For example:

```yaml
# ... snip
spec:
  probes:
    readiness:
      periodSeconds: 3
      timeoutSeconds: 3
      failureThreshold: 10
```

:::warning
    Ensure that any custom probe settings are aligned with your cluster’s
    operational requirements to prevent unintended disruptions.
:::

:::info
    For more information on configuring probes, see the
    [probe API](cloudnative-pg.v1.md#probe).
:::

## Shutdown control

When a Pod running Postgres is deleted, either manually or by Kubernetes
following a node drain operation, the kubelet will send a termination signal to the
instance manager, and the instance manager will take care of shutting down
PostgreSQL in an appropriate way.
The `.spec.smartShutdownTimeout` and `.spec.stopDelay` options, expressed in seconds,
control the amount of time given to PostgreSQL to shut down. The values default
to 180 and 1800 seconds, respectively.

The shutdown procedure is composed of two steps:

1. The instance manager first issues a `CHECKPOINT`, then initiates a **smart**
shut down, disallowing any new connection to PostgreSQL. This step will last
for up to `.spec.smartShutdownTimeout` seconds.

2. If PostgreSQL is still up, the instance manager requests a **fast**
shut down, terminating any existing connection and exiting promptly.
If the instance is archiving and/or streaming WAL files, the process
will wait for up to the remaining time set in `.spec.stopDelay` to complete the
operation and then forcibly shut down. Such a timeout needs to be at least 15
seconds.

:::info[Important]
    In order to avoid any data loss in the Postgres cluster, which impacts
    the database [RPO](before_you_start.md#postgresql-terminology), don't delete the Pod where
    the primary instance is running. In this case, perform a switchover to
    another instance first.
:::

### Shutdown of the primary during a switchover

During a switchover, the shutdown procedure slightly differs from the general
case. The instance manager of the former primary first issues a `CHECKPOINT`,
then initiates a **fast** shutdown of PostgreSQL before the designated new
primary is promoted, ensuring that all data are safely available on the new
primary.

For this reason, the `.spec.switchoverDelay`, expressed in seconds, controls
the  time given to the former primary to shut down gracefully and archive all
the WAL files. By default it is set to `3600` (1 hour).

:::warning
    The `.spec.switchoverDelay` option affects the [RPO](before_you_start.md#postgresql-terminology)
    and [RTO](before_you_start.md#postgresql-terminology) of your PostgreSQL database. Setting it to
    a low value, might favor RTO over RPO but lead to data loss at cluster level
    and/or backup level. On the contrary, setting it to a high value, might remove
    the risk of data loss while leaving the cluster without an active primary for a
    longer time during the switchover.
:::

## Failover

In case of primary pod failure, the cluster will go into failover mode.
Please refer to the ["Failover" section](failover.md) for details.

## Disk Full Failure

Storage exhaustion is a well known issue for PostgreSQL clusters.
The [PostgreSQL documentation](https://www.postgresql.org/docs/current/diskusage.html#DISK-FULL)
highlights the possible failure scenarios and the importance of monitoring disk
usage to prevent it from becoming full.

The same applies to CloudNativePG and Kubernetes as well: the
["Monitoring" section](monitoring.md#predefined-set-of-metrics)
provides details on checking the disk space used by WAL segments and standard
metrics on disk usage exported to Prometheus.

:::info[Important]
    In a production system, it is critical to monitor the database
    continuously. Exhausted disk storage can lead to a database server shutdown.
:::

:::note
    The detection of exhausted storage relies on a storage class that
    accurately reports disk size and usage. This may not be the case in simulated
    Kubernetes environments like Kind or with test storage class implementations
    such as `csi-driver-host-path`.
:::

If the disk containing the WALs becomes full and no more WAL segments can be
stored, PostgreSQL will stop working. CloudNativePG correctly detects this issue
by verifying that there is enough space to store the next WAL segment,
and avoids triggering a failover, which could complicate recovery.

That allows a human administrator to address the root cause.

In such a case, if supported by the storage class, the quickest course of action
is currently to:

1. Expand the storage size of the full PVC
2. Increase the size in the `Cluster` resource to the same value

Once the issue is resolved and there is sufficient free space for WAL segments,
the Pod will restart and the cluster will become healthy.

See also the ["Volume expansion" section](storage.md#volume-expansion) of the
documentation.
