
# Monitoring

!!! Important
    Installing Prometheus and Grafana is beyond the scope of this project.
    We assume they are correctly installed in your system. However, for
    experimentation we provide instructions in 
    [Part 4 of the Quickstart](quickstart.md#part-4-monitor-clusters-with-prometheus-and-grafana).

## Monitoring Instances

For each PostgreSQL instance, the operator provides an exporter of metrics for
[Prometheus](https://prometheus.io/) via HTTP, on port 9187, named `metrics`.
The operator comes with a [predefined set of metrics](#predefined-set-of-metrics), as well as a highly
configurable and customizable system to define additional queries via one or
more `ConfigMap` or `Secret` resources (see the
["User defined metrics" section](#user-defined-metrics) below for details).

!!! Important
    Starting from version 1.11, CloudNativePG already installs
    [by default a set of predefined metrics](#default-set-of-metrics) in
    a `ConfigMap` called `default-monitoring`.

!!! Info
    You can inspect the exported metrics by following the instructions in
    the ["How to inspect the exported metrics"](#how-to-inspect-the-exported-metrics)
    section below.

All monitoring queries that are performed on PostgreSQL are:

- atomic (one transaction per query)
- executed with the `pg_monitor` role
- executed with `application_name` set to `cnpg_metrics_exporter`
- executed as user `postgres`

Please refer to the "Default roles" section in PostgreSQL
[documentation](https://www.postgresql.org/docs/current/default-roles.html)
for details on the `pg_monitor` role.

Queries, by default, are run against the *main database*, as defined by
the specified `bootstrap` method of the `Cluster` resource, according
to the following logic:

- using `initdb`: queries will be run by default against the specified database
  in `initdb.database`, or `app` if not specified
- using `recovery`: queries will be run by default against the specified database
  in `recovery.database`, or `postgres` if not specified
- using `pg_basebackup`: queries will be run by default against the specified database
  in `pg_basebackup.database`, or `postgres` if not specified

The default database can always be overridden for a given user-defined metric,
by specifying a list of one or more databases in the `target_databases` option.

!!! Seealso "Prometheus/Grafana"
    If you are interested in evaluating the integration of CloudNativePG
    with Prometheus and Grafana, you can find a quick setup guide
    in [Part 4 of the quickstart](quickstart.md#part-4-monitor-clusters-with-prometheus-and-grafana)

### Prometheus Operator example

A specific PostgreSQL cluster can be monitored using the
[Prometheus Operator's](https://github.com/prometheus-operator/prometheus-operator) resource 
[PodMonitor](https://github.com/prometheus-operator/prometheus-operator/blob/v0.47.1/Documentation/api.md#podmonitor).
A PodMonitor correctly pointing to a Cluster can be automatically created by the operator by setting
`.spec.monitoring.enablePodMonitor` to `true` in the Cluster resource itself (default: false).

!!! Important
    Any change to the `PodMonitor` created automatically will be overridden by the Operator at the next reconciliation
    cycle, in case you need to customize it, you can do so as described below.

To deploy a `PodMonitor` for a specific Cluster manually, you can just define it as follows, changing it as needed:
```yaml
apiVersion: monitoring.coreos.com/v1
kind: PodMonitor
metadata:
  name: cluster-example
spec:
  selector:
    matchLabels:
      "cnpg.io/cluster": cluster-example
  podMetricsEndpoints:
  - port: metrics
```

!!! Important
    Make sure you modify the example above with a unique name as well as the
    correct cluster's namespace and labels (we are using `cluster-example`).

!!! Important
    Label `postgresql`, used in previous versions of this document, is deprecated
    and will be removed in the future. Please use the label `cnpg.io/cluster`
    instead to select the instances.

### Predefined set of metrics

Every PostgreSQL instance exporter automatically exposes a set of predefined
metrics, which can be classified in two major categories:

- PostgreSQL related metrics, starting with `cnpg_collector_*`, including:

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
# HELP cnpg_collector_collection_duration_seconds Collection time duration in seconds
# TYPE cnpg_collector_collection_duration_seconds gauge
cnpg_collector_collection_duration_seconds{collector="Collect.up"} 0.0031393

# HELP cnpg_collector_collections_total Total number of times PostgreSQL was accessed for metrics.
# TYPE cnpg_collector_collections_total counter
cnpg_collector_collections_total 2

# HELP cnpg_collector_last_collection_error 1 if the last collection ended with error, 0 otherwise.
# TYPE cnpg_collector_last_collection_error gauge
cnpg_collector_last_collection_error 0

# HELP cnpg_collector_manual_switchover_required 1 if a manual switchover is required, 0 otherwise
# TYPE cnpg_collector_manual_switchover_required gauge
cnpg_collector_manual_switchover_required 0

# HELP cnpg_collector_pg_wal Total size in bytes of WAL segments in the '/var/lib/postgresql/data/pgdata/pg_wal' directory  computed as (wal_segment_size * count)
# TYPE cnpg_collector_pg_wal gauge
cnpg_collector_pg_wal{value="count"} 9
cnpg_collector_pg_wal{value="slots_max"} NaN
cnpg_collector_pg_wal{value="keep"} 32
cnpg_collector_pg_wal{value="max"} 64
cnpg_collector_pg_wal{value="min"} 5
cnpg_collector_pg_wal{value="size"} 1.50994944e+08
cnpg_collector_pg_wal{value="volume_max"} 128
cnpg_collector_pg_wal{value="volume_size"} 2.147483648e+09

# HELP cnpg_collector_pg_wal_archive_status Number of WAL segments in the '/var/lib/postgresql/data/pgdata/pg_wal/archive_status' directory (ready, done)
# TYPE cnpg_collector_pg_wal_archive_status gauge
cnpg_collector_pg_wal_archive_status{value="done"} 6
cnpg_collector_pg_wal_archive_status{value="ready"} 0

# HELP cnpg_collector_replica_mode 1 if the cluster is in replica mode, 0 otherwise
# TYPE cnpg_collector_replica_mode gauge
cnpg_collector_replica_mode 0

# HELP cnpg_collector_sync_replicas Number of requested synchronous replicas (synchronous_standby_names)
# TYPE cnpg_collector_sync_replicas gauge
cnpg_collector_sync_replicas{value="expected"} 0
cnpg_collector_sync_replicas{value="max"} 0
cnpg_collector_sync_replicas{value="min"} 0
cnpg_collector_sync_replicas{value="observed"} 0

# HELP cnpg_collector_up 1 if PostgreSQL is up, 0 otherwise.
# TYPE cnpg_collector_up gauge
cnpg_collector_up{cluster="cluster-example"} 1

# HELP cnpg_collector_postgres_version Postgres version
# TYPE cnpg_collector_postgres_version gauge
cnpg_collector_postgres_version{cluster="cluster-example",full="13.4.0"} 13.4

# HELP cnpg_collector_first_recoverability_point The first point of recoverability for the cluster as a unix timestamp
# TYPE cnpg_collector_first_recoverability_point gauge
cnpg_collector_first_recoverability_point 1.63238406e+09

# HELP cnpg_collector_lo_pages Estimated number of pages in the pg_largeobject table
# TYPE cnpg_collector_lo_pages gauge
cnpg_collector_lo_pages{datname="app"} 0
cnpg_collector_lo_pages{datname="postgres"} 78

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
    `cnpg_collector_postgres_version` is a GaugeVec metric containing
    the `Major.Minor` version of PostgreSQL. The full semantic version
    `Major.Minor.Patch` can be found inside one of its label field
    named `full`.

!!! Note
    `cnpg_collector_first_recoverability_point` will be zero until
    your first backup to the object store. This is separate from
    the WAL archival.

### User defined metrics

This feature is currently in *beta* state and the format is inspired by the
[queries.yaml file](https://github.com/prometheus-community/postgres_exporter/blob/master/queries.yaml) <!-- wokeignore:rule=master -->
of the PostgreSQL Prometheus Exporter.

Custom metrics can be defined by users by referring to the created `Configmap`/`Secret` in a `Cluster` definition
under the `.spec.monitoring.customQueriesConfigMap` or `customQueriesSecret` section as in the following example:

```yaml
apiVersion: postgresql.cnpg.io/v1
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
    add a label with key `cnpg.io/reload` to it, otherwise you will have to reload
    the instances using the `kubectl cnpg reload` subcommand.

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
    cnpg.io/reload: ""
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

A list of basic monitoring queries can be found in the
[`default-monitoring.yaml` file](https://github.com/cloudnative-pg/cloudnative-pg/blob/main/config/manager/default-monitoring.yaml)
that is already installed in your CloudNativePG deployment (see ["Default set of metrics"](#default-set-of-metrics)).

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
cnpg_some_query_rows{datname="albert"} 2
cnpg_some_query_rows{datname="bb"} 5
cnpg_some_query_rows{datname="freddie"} 10
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
cnpg_some_query_rows{datname="albert"} 2
cnpg_some_query_rows{datname="bb"} 5
cnpg_some_query_rows{datname="freddie"} 10
cnpg_some_query_rows{datname="template1"} 7
cnpg_some_query_rows{datname="postgres"} 42
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
       (e.g. `">=11.0.0"` or `">=12.0.0 <=15.0.0"`)
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
cnpg_<MetricName>_<ColumnName>{<LabelColumnName>=<LabelColumnValue> ... } <ColumnValue>
```

!!! Note
    `LabelColumnName` are metrics with `usage` set to `LABEL` and their `Value`


Considering the `pg_replication` example above, the exporter's endpoint would
return the following output when invoked:

```text
# HELP cnpg_pg_replication_in_recovery Whether the instance is in recovery
# TYPE cnpg_pg_replication_in_recovery gauge
cnpg_pg_replication_in_recovery 0
# HELP cnpg_pg_replication_lag Replication lag behind primary in seconds
# TYPE cnpg_pg_replication_lag gauge
cnpg_pg_replication_lag 0
# HELP cnpg_pg_replication_streaming_replicas Number of streaming replicas connected to the instance
# TYPE cnpg_pg_replication_streaming_replicas gauge
cnpg_pg_replication_streaming_replicas 2
# HELP cnpg_pg_replication_is_wal_receiver_up Whether the instance wal_receiver is up
# TYPE cnpg_pg_replication_is_wal_receiver_up gauge
cnpg_pg_replication_is_wal_receiver_up 0
```

### Default set of metrics

The operator can be configured to automatically inject in a Cluster a set of 
monitoring queries defined in a ConfigMap or a Secret, inside the operator's namespace.
You have to set the `MONITORING_QUERIES_CONFIGMAP` or
`MONITORING_QUERIES_SECRET` key in the ["operator configuration"](operator_conf.md),
respectively to the name of the ConfigMap or the Secret;
the operator will then use the content of the `queries` key.

Any change to the `queries` content will be immediately reflected on all the
deployed Clusters using it.

The operator installation manifests come with a predefined ConfigMap, 
called `cnpg-default-monitoring`, to be used by all Clusters.
`MONITORING_QUERIES_CONFIGMAP` is by default set to `cnpg-default-monitoring` in the operator configuration.

If you want to disable the default set of metrics, you can:
- disable it at operator level: set the `MONITORING_QUERIES_CONFIGMAP`/`MONITORING_QUERIES_SECRET` key to `""`
  (empty string), in the operator ConfigMap. Changes to operator ConfigMap require an operator restart.
- disable it for a specific Cluster: set `.spec.monitoring.disableDefaultQueries` to `true` in the Cluster.

!!! Important
    The ConfigMap or Secret specified via `MONITORING_QUERIES_CONFIGMAP`/`MONITORING_QUERIES_SECRET`
    will always be copied to the Cluster's namespace with a fixed name: `cnpg-default-monitoring`.
    So that, if you intend to have default metrics, you should not create a ConfigMap with this name in the cluster's namespace.

### Differences with the Prometheus Postgres exporter

CloudNativePG is inspired by the PostgreSQL Prometheus Exporter, but
presents some differences. In particular, the `cache_seconds` field is not implemented
in CloudNativePG's exporter.

## Monitoring the operator

The operator internally exposes [Prometheus](https://prometheus.io/) metrics
via HTTP on port 8080, named `metrics`.

!!! Info
    You can inspect the exported metrics by following the instructions in
    the ["How to inspect the exported metrics"](#how-to-inspect-the-exported-metrics)
    section below.

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
  name: cnpg-controller-manager
spec:
  selector:
    matchLabels:
      app.kubernetes.io/name: cloudnative-pg
  podMetricsEndpoints:
    - port: metrics
```

## How to inspect the exported metrics

In this section we provide some basic instructions on how to inspect
the metrics exported by a specific PostgreSQL instance manager (primary
or replica) or the operator, using a temporary pod running `curl` in
the same namespace.

!!! Note
    In the example below we assume we are working in the default namespace,
    alongside with the PostgreSQL cluster. Please feel free to adapt
    this example to your use case, by applying basic Kubernetes knowledge.

Create the `curl.yaml` file with this content:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: curl
spec:
  containers:
  - name: curl
    image: curlimages/curl:7.84.0
    command: ['sleep', '3600']
```

Then create the pod:

```shell
kubectl apply -f curl.yaml
```

In case you want to inspect the metrics exported by an instance, you need
to connect to port 9187 of the target pod. This is the generic command to be
run (make sure you use the correct IP for the pod):

```shell
kubectl exec -ti curl -- curl -s <pod_ip>:9187/metrics
```

For example, if your PostgreSQL cluster is called `cluster-example` and
you want to retrieve the exported metrics of the first pod in the cluster,
you can run the following command to programmatically get the IP of
that pod:

```shell
POD_IP=$(kubectl get pod cluster-example-1 --template '{{.status.podIP}}')
```

And then run:

```shell
kubectl exec -ti curl -- curl -s ${POD_IP}:9187/metrics
```

In case you want to access the metrics of the operator, you need to point
to the pod where the operator is running, and use TCP port 8080 as target.

At the end of the inspection, please make sure you delete the `curl` pod:

```shell
kubectl delete -f curl.yaml
```

## Auxiliary resources

!!! Important
    These resources are provided for illustration and experimentation, and do
    not represent any kind of recommendation for your production system

In the [`doc/src/samples/monitoring/`](https://github.com/cloudnative-pg/cloudnative-pg/tree/main/docs/src/samples/monitoring)
directory you will find a series of sample files for observability.
Please refer to [Part 4 of the quickstart](quickstart.md#part-4-monitor-clusters-with-prometheus-and-grafana)
section for context:

- `kube-stack-config.yaml`: a configuration file for the kube-stack helm chart
  installation. It ensures that Prometheus listens for all PodMonitor resources.
- `cnpg-prometheusrule.yaml`: a `PrometheusRule` with alerts for CloudNativePG.
  NOTE: this does not include inter-operation with notification services. Please refer
  to the [Prometheus documentation](https://prometheus.io/docs/alerting/latest/alertmanager/).
- `grafana-configmap.yaml`: a ConfigMap containing the definition of the sample
  CloudNativePG Dashboard. Note the labels in the definition, which ensure that
  the Grafana deployment will find the ConfigMap.

In addition, we provide the "raw" sources for the Grafana dashboard and the
Prometheus alert rules, for your reference:

- `alerts.yaml`: Prometheus rules with alerts
- `grafana-dashboard.json`: the CloudNativePG dashboard as a native Grafana JSON.

Note that, for the configuration of `kube-prometheus-stack`, other fields and
settings are available over what we provide in `kube-stack-config.yaml`.

You can execute `helm show values prometheus-community/kube-prometheus-stack`
to view them. For further information, please refer to the
[kube-prometheus-stack](https://github.com/prometheus-community/helm-charts/tree/main/charts/kube-prometheus-stack)
page.
