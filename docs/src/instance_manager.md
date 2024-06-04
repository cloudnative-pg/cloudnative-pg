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
probe checks if the database is up and able to accept connections using the
superuser credentials.

The readiness probe is positive when the Pod is ready to accept traffic.
The liveness probe controls when to restart the container once
the startup probe interval has elapsed.

!!! Important
    The liveness and readiness probes will report a failure if the probe command
    fails three times with a 10-second interval between each check.

The liveness probe detects if the PostgreSQL instance is in a
broken state and needs to be restarted. The value in `startDelay` is used
to delay the probe's execution, preventing an
instance with a long startup time from being restarted.

The interval (in seconds) after the Pod has started before the liveness
probe starts working is expressed in the `.spec.startDelay` parameter,
which defaults to 3600 seconds. The correct value for your cluster is
related to the time needed by PostgreSQL to start.

!!! Warning
    If `.spec.startDelay` is too low, the liveness probe will start working
    before the PostgreSQL startup is complete, and the Pod could be restarted
    prematurely.

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

## Disk Full Failure

Storage exhaustion is a well known issue for PostgreSQL clusters.
The [PostgreSQL documentation](https://www.postgresql.org/docs/current/disk-full.html)
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
