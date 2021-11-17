# Postgres instance manager

Cloud Native PostgreSQL does not rely on an external tool for failover management.
It simply relies on the Kubernetes API server and a native key component called:
the **Postgres instance manager**.

The instance manager takes care of the entire lifecycle of the PostgreSQL
leading process (also known as `postmaster`).

When you create a new cluster, the operator makes a Pod per instance.
The field `.spec.instances` specifies how many instances to create.

Each Pod will start the instance manager as the parent process (PID 1) for the
main container, which in turn runs the PostgreSQL instance. During the lifetime
of the Pod, the instance manager acts as a backend to handle the [liveness and
readiness probes](https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/#container-probes).

## Liveness and readiness probes

The liveness probe relies on `pg_isready`, while the readiness probe checks if
the database is up and able to accept connections using the superuser
credentials.
The readiness probe is positive when the Pod is ready to accept traffic.
The liveness probe controls when to restart the container.

> The two probes will report a failure if the probe command fails 3 times with a 10 seconds interval between each check.

For now, the operator doesn't configure a `startupProbe` on the Pods, since
startup probes have been introduced only in Kubernetes 1.17.

The liveness probe is used to detect if the PostgreSQL instance is in a
broken state and needs to be restarted. The value in `startDelay` is used
to delay the probe's execution, which is used to prevent an
instance with a long startup time from being restarted.

The number of seconds after the Pod has started before the liveness
probe starts working is expressed in the `.spec.StartDelay` parameter,
which defaults to 30 seconds. The correct value for your cluster is
related to the time needed by PostgreSQL to start.

If `StartDelay` is too low, the liveness probe will start working
before the PostgreSQL startup, and the Pod could be restarted
inappropriately.

## Shutdown control

When you drain a Kubernetes node, or you delete the cluster, Kubernetes
will delete the Pods. The kubelet will send a termination signal to the
instance manager, and the instance manager will take care of shutting down
PostgreSQL in an appropriate way.
The `.spec.StopDelay`, expressed in seconds, is the amount of time
given to PostgreSQL to shut down. The value defaults to 30 seconds.

The shutdown procedure is composed of two steps:

1. The instance manager requests a **smart** shut down, disallowing any
new connection to PostgreSQL. This step will last for half of the
time set in `.spec.StopDelay`.

2. If PostgreSQL is still up, the instance manager requests a **fast**
shut down, terminating any existing connection and exiting promptly.

