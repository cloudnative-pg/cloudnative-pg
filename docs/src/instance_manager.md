# Postgres instance manager

CloudNativePG doesn't rely on an external tool for failover management.
It relies on the Kubernetes API server and a native key component called
the Postgres *instance manager*.

The instance manager takes care of the entire lifecycle of the PostgreSQL
leading process (also known as `postmaster`).

When you create a new cluster, the operator makes a pod per instance.
The field `.spec.instances` specifies how many instances to create.

Each pod starts the instance manager as the parent process (PID 1) for the
main container, which in turn runs the PostgreSQL instance. During the lifetime
of the pod, the instance manager acts as a backend to handle the
[startup, liveness, and readiness probes](https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/#container-probes).

## Startup, liveness, and readiness probes

The startup and liveness probes rely on `pg_isready`. The readiness
probe checks if the database is up and able to accept connections using the
superuser credentials.

The readiness probe is positive when the pod is ready to accept traffic.
The liveness probe controls when to restart the container once
the startup probe interval has elapsed.

!!! Important
    The liveness and readiness probes report a failure if the probe command
    fails three times with a 10-second interval between each check.

The liveness probe detects if the PostgreSQL instance is in a
broken state and needs to be restarted. The value in `startDelay` is used
to delay the probe's execution, preventing an
instance with a long startup time from being restarted.

The interval (in seconds) after the pod has started before the liveness
probe starts working is expressed in the `.spec.startDelay` parameter,
which defaults to 3600 seconds. The correct value for your cluster is
related to the time needed by PostgreSQL to start.

!!! Warning
    If `.spec.startDelay` is too low, the liveness probe will start working
    before the PostgreSQL startup is complete, and the Pod might be restarted
    prematurely.

## Shutdown control

When a Pod running Postgres is deleted, either manually or by Kubernetes
following a node drain operation, the kubelet sends a termination signal to the
instance manager. The instance manager takes care of shutting down
PostgreSQL in an appropriate way.
The `.spec.smartShutdownTimeout` and `.spec.stopDelay` options, expressed in seconds,
control the amount of time given to PostgreSQL to shut down. The values default
to 180 and 1800 seconds, respectively.

The shutdown procedure is composed of two steps:

1. The instance manager requests a *smart* shut down, disallowing any
new connection to PostgreSQL. This step lasts for up to
`.spec.smartShutdownTimeout` seconds.

2. If PostgreSQL is still up, the instance manager requests a *fast*
shut down, terminating any existing connection and exiting promptly.
If the instance is archiving or streaming WAL files, the process
waits for up to the remaining time set in `.spec.stopDelay` to complete the
operation and then forcibly shut down. Such a timeout needs to be at least 15
seconds.

!!! Important
    To avoid any data loss in the Postgres cluster, which impacts
    the database RPO, don't delete the pod where the primary instance is running.
    In this case, perform a switchover to another instance first.

### Shutdown of the primary during a switchover

During a switchover, the shutdown procedure is slightly different from the
general case. Indeed, to ensure that all the data are available on the new 
primary, the operator requires the former primary to issue a
fast shutdown before the selected new primary can be promoted.

For this reason, the `.spec.switchoverDelay`, expressed in seconds, controls
the time given to the former primary to shut down gracefully and archive all
the WAL files. By default it's set to `3600` (1 hour).

!!! Warning
    The `.spec.switchoverDelay` option affects the RPO and RTO of your
    PostgreSQL database. Setting it to a low value might favor RTO over RPO
    but lead to data loss at the cluster level or backup level. Conversely,
    setting it to a high value might remove the risk of data loss while leaving
    the cluster without an active primary for a longer time during the switchover.

## Failover

In case of primary pod failure, the cluster goes into failover mode.
For details, see [Failover](failover.md).
