# Postgres instance manager

CloudNativePG does not rely on an external tool for failover management.
It simply relies on the Kubernetes API server and a native key component called:
the **Postgres instance manager**.

The instance manager takes care of the entire lifecycle of the PostgreSQL
leading process (also known as `postmaster`).

When you create a new cluster, the operator makes a Pod per instance.
The field `.spec.instances` specifies how many instances to create.

Each Pod will start the instance manager as the parent process (PID 1) for the
main container, which in turn runs the PostgreSQL instance. During the lifetime
of the Pod, the instance manager acts as a backend to handle the
[startup, liveness and readiness probes](https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/#container-probes).

## Startup, liveness and readiness probes

The startup and liveness probes rely on `pg_isready`, while the readiness
probe checks if the database is up and able to accept connections.

### Startup Probe

The `.spec.startDelay` parameter specifies the delay (in seconds) before the
liveness probe activates after a PostgreSQL Pod starts. By default, this is set
to `3600` seconds. You should adjust this value based on the time PostgreSQL
requires to fully initialize in your environment.

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

Here, `FAILURE_THRESHOLD` is calculated as `startDelay` divided by
`periodSeconds`.

If the default behavior based on `startDelay` is not suitable for your use
case, you can take full control of the startup probe by specifying custom
parameters in the `.spec.probes.startup` stanza. Note that defining this stanza
will override the default behavior, including the use of `startDelay`.

!!! Warning
    Ensure that any custom probe settings are aligned with your cluster’s
    operational requirements to prevent unintended disruptions.

!!! Info
    For detailed information about probe configuration, refer to the
    [probe API](cloudnative-pg.v1.md#postgresql-cnpg-io-v1-Probe).

For example, the following configuration bypasses `startDelay` entirely:

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

The liveness probe begins after the startup probe succeeds and is responsible
for detecting if the PostgreSQL instance has entered a broken state that
requires a restart of the pod.

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

Here, `FAILURE_THRESHOLD` is calculated as `livenessProbeTimeout` divided by
`periodSeconds`.

By default, `.spec.livenessProbeTimeout` is set to `30` seconds. This means the
liveness probe will report a failure if it detects three consecutive probe
failures, with a 10-second interval between each check.

If the default behavior using `livenessProbeTimeout` does not meet your needs,
you can fully customize the liveness probe by defining parameters in the
`.spec.probes.liveness` stanza. Keep in mind that specifying this stanza will
override the default behavior, including the use of `livenessProbeTimeout`.

!!! Warning
    Ensure that any custom probe settings are aligned with your cluster’s
    operational requirements to prevent unintended disruptions.

!!! Info
    For more details on probe configuration, refer to the
    [probe API](cloudnative-pg.v1.md#postgresql-cnpg-io-v1-Probe).

For example, the following configuration overrides the default behavior and
bypasses `livenessProbeTimeout`:

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

The readiness probe determines when a pod running a PostgreSQL instance is
prepared to accept traffic and serve requests.

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
    [probe API](cloudnative-pg.v1.md#postgresql-cnpg-io-v1-Probe).

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
    the database RPO, don't delete the Pod where the primary instance is running.
    In this case, perform a switchover to another instance first.

### Shutdown of the primary during a switchover

During a switchover, the shutdown procedure is slightly different from the
general case. Indeed, the operator requires the former primary to issue a
**fast** shut down before the selected new primary can be promoted,
in order to ensure that all the data are available on the new primary.

For this reason, the `.spec.switchoverDelay`, expressed in seconds, controls
the  time given to the former primary to shut down gracefully and archive all
the WAL files. By default it is set to `3600` (1 hour).

!!! Warning
    The `.spec.switchoverDelay` option affects the RPO and RTO of your
    PostgreSQL database. Setting it to a low value, might favor RTO over RPO
    but lead to data loss at cluster level and/or backup level. On the contrary,
    setting it to a high value, might remove the risk of data loss while leaving
    the cluster without an active primary for a longer time during the switchover.

## Failover

In case of primary pod failure, the cluster will go into failover mode.
Please refer to the ["Failover" section](failover.md) for details.
