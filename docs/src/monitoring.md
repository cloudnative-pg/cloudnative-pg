
# Monitoring

## Monitoring Instances

For each PostgreSQL instance, the operator provides an exporter of metrics for
[Prometheus](https://prometheus.io/) via HTTP, on port 9187, named `metrics`.
The operator comes with a [predefined set of metrics](#predefined-set-of-metrics), as well as a highly
configurable and customizable system to define additional queries via one or
more `ConfigMap` or `Secret` resources (see the
["User defined metrics" section](#user-defined-metrics) below for details).

Metrics can be accessed as follows:

```shell
curl http://<pod_ip>:9187/metrics
```

All monitoring queries that are performed on PostgreSQL are:

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

### Predefined set of metrics

Every PostgreSQL instance exporter automatically exposes a set of predefined
metrics, which can be classified in two major categories:

- PostgreSQL related metrics, starting with `cnp_collector_*`, including:

    - number of WAL files and total size on disk
    - number of `.ready` and `.done` files in the archive status folder
    - requested minimum and maximum number of synchronous replicas, as well as
      the expected and actually observed values
    - flag indicating if replica cluster mode is enabled or disabled
    - flag indicating if a manual switchover is required

- Go runtime related metrics, starting with `go_*`

Below is a sample of the metrics returned by the `localhost:9187/metrics`
endpoint of an instance. As you can see, the Prometheus format is
self-documenting:

```text
# HELP cnp_collector_collection_duration_seconds Collection time duration in seconds
# TYPE cnp_collector_collection_duration_seconds gauge
cnp_collector_collection_duration_seconds{collector="Collect.up"} 0.0031393

# HELP cnp_collector_collections_total Total number of times PostgreSQL was accessed for metrics.
# TYPE cnp_collector_collections_total counter
cnp_collector_collections_total 2

# HELP cnp_collector_last_collection_error 1 if the last collection ended with error, 0 otherwise.
# TYPE cnp_collector_last_collection_error gauge
cnp_collector_last_collection_error 0

# HELP cnp_collector_manual_switchover_required 1 if a manual switchover is required, 0 otherwise
# TYPE cnp_collector_manual_switchover_required gauge
cnp_collector_manual_switchover_required 0

# HELP cnp_collector_pg_wal Total size in bytes of WAL segments in the '/var/lib/postgresql/data/pgdata/pg_wal' directory  computed as (wal_segment_size * count)
# TYPE cnp_collector_pg_wal gauge
cnp_collector_pg_wal{value="count"} 7
cnp_collector_pg_wal{value="size"} 1.17440512e+08

# HELP cnp_collector_pg_wal_archive_status Number of WAL segments in the '/var/lib/postgresql/data/pgdata/pg_wal/archive_status' directory (ready, done)
# TYPE cnp_collector_pg_wal_archive_status gauge
cnp_collector_pg_wal_archive_status{value="done"} 6
cnp_collector_pg_wal_archive_status{value="ready"} 0

# HELP cnp_collector_replica_mode 1 if the cluster is in replica mode, 0 otherwise
# TYPE cnp_collector_replica_mode gauge
cnp_collector_replica_mode 0

# HELP cnp_collector_sync_replicas Number of requested synchronous replicas (synchronous_standby_names)
# TYPE cnp_collector_sync_replicas gauge
cnp_collector_sync_replicas{value="expected"} 0
cnp_collector_sync_replicas{value="max"} 0
cnp_collector_sync_replicas{value="min"} 0
cnp_collector_sync_replicas{value="observed"} 0

# HELP cnp_collector_up 1 if PostgreSQL is up, 0 otherwise.
# TYPE cnp_collector_up gauge
cnp_collector_up{cluster="cluster-example"} 1

# HELP cnp_collector_postgres_version Postgres version
# TYPE cnp_collector_postgres_version gauge
cnp_collector_postgres_version{cluster="cluster-example",full="13.4.0"} 13.4

# HELP cnp_collector_first_recoverability_point The first point of recoverability for the cluster as a unix timestamp
# TYPE cnp_collector_first_recoverability_point gauge
cnp_collector_first_recoverability_point 1.63238406e+09

# HELP cnp_collector_lo_pages Estimated number of pages in the pg_largeobject table
# TYPE cnp_collector_lo_pages gauge
cnp_collector_lo_pages{datname="app"} 0
cnp_collector_lo_pages{datname="postgres"} 78

# HELP go_gc_duration_seconds A summary of the pause duration of garbage collection cycles.
# TYPE go_gc_duration_seconds summary
go_gc_duration_seconds{quantile="0"} 5.01e-05
go_gc_duration_seconds{quantile="0.25"} 7.27e-05
go_gc_duration_seconds{quantile="0.5"} 0.0001748
go_gc_duration_seconds{quantile="0.75"} 0.0002959
go_gc_duration_seconds{quantile="1"} 0.0012776
go_gc_duration_seconds_sum 0.0035741
go_gc_duration_seconds_count 13

# HELP go_goroutines Number of goroutines that currently exist.
# TYPE go_goroutines gauge
go_goroutines 25

# HELP go_info Information about the Go environment.
# TYPE go_info gauge
go_info{version="go1.17.1"} 1

# HELP go_memstats_alloc_bytes Number of bytes allocated and still in use.
# TYPE go_memstats_alloc_bytes gauge
go_memstats_alloc_bytes 4.493744e+06

# HELP go_memstats_alloc_bytes_total Total number of bytes allocated, even if freed.
# TYPE go_memstats_alloc_bytes_total counter
go_memstats_alloc_bytes_total 2.1698216e+07

# HELP go_memstats_buck_hash_sys_bytes Number of bytes used by the profiling bucket hash table.
# TYPE go_memstats_buck_hash_sys_bytes gauge
go_memstats_buck_hash_sys_bytes 1.456234e+06

# HELP go_memstats_frees_total Total number of frees.
# TYPE go_memstats_frees_total counter
go_memstats_frees_total 172118

# HELP go_memstats_gc_cpu_fraction The fraction of this program's available CPU time used by the GC since the program started.
# TYPE go_memstats_gc_cpu_fraction gauge
go_memstats_gc_cpu_fraction 1.0749468700447189e-05

# HELP go_memstats_gc_sys_bytes Number of bytes used for garbage collection system metadata.
# TYPE go_memstats_gc_sys_bytes gauge
go_memstats_gc_sys_bytes 5.530048e+06

# HELP go_memstats_heap_alloc_bytes Number of heap bytes allocated and still in use.
# TYPE go_memstats_heap_alloc_bytes gauge
go_memstats_heap_alloc_bytes 4.493744e+06

# HELP go_memstats_heap_idle_bytes Number of heap bytes waiting to be used.
# TYPE go_memstats_heap_idle_bytes gauge
go_memstats_heap_idle_bytes 5.8236928e+07

# HELP go_memstats_heap_inuse_bytes Number of heap bytes that are in use.
# TYPE go_memstats_heap_inuse_bytes gauge
go_memstats_heap_inuse_bytes 7.528448e+06

# HELP go_memstats_heap_objects Number of allocated objects.
# TYPE go_memstats_heap_objects gauge
go_memstats_heap_objects 26306

# HELP go_memstats_heap_released_bytes Number of heap bytes released to OS.
# TYPE go_memstats_heap_released_bytes gauge
go_memstats_heap_released_bytes 5.7401344e+07

# HELP go_memstats_heap_sys_bytes Number of heap bytes obtained from system.
# TYPE go_memstats_heap_sys_bytes gauge
go_memstats_heap_sys_bytes 6.5765376e+07

# HELP go_memstats_last_gc_time_seconds Number of seconds since 1970 of last garbage collection.
# TYPE go_memstats_last_gc_time_seconds gauge
go_memstats_last_gc_time_seconds 1.6311727586032727e+09

# HELP go_memstats_lookups_total Total number of pointer lookups.
# TYPE go_memstats_lookups_total counter
go_memstats_lookups_total 0

# HELP go_memstats_mallocs_total Total number of mallocs.
# TYPE go_memstats_mallocs_total counter
go_memstats_mallocs_total 198424

# HELP go_memstats_mcache_inuse_bytes Number of bytes in use by mcache structures.
# TYPE go_memstats_mcache_inuse_bytes gauge
go_memstats_mcache_inuse_bytes 14400

# HELP go_memstats_mcache_sys_bytes Number of bytes used for mcache structures obtained from system.
# TYPE go_memstats_mcache_sys_bytes gauge
go_memstats_mcache_sys_bytes 16384

# HELP go_memstats_mspan_inuse_bytes Number of bytes in use by mspan structures.
# TYPE go_memstats_mspan_inuse_bytes gauge
go_memstats_mspan_inuse_bytes 191896

# HELP go_memstats_mspan_sys_bytes Number of bytes used for mspan structures obtained from system.
# TYPE go_memstats_mspan_sys_bytes gauge
go_memstats_mspan_sys_bytes 212992

# HELP go_memstats_next_gc_bytes Number of heap bytes when next garbage collection will take place.
# TYPE go_memstats_next_gc_bytes gauge
go_memstats_next_gc_bytes 8.689632e+06

# HELP go_memstats_other_sys_bytes Number of bytes used for other system allocations.
# TYPE go_memstats_other_sys_bytes gauge
go_memstats_other_sys_bytes 2.566622e+06

# HELP go_memstats_stack_inuse_bytes Number of bytes in use by the stack allocator.
# TYPE go_memstats_stack_inuse_bytes gauge
go_memstats_stack_inuse_bytes 1.343488e+06

# HELP go_memstats_stack_sys_bytes Number of bytes obtained from system for stack allocator.
# TYPE go_memstats_stack_sys_bytes gauge
go_memstats_stack_sys_bytes 1.343488e+06

# HELP go_memstats_sys_bytes Number of bytes obtained from system.
# TYPE go_memstats_sys_bytes gauge
go_memstats_sys_bytes 7.6891144e+07

# HELP go_threads Number of OS threads created.
# TYPE go_threads gauge
go_threads 18
```

!!! Note
    `cnp_collector_postgres_version` is a GaugeVec metric containing
    the `Major.Minor` version of PostgreSQL. The full semantic version
    `Major.Minor.Patch` can be found inside one of its label field
    named `full`.

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

!!! Important
    When a user defined metric overwrites an already existing metric the instance manager prints a json warning log,
    containing the message:`Query with the same name already found. Overwriting the existing one.`
    and a key `queryName` containing the overwritten query name.

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
              END AS lag,
              pg_is_in_recovery() AS in_recovery,
              EXISTS (TABLE pg_stat_wal_receiver) AS is_wal_receiver_up,
              (SELECT count(*) FROM pg_stat_replication) AS streaming_replicas"

      metrics:
        - lag:
            usage: "GAUGE"
            description: "Replication lag behind primary in seconds"
        - in_recovery:
            usage: "GAUGE"
            description: "Whether the instance is in recovery"
        - is_wal_receiver_up:
            usage: "GAUGE"
            description: "Whether the instance wal_receiver is up"
        - streaming_replicas:
            usage: "GAUGE"
            description: "Number of streaming replicas connected to the instance"
```

A list of basic monitoring queries can be found in the [`cnp-basic-monitoring.yaml` file](
./samples/cnp-basic-monitoring.yaml).

#### Example of a user defined metric running on multiple databases

If the `target_databases` option lists more than one database
the metric is collected from each of them.

Database auto-discovery can be enabled for a specific query by specifying a
*shell-like pattern* (i.e., containing `*`, `?` or `[]`) in the list of
`target_databases`. If provided, the operator will expand the list of target
databases by adding all the databases returned by the execution of `SELECT
datname FROM pg_database WHERE datallowconn AND NOT datistemplate` and matching
the pattern according to [path.Match()](https://pkg.go.dev/path#Match) rules.

!!! Note
    The `*` character has a [special meaning](https://yaml.org/spec/1.2/spec.html#id2786448) in yaml,
    so you need to quote (`"*"`) the `target_databases` value when it includes such a pattern.

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

Here is an example of a query with auto-discovery enabled which also
runs on the `template1` database (otherwise not returned by the
aforementioned query):

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
    - "*"
    - "template1"
```

The above example will produce the following metrics (provided the databases exist):

```text
cnp_some_query_rows{datname="albert"} 2
cnp_some_query_rows{datname="bb"} 5
cnp_some_query_rows{datname="freddie"} 10
cnp_some_query_rows{datname="template1"} 7
cnp_some_query_rows{datname="postgres"} 42
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
    - `runonserver`: a semantic version range to limit the versions of PostgreSQL the query should run on
       (e.g. `">=10.0.0"` or `">=12.0.0 <=14.0.0"`)
    - `target_databases`: a list of databases to run the `query` against,
      or a [shell-like pattern](#example-of-a-user-defined-metric-running-on-multiple-databases)
      to enable auto discovery. Overwrites the default database if provided.
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
| `HISTOGRAM`         | use this column as a histogram                          |


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
# HELP cnp_pg_replication_in_recovery Whether the instance is in recovery
# TYPE cnp_pg_replication_in_recovery gauge
cnp_pg_replication_in_recovery 0
# HELP cnp_pg_replication_lag Replication lag behind primary in seconds
# TYPE cnp_pg_replication_lag gauge
cnp_pg_replication_lag 0
# HELP cnp_pg_replication_streaming_replicas Number of streaming replicas connected to the instance
# TYPE cnp_pg_replication_streaming_replicas gauge
cnp_pg_replication_streaming_replicas 2
# HELP cnp_pg_replication_is_wal_receiver_up Whether the instance wal_receiver is up
# TYPE cnp_pg_replication_is_wal_receiver_up gauge
cnp_pg_replication_is_wal_receiver_up 0
```

### Differences with the Prometheus Postgres exporter

Cloud Native PostgreSQL is inspired by the PostgreSQL Prometheus Exporter, but
presents some differences. In particular, the following fields of a metric that
are defined in the official Prometheus exporter are not implemented in Cloud
Native PostgreSQL's exporter:

- `cache_seconds`: number of seconds to cache the result of the query

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

