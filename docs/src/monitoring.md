# Monitoring

For each PostgreSQL instance, the operator provides an exporter of metrics for
[Prometheus](https://prometheus.io/) via HTTP, on port 9187.
The operator comes with a predefined set of metrics, as well as a highly
configurable and customizable system to define additional queries via one or
more `ConfigMap` objects - and, future versions, `Secret` too.

The exporter can be accessed as follows:

```shell
curl http://<pod ip>:9187/metrics
```

All monitoring queries are:

- transactionally atomic (one transaction per query)
- executed with the `pg_monitor` role

Please refer to the
["Default roles" section in PostgreSQL documentation](https://www.postgresql.org/docs/current/default-roles.html)
for details on the `pg_monitor` role.

## User defined metrics

Users will be able to define metrics through the available interface
that the operator provides. This interface is currently in *beta* state and
only supports definition of custom queries as `ConfigMap` and `Secret` objects
using a YAML file that is inspired by the [queries.yaml file](https://github.com/prometheus-community/postgres_exporter/blob/main/queries.yaml)
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
        key: custom-queries
```

Specifically, the `monitoring` section looks for an array with the name
`customQueriesConfigMap`, which, as the name suggests, needs a list of
`ConfigMap` key references to be used as the source of custom queries.

For example:

```yaml
---
apiVersion: v1
kind: ConfigMap
metadata:
  namespace: default
  name: example-monitoring
data:
  custom-queries: |
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
# HELP custom_pg_replication_lag Replication lag behind primary in seconds
# TYPE custom_pg_replication_lag gauge
custom_pg_replication_lag 0
```

This framework enables the definition of custom metrics to monitor the database
or the application inside the PostgreSQL cluster.
