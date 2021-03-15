# Monitoring

For each PostgreSQL instance, the operator provides an exporter of metrics for
[Prometheus](https://prometheus.io/) via HTTP, on port 8000.
The operator comes with a predefined set of metrics, as well as a highly
configurable and customizable system to define additional queries via one or
more `ConfigMap` objects - and, future versions, `Secret` too.

The exporter can be accessed as follows:

```shell
curl http://<pod ip>:8000/metrics
```

## Predefined internal metrics

The operator provides a set of internal metrics and publishes them by default.
These includes PostgreSQL metrics such as:

* `pg_stat_activity`
* `pg_stat_archiver`
* `pg_stat_replication`
* `pg_locks`

All pre-defined metrics will be published with the `cnp_` prefix. As a result,
users won't be able to modify or add any metrics of this kind.

### `pg_stat_activity`

Monitor the current activity of the PostgreSQL server with information on backend processes:

```sql
SELECT states.state
  , sa.usename
  , COALESCE(sa.count, 0) AS backends
  , COALESCE(sa.max_tx_secs, 0) AS max_tx_duration_seconds
FROM ( VALUES ('active')
  , ('idle')
  , ('idle in transaction')
  , ('idle in transaction (aborted)')
  , ('fastpath function call')
  , ('disabled')
) AS states(state)
  LEFT JOIN (
    SELECT state
      , usename
      , count(*)
      , COALESCE(EXTRACT (EPOCH FROM (max(now() - xact_start))), 0)
        AS max_tx_secs
    FROM pg_stat_activity
    GROUP BY state, usename
  ) sa ON states.state = sa.state
WHERE sa.usename IS NOT NULL;
```

Please refer to the [PostgreSQL documentation on `pg_stat_activity`](https://www.postgresql.org/docs/current/monitoring-stats.html#MONITORING-PG-STAT-ACTIVITY-VIEW) for more information.

### `pg_stat_archiver`

Monitor the status of the WAL archiver process when continuous backup is in place:

```sql
SELECT archived_count, failed_count FROM pg_stat_archiver
```

Please refer to the [PostgreSQL documentation on `pg_stat_archiver`](https://www.postgresql.org/docs/current/monitoring-stats.html#MONITORING-PG-STAT-ARCHIVER-VIEW) for more information.

### `pg_stat_replication`

Monitor the status of replication:

```sql
SELECT usename
  , COALESCE(application_name, '')
  , COALESCE(client_addr::text, '') AS client_addr
  , pg_wal_lsn_diff(pg_current_wal_lsn(), sent_lsn) AS sent_diff_bytes
  , pg_wal_lsn_diff(pg_current_wal_lsn(), write_lsn) AS write_diff_bytes
  , pg_wal_lsn_diff(pg_current_wal_lsn(), flush_lsn) AS flush_diff_bytes
  , COALESCE(pg_wal_lsn_diff(pg_current_wal_lsn(), replay_lsn),0) AS replay_diff_bytes
  , COALESCE((EXTRACT(EPOCH FROM write_lag)),0)::float AS write_lag_seconds
  , COALESCE((EXTRACT(EPOCH FROM flush_lag)),0)::float AS flush_lag_seconds
  , COALESCE((EXTRACT(EPOCH FROM replay_lag)),0)::float AS replay_lag_seconds
FROM pg_stat_replication;
```

Please refer to the [PostgreSQL documentation on `pg_stat_replication`](https://www.postgresql.org/docs/current/monitoring-stats.html#MONITORING-PG-STAT-REPLICATION-VIEW) for more information.

### `pg_locks`

Monitor the locks held by active processes within the database:

```sql
SELECT count(*)
FROM pg_catalog.pg_locks blocked_locks
JOIN pg_catalog.pg_locks blocking_locks
  ON blocking_locks.locktype = blocked_locks.locktype
    AND blocking_locks.database IS NOT DISTINCT FROM blocked_locks.database
    AND blocking_locks.relation IS NOT DISTINCT FROM blocked_locks.relation
    AND blocking_locks.page IS NOT DISTINCT FROM blocked_locks.page
    AND blocking_locks.tuple IS NOT DISTINCT FROM blocked_locks.tuple
    AND blocking_locks.virtualxid IS NOT DISTINCT FROM blocked_locks.virtualxid
    AND blocking_locks.transactionid IS NOT DISTINCT FROM blocked_locks.transactionid
    AND blocking_locks.classid IS NOT DISTINCT FROM blocked_locks.classid
    AND blocking_locks.objid IS NOT DISTINCT FROM blocked_locks.objid
    AND blocking_locks.objsubid IS NOT DISTINCT FROM blocked_locks.objsubid
    AND blocking_locks.pid != blocked_locks.pid
JOIN pg_catalog.pg_stat_activity blocking_activity
  ON blocking_activity.pid = blocking_locks.pid
WHERE NOT blocked_locks.granted;
```

Please refer to the [PostgreSQL documentation on `pg_locks`](https://www.postgresql.org/docs/current/view-pg-locks.html) for more information.

## User defined metrics

Users will be able to define additional metrics through the available interface
that the operator provides. This interface is currently in *beta* state and
only supports definition of custom queries as `ConfigMap` objects using a YAML
file that is inspired by the [queries.yaml file](https://github.com/prometheus-community/postgres_exporter/blob/main/queries.yaml)
of the PostgreSQL Prometheus Exporter.

Queries must be defined in a `ConfigMap` to be referenced in the `monitoring`
section of the `Cluster` definition, as in the following example:

```yaml
apiVersion: postgresql.k8s.enterprisedb.io/v1
kind: Cluster
metadata:
  name: cluster-example
spec:
  instances: 3

  storage:
    size: 1Gi

  monitoring:
    customQueriesConfigMap:
      - name: example-monitoring
```

Specifically, the `monitoring` section looks for an array with the name
`customQueriesConfigMap`, which, as the name suggests, needs a list of
`ConfigMap` names to be used as the source of custom queries. The `ConfigMap`
must have a data field called `queries.yaml` which contains YAML content only.
For example:

```yaml
---
apiVersion: v1
kind: ConfigMap
metadata:
  namespace: default
  name: example-monitoring
data:
  queries.yaml: |
    pg_replication:
      query: "SELECT CASE WHEN NOT pg_is_in_recovery()
              THEN 0
              ELSE GREATEST (0,
                EXTRACT(EPOCH FROM (now() - pg_last_xact_replay_timestamp())))
              END AS lag"
      primary: true
      metrics:
        - lag:
            usage: "GAUGE"
            description: "Replication lag behind primary in seconds"
```

The object must have a name and be in the same namespace as the `Cluster`.
Note that the above query will be executed on the `primary` node, with the
following output.

```text
# HELP pg_replication_lag Replication lag behind primary in seconds
# TYPE pg_replication_lag gauge
pg_replication_lag 0
```

This framework enables the definition of custom metrics to monitor the database
or the application inside the PostgreSQL cluster.
