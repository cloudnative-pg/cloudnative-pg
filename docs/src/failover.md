# Automated failover

In the case of unexpected errors on the primary for longer than the
`spec.failoverDelay` (by default `0` seconds), the cluster goes into
*failover mode*. This might happen, for example, when:

- The primary pod has a disk failure.
- The primary pod is deleted.
- The `postgres` container on the primary has any kind of sustained failure.

In the failover scenario, you can't assume the primary is working properly.

After cases like the ones above, the readiness probe for the primary pod starts
failing. This failure is picked up in the controller's reconciliation loop. The
controller initiates the failover process, in two steps:

1. It marks the `TargetPrimary` as `pending`. This change of state
   forces the primary pod to shut down, to ensure the WAL receivers on the replicas
   stops. The cluster is marked in failover phase (that is, *failing over*).
2. Once all WAL receivers are stopped, a leader election occurs, and a
   new primary is named. The chosen instance initiates promotion to
   primary, and, after this is completed, the cluster resumes normal operations.
   Meanwhile, the former primary pod restarts, detects that it's no longer
   the primary, and becomes a replica node.

!!! Important
    The two-phase procedure helps ensure the WAL receivers can stop in an orderly
    fashion and that the failing primary doesn't start streaming WALs again upon
    restart. These safeguards prevent timeline discrepancies between the new primary
    and the replicas.

During the time the failing primary is being shut down:

1. It tries a PostgreSQL's *fast shutdown* with
   `.spec.switchoverDelay` seconds as timeout. This graceful shutdown attempts
   to archive pending WALs.
2. If the fast shutdown fails, or its timeout is exceeded, a PostgreSQL
   *immediate shutdown* is initiated.

!!! Info
    Fast mode doesn't wait for PostgreSQL clients to disconnect and
    terminates an online backup in progress. All active transactions are rolled back,
    clients are forcibly disconnected, and then the server is shut down.
    Immediate mode aborts all PostgreSQL server processes immediately,
    without a clean shutdown.

## RTO and RPO impact

Failover might result in the service being impacted or data being lost:

1. During the time when the primary starts to fail and before the controller
   starts failover procedures, queries in transit, WAL writes, checkpoints, and
   similar operations might fail.
2. Once the fast shutdown command is issued, the cluster no longer
   accepts connections, so service is impacted. However, no data
   is lost.
3. If the fast shutdown fails, the immediate shutdown stops any pending
   processes, including WAL writing. Data might be lost.
4. During the time the primary is shutting down and a new primary hasn't yet
   started, the cluster operates without a primary and is thus impaired. However,
   no data is lost.

!!! Note
    The timeout that controls fast shutdown is set by `.spec.switchoverDelay`,
    as in the case of a switchover. Increasing the time for fast shutdown is safer
    from an RPO point of view. However, this technique can delay the return to normal operation 
    and negatively affect RTO.

!!! Warning
    As mentioned in [Instance manager](instance_manager.md),
    when explaining the switchover process, the `.spec.switchoverDelay` option
    affects the RPO and RTO of your PostgreSQL database. Setting it to a low value
    might favor RTO over RPO but lead to data loss at the cluster and backup
    level. Conversely, setting it to a high value might remove the risk of
    data loss while leaving the cluster without an active primary for a longer time
    during the switchover.

## Delayed failover

The `spec.failoverDelay` option allows you to delay the start
of the failover procedure by a number of seconds after the primary is
detected to be unhealthy. By default, this setting is set to `0`, triggering the
failover procedure immediately.

Sometimes failing over to a new primary can be more disruptive than waiting
for the primary to come back online. This is especially true of network
disruptions where multiple tiers are affected (that is, downstream logical
subscribers) or when the time to perform the failover is longer than the
expected outage.

Enabling a new configuration option to delay failover provides a mechanism to
prevent premature failover for short-lived network or node instability.
