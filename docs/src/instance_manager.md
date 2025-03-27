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

## Startup Probe

The startup probe ensures that a PostgreSQL instance, whether a primary or
standby, has fully started.

!!! Info
    By default, the startup probe uses
    [`pg_isready`](https://www.postgresql.org/docs/current/app-pg-isready.html).
    However, the behavior can be customized by specifying a different startup
    strategy.

While the startup probe is running, the liveness and readiness probes remain
disabled. Following Kubernetes standards, if the startup probe fails, the
kubelet will terminate the container, which will then be restarted.

The `.spec.startDelay` parameter specifies the maximum time, in seconds,
allowed for the startup probe to succeed.

By default, the `startDelay` is set to `3600` seconds. It is recommended to
adjust this setting based on the time PostgreSQL needs to fully initialize in
your specific environment.

!!! Warning
    Setting `.spec.startDelay` too low can cause the liveness probe to activate
    prematurely, potentially resulting in unnecessary Pod restarts if PostgreSQL
    hasn’t fully initialized.

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

!!! Warning
    Be sure that any custom probe settings are tailored to your cluster's
    operational requirements to avoid unintended disruptions.

!!! Info
    For more details on probe configuration, refer to the
    [probe API documentation](cloudnative-pg.v1.md#postgresql-cnpg-io-v1-ProbeWithStrategy).

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

### Startup Probe Strategy

In certain scenarios, you may need to customize the startup strategy for your
PostgreSQL cluster. For example, you might delay marking a replica as started
until it begins streaming from the primary or define a replication lag
threshold that must be met before considering the replica ready.

To accommodate these requirements, CloudNativePG extends the
`.spec.probes.startup` stanza with two optional parameters:

- `type`: specifies the criteria for considering the probe successful. Accepted
  values, in increasing order of complexity/depth, include:

    - `pg_isready`: marks the probe as successful when the `pg_isready` command
      exits with `0`. This is the default for primary instances and replicas.
    - `query`: marks the probe as successful when a basic query is executed on
      the `postgres` database locally.
    - `streaming`: marks the probe as successful when the replica begins
      streaming from its source and meets the specified lag requirements (details
      below).

- `maximumLag`: defines the maximum acceptable replication lag, measured in bytes
  (expressed as Kubernetes quantities). This parameter is only applicable when
  `type` is set to `streaming`. If `maximumLag` is not specified, the replica is
  considered successfully started as soon as it begins streaming.

!!! Important
    The `.spec.probes.startup.maximumLag` option is validated and enforced only
    during the startup phase of the pod, meaning it applies exclusively when the
    replica is starting.

!!! Warning
    Incorrect configuration of the `maximumLag` option can cause continuous
    failures of the startup probe, leading to repeated replica restarts. Ensure
    you understand how this option works and configure appropriate values for
    `failureThreshold` and `periodSeconds` to give the replica enough time to
    catch up with its source.

The following example requires a replica to have a maximum lag of 16Mi from the
source to be considered started:

```yaml
# <snip>
probes:
  startup:
    type: streaming
    maximumLag: 16Mi
```

## Liveness Probe

The liveness probe begins after the startup probe successfully completes. Its
primary role is to ensure the PostgreSQL instance manager is operating
correctly.

Following Kubernetes standards, if the liveness probe fails, the kubelet will
terminate the container, which will then be restarted.

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

!!! Warning
    Be sure that any custom probe settings are tailored to your cluster's
    operational requirements to avoid unintended disruptions.

!!! Info
    For more details on probe configuration, refer to the
    [probe API documentation](cloudnative-pg.v1.md#postgresql-cnpg-io-v1-Probe).

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

## Readiness Probe

The readiness probe starts once the startup probe has successfully completed.
Its primary purpose is to check whether the PostgreSQL instance is ready to
accept traffic and serve requests at any point during the pod's lifecycle.

!!! Info
    By default, the readiness probe uses
    [`pg_isready`](https://www.postgresql.org/docs/current/app-pg-isready.html).
    However, the behavior can be customized by specifying a different readiness
    strategy.

Following Kubernetes standards, if the readiness probe fails, the pod will be
marked unready and will not receive traffic from any services. An unready pod
is also ineligible for promotion during automated failover scenarios.

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

!!! Warning
    Ensure that any custom probe settings are aligned with your cluster’s
    operational requirements to prevent unintended disruptions.

!!! Info
    For more information on configuring probes, see the
    [probe API](cloudnative-pg.v1.md#postgresql-cnpg-io-v1-ProbeWithStrategy).

### Readiness Probe Strategy

In certain scenarios, you may need to customize the readiness strategy for your
cluster. For example, you might delay marking a replica as ready until it
begins streaming from the primary or define a maximum replication lag threshold
before considering the replica ready.

To accommodate these requirements, CloudNativePG extends the
`.spec.probes.readiness` stanza with two optional parameters: `type` and
`maximumLag`. Please refer to the [Startup Probe Strategy](#startup-probe-strategy)
section for detailed information on these options.

!!! Important
    Unlike the startup probe, the `.spec.probes.readiness.maximumLag` option is
    continuously monitored. A lagging replica may become unready if this setting is
    not appropriately tuned.

!!! Warning
    Incorrect configuration of the `maximumLag` option can lead to repeated
    readiness probe failures, causing serious consequences, such as:

    - Exclusion of the replica from key operator features, such as promotion
      during failover or participation in synchronous replication quorum.
    - Disruptions in read/read-only services.
    - In longer failover times scenarios, replicas might be declared unready,
      leading to a cluster stall requiring manual intervention.

!!! Recommendation
    Use the `streaming` and `maximumLag` options with extreme caution. If
    you're unfamiliar with PostgreSQL replication, rely on the default
    strategy. Seek professional advice if unsure.

The following example requires a replica to have a maximum lag of 64Mi from the
source to be considered ready. It also provides approximately 300 seconds (30
failures × 10 seconds) for the startup probe to succeed:

```yaml
# <snip>
probes:
  readiness:
    type: streaming
    maximumLag: 64Mi
    failureThreshold: 30
    periodSeconds: 10
```

## Shutdown control

When a Pod running Postgres is deleted, either manually or by Kubernetes
following a node drain operation, the kubelet will send a termination signal to the
instance manager, and the instance manager will take care of shutting down
PostgreSQL in an appropriate way.
The `.spec.smartShutdownTimeout` and `.spec.stopDelay` options, expressed in seconds,
control the amount of time given to PostgreSQL to shut down. The values default
to 180 and 1800 seconds, respectively.

The shutdown procedure is composed of two steps:

1. The instance manager requests a **smart** shut down, disallowing any
new connection to PostgreSQL. This step will last for up to
`.spec.smartShutdownTimeout` seconds.

2. If PostgreSQL is still up, the instance manager requests a **fast**
shut down, terminating any existing connection and exiting promptly.
If the instance is archiving and/or streaming WAL files, the process
will wait for up to the remaining time set in `.spec.stopDelay` to complete the
operation and then forcibly shut down. Such a timeout needs to be at least 15
seconds.

!!! Important
    In order to avoid any data loss in the Postgres cluster, which impacts
    the database [RPO](before_you_start.md#rpo), don't delete the Pod where
    the primary instance is running. In this case, perform a switchover to
    another instance first.

### Shutdown of the primary during a switchover

During a switchover, the shutdown procedure is slightly different from the
general case. Indeed, the operator requires the former primary to issue a
**fast** shut down before the selected new primary can be promoted,
in order to ensure that all the data are available on the new primary.

For this reason, the `.spec.switchoverDelay`, expressed in seconds, controls
the  time given to the former primary to shut down gracefully and archive all
the WAL files. By default it is set to `3600` (1 hour).

!!! Warning
    The `.spec.switchoverDelay` option affects the [RPO](before_you_start.md#rpo)
    and [RTO](before_you_start.md#rto) of your PostgreSQL database. Setting it to
    a low value, might favor RTO over RPO but lead to data loss at cluster level
    and/or backup level. On the contrary, setting it to a high value, might remove
    the risk of data loss while leaving the cluster without an active primary for a
    longer time during the switchover.

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

!!! Important
    In a production system, it is critical to monitor the database
    continuously. Exhausted disk storage can lead to a database server shutdown.

!!! Note
    The detection of exhausted storage relies on a storage class that
    accurately reports disk size and usage. This may not be the case in simulated
    Kubernetes environments like Kind or with test storage class implementations
    such as `csi-driver-host-path`.

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
