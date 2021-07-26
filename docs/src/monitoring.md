
# Monitoring

## Monitoring Instances

For each PostgreSQL instance, the operator provides an exporter of metrics for
[Prometheus](https://prometheus.io/) via HTTP, on port 9187, named `metrics`.
The operator comes with a predefined set of metrics, as well as a highly
configurable and customizable system to define additional queries via one or
more `ConfigMap` or `Secret` resources (see the
["User defined metrics" section](#user-defined-metrics) below for details).

Metrics can be accessed as follows:

```shell
curl http://<pod_ip>:9187/metrics
```

All monitoring queries are:

- transactionally atomic (one transaction per query)
- executed with the `pg_monitor` role
- executed with `application_name` set to `cnp_metrics_exporter`
- executed as user `postgres`

Please refer to the "Default roles" section in PostgreSQL
[documentation](https://www.postgresql.org/docs/current/default-roles.html)
for details on the `pg_monitor` role.

Queries, by default, are run against the *main database*, as defined by
the specified `bootstrap` method of the `Cluster` resource, according
to the following logic:

- using `initdb`: queries will be run against the specified database by default, so the
  value passed as `initdb.database` or defaulting to `app` if not specified.
- not using `initdb`: queries will run against the `postgres` database, by default.

The default database can always be overridden for a given user-defined metric,
by specifying a list of one or more databases in the `target_databases` option.

### Prometheus Operator example

A specific PostgreSQL cluster can be monitored using the
[Prometheus Operator](https://github.com/prometheus-operator/prometheus-operator) by defining the following
[PodMonitor](https://github.com/prometheus-operator/prometheus-operator/blob/v0.47.1/Documentation/api.md#podmonitor)
resource:

```yaml
apiVersion: monitoring.coreos.com/v1
kind: PodMonitor
metadata:
  name: cluster-example
spec:
  selector:
    matchLabels:
      postgresql: cluster-example
  podMetricsEndpoints:
  - port: metrics
```

!!! Important
    Make sure you modify the example above with a unique name as well as the
    correct cluster's namespace and labels (we are using `cluster-example`).

### User defined metrics

This feature is currently in *beta* state and the format is inspired by the
[queries.yaml file](https://github.com/prometheus-community/postgres_exporter/blob/master/queries.yaml) <!-- wokeignore:rule=master -->
of the PostgreSQL Prometheus Exporter.

Custom metrics can be defined by users by referring to the created `Configmap`/`Secret` in a `Cluster` definition
under the `.spec.monitoring.customQueriesConfigMap` or `customQueriesSecret` section as in the following example:

```yaml
apiVersion: postgresql.k8s.enterprisedb.io/v1
kind: Cluster
metadata:
  name: cluster-example
  namespace: test
spec:
  instances: 3

  storage:
    size: 1Gi

  monitoring:
    customQueriesConfigMap:
      - name: example-monitoring
        key: custom-queries
```

The `customQueriesConfigMap`/`customQueriesSecret` sections contain a list of
`ConfigMap`/`Secret` references specifying the key in which the custom queries are defined.
Take care that the referred resources have to be created **in the same namespace as the Cluster** resource.

!!! Note
    If you want ConfigMaps and Secrets to be **automatically** reloaded by instances, you can 
    add a label with key `k8s.enterprisedb.io/reload` to it, otherwise you will have to reload
    the instances using the `kubectl cnp reload` subcommand.

#### Example of a user defined metric

Here you can see an example of a `ConfigMap` containing a single custom query,
referenced by the `Cluster` example above:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: example-monitoring
  namespace: test
  labels:
    k8s.enterprisedb.io/reload: ""
data:
  custom-queries: |
    pg_replication:
      query: "SELECT CASE WHEN NOT pg_is_in_recovery()
              THEN 0
              ELSE GREATEST (0,
                EXTRACT(EPOCH FROM (now() - pg_last_xact_replay_timestamp())))
              END AS lag"
      metrics:
        - lag:
            usage: "GAUGE"
            description: "Replication lag behind primary in seconds"
```

A list of basic monitoring queries can be found in the [`cnp-basic-monitoring.yaml` file](
./samples/cnp-basic-monitoring.yaml).

#### Example of a user defined metric running on multiple databases

If the `target_databases` option lists more than one database
the metric is collected from each of them.

It is recommended that you always include the name of the database 
in the returned labels, for example using the `current_database()` function
as in the following example:

```yaml
some_query:
  query: |
    SELECT
     current_database() as datname,
     count(*) as rows
    FROM some_table
  metrics:
    - datname:
        usage: "LABEL"
        description: "Name of current database"
    - rows:
        usage: "GAUGE"
        description: "number of rows"
  target_databases:
    - albert
    - bb
    - freddie
```

This will produce in the following metric being exposed:

```text
cnp_some_query_rows{datname="albert"} 2
cnp_some_query_rows{datname="bb"} 5
cnp_some_query_rows{datname="freddie"} 10
```

### Structure of a user defined metric

Every custom query has the following basic structure:

```yaml
<MetricName>:
      query: "<SQLQuery>"
      metrics:
        - <ColumnName>:
            usage: "<MetricType>"
            description: "<MetricDescription>"
```

Here is a short description of all the available fields:

- `<MetricName>`: the name of the Prometheus metric
    - `query`: the SQL query to run on the target database to generate the metrics
    - `primary`: whether to run the query only on the primary instance <!-- wokeignore:rule=master -->
    - `master`: same as `primary` (for compatibility with the Prometheus PostgreSQL exporter's syntax - deprecated) <!-- wokeignore:rule=master -->
    - `target_databases`: a list of databases to run the `query` against, overwrites the default database, take care 
      to grant the `pg_monitor` role access to the required databases or tables
    - `metrics`: section containing a list of all exported columns, defined as follows:
      - `<ColumnName>`: the name of the column returned by the query
          - `usage`: one of the values described below
          - `description`: the metric's description
          - `metrics_mapping`: the optional column mapping when `usage` is set to `MAPPEDMETRIC`

The possible values for `usage` are:

| Column Usage Label  | Description                                              |
|:--------------------|:---------------------------------------------------------|
| `DISCARD`           | this column should be ignored                            |
| `LABEL`             | use this column as a label                               |
| `COUNTER`           | use this column as a counter                             |
| `GAUGE`             | use this column as a gauge                               |
| `MAPPEDMETRIC`      | use this column with the supplied mapping of text values |
| `DURATION`          | use this column as a text duration (in milliseconds)     |
| `HISTOGRAM`         | use this column as an histogram                          |


Please visit the ["Metric Types" page](https://prometheus.io/docs/concepts/metric_types/)
from the Prometheus documentation for more information.

### Output of a user defined metric

Custom defined metrics are returned by the Prometheus exporter endpoint (`:9187/metrics`)
with the following format:

```text
cnp_<MetricName>_<ColumnName>{<LabelColumnName>=<LabelColumnValue> ... } <ColumnValue>
```

!!! Note
    `LabelColumnName` are metrics with `usage` set to `LABEL` and their `Value`


Considering the `pg_replication` example above, the exporter's endpoint would
return the following output when invoked:

```text
# HELP cnp_pg_replication_lag Replication lag behind primary in seconds
# TYPE cnp_pg_replication_lag gauge
cnp_pg_replication_lag 0
```

### Differences with the Prometheus Postgres exporter

Cloud Native PostgreSQL is inspired by the PostgreSQL Prometheus Exporter, but
presents some differences. In particular, the following fields of a metric that
are defined in the official Prometheus exporter are not implemented in Cloud
Native PostgreSQL's exporter:

- `cache_seconds`: number of seconds to cache the result of the query
- `runonserver`: a semantic version range to limit the versions of PostgreSQL the query should run on (e.g. `">=10.0.0"`)

Similarly, the `pg_version` field of a column definition is not implemented.

## Monitoring the operator

The operator internally exposes [Prometheus](https://prometheus.io/) metrics
via HTTP on port 8080, named `metrics`.

Metrics can be accessed as follows:

```shell
curl http://<pod_ip>:8080/metrics
```

Currently, the operator exposes default `kubebuilder` metrics, see
[kubebuilder documentation](https://book.kubebuilder.io/reference/metrics.html) for more details.

### Prometheus Operator example

The operator deployment can be monitored using the
[Prometheus Operator](https://github.com/prometheus-operator/prometheus-operator) by defining the following
[PodMonitor](https://github.com/prometheus-operator/prometheus-operator/blob/v0.47.1/Documentation/api.md#podmonitor)
resource:

```yaml
apiVersion: monitoring.coreos.com/v1
kind: PodMonitor
metadata:
  name: postgresql-operator-controller-manager
spec:
  selector:
    matchLabels:
      app.kubernetes.io/name: cloud-native-postgresql
  podMetricsEndpoints:
    - port: metrics
```
