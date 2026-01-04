---
id: monitoring
sidebar_position: 300
title: Monitoring
---

# Monitoring
<!-- SPDX-License-Identifier: CC-BY-4.0 -->

:::info[Important]
    Installing Prometheus and Grafana is beyond the scope of this project.
    We assume they are correctly installed in your system. However, for
    experimentation we provide instructions in
    [Part 4 of the Quickstart](quickstart.md#part-4-monitor-clusters-with-prometheus-and-grafana).
:::

## Monitoring Instances

For each PostgreSQL instance, the operator provides an exporter of metrics for
[Prometheus](https://prometheus.io/) via HTTP or HTTPS, on port 9187, named `metrics`.
The operator comes with a [predefined set of metrics](#predefined-set-of-metrics), as well as a highly
configurable and customizable system to define additional queries via one or
more `ConfigMap` or `Secret` resources (see the
["User defined metrics" section](#user-defined-metrics) below for details).

:::info[Important]
    CloudNativePG, by default, installs a set of [predefined metrics](#default-set-of-metrics)
    in a `ConfigMap` named `cnpg-default-monitoring`.
:::

:::info
    You can inspect the exported metrics by following the instructions in
    the ["How to inspect the exported metrics"](#how-to-inspect-the-exported-metrics)
    section below.
:::

All monitoring queries that are performed on PostgreSQL are:

- atomic (one transaction per query)
- executed with the `pg_monitor` role
- executed with `application_name` set to `cnpg_metrics_exporter`
- executed as user `postgres`

Please refer to the "Predefined Roles" section in PostgreSQL
[documentation](https://www.postgresql.org/docs/current/predefined-roles.html)
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

:::note[Prometheus/Grafana]
    If you are interested in evaluating the integration of CloudNativePG
    with Prometheus and Grafana, you can find a quick setup guide
    in [Part 4 of the quickstart](quickstart.md#part-4-monitor-clusters-with-prometheus-and-grafana)
:::

### Monitoring with the Prometheus operator

You can monitor a specific PostgreSQL cluster using the
[Prometheus Operator's](https://github.com/prometheus-operator/prometheus-operator)
[`PodMonitor` resource](https://github.com/prometheus-operator/prometheus-operator/blob/main/Documentation/api-reference/api.md#monitoring.coreos.com/v1.PodMonitor).

The recommended approach is to manually create and manage a `PodMonitor` for
each CloudNativePG cluster. This method provides you with full control over the
monitoring configuration and lifecycle.

#### Creating a `PodMonitor`

To monitor your cluster, define a `PodMonitor` resource as follows. Be sure to
deploy it in the same namespace where your Prometheus Operator is configured to
find `PodMonitor` resources.

```yaml
apiVersion: monitoring.coreos.com/v1
kind: PodMonitor
metadata:
  name: cluster-example
spec:
  selector:
    matchLabels:
      cnpg.io/cluster: cluster-example
  podMetricsEndpoints:
  - port: metrics
```

:::info[Important Configuration Details]
    - `metadata.name`: Give your `PodMonitor` a unique name.
    - `spec.namespaceSelector`: Use this to specify the namespace where
      your PostgreSQL cluster is running.
    - `spec.selector.matchLabels`: You must use the `cnpg.io/cluster: <cluster-name>`
      label to correctly target the PostgreSQL instances.
:::

#### Deprecation of Automatic `PodMonitor` Creation

:::warning[Feature Deprecation Notice]
    The `.spec.monitoring.enablePodMonitor` field in the `Cluster` resource is
    now deprecated and will be removed in a future version of the operator.
:::

If you are currently using this feature, we strongly recommend you either
remove or set `.spec.monitoring.enablePodMonitor` to `false` and manually
create a `PodMonitor` resource for your cluster as described above.
This change ensures that you have complete ownership of your monitoring
configuration, preventing it from being managed or overwritten by the operator.

### Enabling TLS on the Metrics Port

To enable TLS communication on the metrics port, configure the `.spec.monitoring.tls.enabled`
setting to `true`. This setup ensures that the metrics exporter uses the same
server certificate used by PostgreSQL to secure communication on port 5432.

:::info[Important]
    Changing the `.spec.monitoring.tls.enabled` setting will trigger a rolling restart of the Cluster.
:::

If the `PodMonitor` is managed by the operator (`.spec.monitoring.enablePodMonitor` set to `true`),
it will automatically contain the necessary configurations to access the metrics via TLS.

To manually deploy a `PodMonitor` suitable for reading metrics via TLS, define it as follows and
adjust as needed:

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
    scheme: https
    tlsConfig:
      ca:
        secret:
          name: cluster-example-ca
          key: ca.crt
      serverName: cluster-example-rw
```

:::info[Important]
    Ensure you modify the example above with a unique name, as well as the
    correct Cluster's namespace and labels (e.g., `cluster-example`).
:::

:::info[Important]
    The `serverName` field in the metrics endpoint must match one of the names
    defined in the server certificate. If the default certificate is in use,
    the `serverName` value should be in the format `<cluster-name>-rw`.
:::

### Predefined set of metrics

Every PostgreSQL instance exporter automatically exposes a set of predefined
metrics, which can be classified in two major categories:

- PostgreSQL related metrics, starting with `cnpg_collector_*`, including:

    - number of WAL files and total size on disk
    - number of `.ready` and `.done` files in the archive status folder
    - requested minimum and maximum number of synchronous replicas, as well as
      the expected and actually observed values
    - number of distinct nodes accommodating the instances
    - timestamps indicating last failed and last available backup, as well
      as the first point of recoverability for the cluster
    - flag indicating if replica cluster mode is enabled or disabled
    - flag indicating if a manual switchover is required
    - flag indicating if fencing is enabled or disabled

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

# HELP cnpg_collector_fencing_on 1 if the instance is fenced, 0 otherwise
# TYPE cnpg_collector_fencing_on gauge
cnpg_collector_fencing_on 0

# HELP cnpg_collector_nodes_used NodesUsed represents the count of distinct nodes accommodating the instances. A value of '-1' suggests that the metric is not available. A value of '1' suggests that all instances are hosted on a single node, implying the absence of High Availability (HA). Ideally this value should match the number of instances in the cluster.
# TYPE cnpg_collector_nodes_used gauge
cnpg_collector_nodes_used 3

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
cnpg_collector_postgres_version{cluster="cluster-example",full="18.1"} 18.1

# HELP cnpg_collector_last_failed_backup_timestamp The last failed backup as a unix timestamp (Deprecated)
# TYPE cnpg_collector_last_failed_backup_timestamp gauge
cnpg_collector_last_failed_backup_timestamp 0

# HELP cnpg_collector_last_available_backup_timestamp The last available backup as a unix timestamp (Deprecated)
# TYPE cnpg_collector_last_available_backup_timestamp gauge
cnpg_collector_last_available_backup_timestamp 1.63238406e+09

# HELP cnpg_collector_first_recoverability_point The first point of recoverability for the cluster as a unix timestamp (Deprecated)
# TYPE cnpg_collector_first_recoverability_point gauge
cnpg_collector_first_recoverability_point 1.63238406e+09

# HELP cnpg_collector_lo_pages Estimated number of pages in the pg_largeobject table
# TYPE cnpg_collector_lo_pages gauge
cnpg_collector_lo_pages{datname="app"} 0
cnpg_collector_lo_pages{datname="postgres"} 78

# HELP cnpg_collector_wal_buffers_full Number of times WAL data was written to disk because WAL buffers became full. Only available on PG 14+
# TYPE cnpg_collector_wal_buffers_full gauge
cnpg_collector_wal_buffers_full{stats_reset="2023-06-19T10:51:27.473259Z"} 6472

# HELP cnpg_collector_wal_bytes Total amount of WAL generated in bytes. Only available on PG 14+
# TYPE cnpg_collector_wal_bytes gauge
cnpg_collector_wal_bytes{stats_reset="2023-06-19T10:51:27.473259Z"} 1.0035147e+07

# HELP cnpg_collector_wal_fpi Total number of WAL full page images generated. Only available on PG 14+
# TYPE cnpg_collector_wal_fpi gauge
cnpg_collector_wal_fpi{stats_reset="2023-06-19T10:51:27.473259Z"} 1474

# HELP cnpg_collector_wal_records Total number of WAL records generated. Only available on PG 14+
# TYPE cnpg_collector_wal_records gauge
cnpg_collector_wal_records{stats_reset="2023-06-19T10:51:27.473259Z"} 26178

# HELP cnpg_collector_wal_sync Number of times WAL files were synced to disk via issue_xlog_fsync request (if fsync is on and wal_sync_method is either fdatasync, fsync or fsync_writethrough, otherwise zero). Only available on PG 14+
# TYPE cnpg_collector_wal_sync gauge
cnpg_collector_wal_sync{stats_reset="2023-06-19T10:51:27.473259Z"} 37

# HELP cnpg_collector_wal_sync_time Total amount of time spent syncing WAL files to disk via issue_xlog_fsync request, in milliseconds (if track_wal_io_timing is enabled, fsync is on, and wal_sync_method is either fdatasync, fsync or fsync_writethrough, otherwise zero). Only available on PG 14+
# TYPE cnpg_collector_wal_sync_time gauge
cnpg_collector_wal_sync_time{stats_reset="2023-06-19T10:51:27.473259Z"} 0

# HELP cnpg_collector_wal_write Number of times WAL buffers were written out to disk via XLogWrite request. Only available on PG 14+
# TYPE cnpg_collector_wal_write gauge
cnpg_collector_wal_write{stats_reset="2023-06-19T10:51:27.473259Z"} 7243

# HELP cnpg_collector_wal_write_time Total amount of time spent writing WAL buffers to disk via XLogWrite request, in milliseconds (if track_wal_io_timing is enabled, otherwise zero). This includes the sync time when wal_sync_method is either open_datasync or open_sync. Only available on PG 14+
# TYPE cnpg_collector_wal_write_time gauge
cnpg_collector_wal_write_time{stats_reset="2023-06-19T10:51:27.473259Z"} 0

# HELP cnpg_last_error 1 if the last collection ended with error, 0 otherwise.
# TYPE cnpg_last_error gauge
cnpg_last_error 0

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
go_info{version="go1.20.5"} 1

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

:::note
    `cnpg_collector_postgres_version` is a GaugeVec metric containing
    the `Major.Minor` version of PostgreSQL. The full semantic version
    `Major.Minor.Patch` can be found inside one of its label field
    named `full`.
:::

:::warning
    The metrics `cnpg_collector_last_failed_backup_timestamp`,
    `cnpg_collector_last_available_backup_timestamp`, and
    `cnpg_collector_first_recoverability_point` have been deprecated starting
    from version 1.26. These metrics will continue to function with native backup
    solutions such as in-core Barman Cloud (deprecated) and volume snapshots. Note
    that for these cases, `cnpg_collector_first_recoverability_point` and
    `cnpg_collector_last_available_backup_timestamp` will remain zero until the
    first backup is completed to the object store. This is separate from WAL
    archiving.
:::

### User defined metrics

This feature is currently in *beta* state and the format is inspired by the
[queries.yaml file (release 0.12)](https://github.com/prometheus-community/postgres_exporter/blob/v0.12.1/queries.yaml)
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

:::note
    If you want ConfigMaps and Secrets to be **automatically** reloaded by instances, you can
    add a label with key `cnpg.io/reload` to it, otherwise you will have to reload
    the instances using the `kubectl cnpg reload` subcommand.
:::

:::info[Important]
    When a user defined metric overwrites an already existing metric the instance manager prints a json warning log,
    containing the message:`Query with the same name already found. Overwriting the existing one.`
    and a key `queryName` containing the overwritten query name.
:::

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
[`default-monitoring.yaml` file](https://raw.githubusercontent.com/cloudnative-pg/cloudnative-pg/main/config/manager/default-monitoring.yaml)
that is already installed in your CloudNativePG deployment (see ["Default set of metrics"](#default-set-of-metrics)).

#### Example of a user defined metric with predicate query

The `predicate_query` option allows the user to execute the `query` to collect the metrics only under the specified conditions.
To do so the user needs to provide a predicate query that returns at most one row with a single `boolean` column.

The predicate query is executed in the same transaction as the main query and against the same databases.

```yaml
some_query: |
  predicate_query: |
    SELECT 
      some_bool as predicate 
    FROM some_table
  query: |
    SELECT
     count(*) as rows
    FROM some_table
  metrics:
    - rows:
        usage: "GAUGE"
        description: "number of rows"
```

#### Example of a user defined metric running on multiple databases

If the `target_databases` option lists more than one database
the metric is collected from each of them.

Database auto-discovery can be enabled for a specific query by specifying a
*shell-like pattern* (i.e., containing `*`, `?` or `[]`) in the list of
`target_databases`. If provided, the operator will expand the list of target
databases by adding all the databases returned by the execution of `SELECT
datname FROM pg_database WHERE datallowconn AND NOT datistemplate` and matching
the pattern according to [path.Match()](https://pkg.go.dev/path#Match) rules.

:::note
    The `*` character has a [special meaning](https://yaml.org/spec/1.2/spec.html#id2786448) in yaml,
    so you need to quote (`"*"`) the `target_databases` value when it includes such a pattern.
:::

It is recommended that you always include the name of the database
in the returned labels, for example using the `current_database()` function
as in the following example:

```yaml
some_query: |
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
some_query: |
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
    - `name`: override `<MetricName>`, if defined
    - `query`: the SQL query to run on the target database to generate the metrics
    - `primary`: whether to run the query only on the primary instance
    - `master`: same as `primary` (for compatibility with the Prometheus PostgreSQL exporter's syntax - deprecated) <!-- wokeignore:rule=master -->
    - `runonserver`: a semantic version range to limit the versions of PostgreSQL the query should run on
       (e.g. `">=11.0.0"` or `">=12.0.0 <=15.0.0"`)
    - `target_databases`: a list of databases to run the `query` against,
      or a [shell-like pattern](#example-of-a-user-defined-metric-running-on-multiple-databases)
      to enable auto discovery. Overwrites the default database if provided.
    - `predicate_query`: a SQL query that returns at most one row and one `boolean` column to run on the target database.
       The system evaluates the predicate and if `true` executes the `query`. 
    - `metrics`: section containing a list of all exported columns, defined as follows:
      - `<ColumnName>`: the name of the column returned by the query
          - `name`: override the `ColumnName` of the column in the metric, if defined
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

:::note
    `LabelColumnName` are metrics with `usage` set to `LABEL` and their `Value`
:::

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

:::info[Important]
    The ConfigMap or Secret specified via `MONITORING_QUERIES_CONFIGMAP`/`MONITORING_QUERIES_SECRET`
    will always be copied to the Cluster's namespace with a fixed name: `cnpg-default-monitoring`.
    So that, if you intend to have default metrics, you should not create a ConfigMap with this name in the cluster's namespace.
:::

### Differences with the Prometheus Postgres exporter

CloudNativePG is inspired by the PostgreSQL Prometheus Exporter, but
presents some differences. In particular, the `cache_seconds` field is not implemented
in CloudNativePG's exporter.

## Monitoring the CloudNativePG operator

The operator internally exposes [Prometheus](https://prometheus.io/) metrics
via HTTP on port 8080, named `metrics`.

:::info
    You can inspect the exported metrics by following the instructions in
    the ["How to inspect the exported metrics"](#how-to-inspect-the-exported-metrics)
    section below.
:::

Currently, the operator exposes default `kubebuilder` metrics. See
[kubebuilder documentation](https://book.kubebuilder.io/reference/metrics.html)
for more details.

### Monitoring the operator with Prometheus

The operator can be monitored using the
[Prometheus Operator](https://github.com/prometheus-operator/prometheus-operator) by defining a
[PodMonitor](https://github.com/prometheus-operator/prometheus-operator/blob/v0.47.1/Documentation/api.md#podmonitor)
pointing to the operator pod(s), as follows (note it's applied in the same
namespace as the operator):

```yaml
kubectl -n cnpg-system apply -f - <<EOF
---
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
EOF
```

## How to inspect the exported metrics

In this section we provide basic instructions on how to inspect
the metrics exported by a specific PostgreSQL instance manager (primary
or replica) or the operator.

:::note
    In the examples below we assume we are working in the default namespace, and
    with the operator installed in the `cnpg-system` namespace.
    Please adapt to your use case.
:::

### Using port forwarding

The simplest way to inspect the metrics is to port-forward the metrics ports
of the pods involved.

For example, to inspect the metrics on the `-1` instance of `cluster-example`,
we port-forward the 9187 port:

``` sh
kubectl port-forward cluster-example-1 9187:9187
```

With port-forwarding active, the metrics can be inspected easily, for
example on a web browser, using HTTP or HTTPS depending on the configuration,
with address: `localhost:9187/metrics`.

The operator pod also exports metrics, on port 8080. Similarly to instances, we
port-forward the operator pod, which is located in the operator namespace:

``` sh
kubectl -n cnpg-system port-forward pod/<CONTROLLER-MANAGER-POD> 8080:8080
```

With port forwarding active, the metrics are easily viewable on a browser at
[`localhost:8080/metrics`](http://localhost:8080/metrics).

### Using curl

Create the `curl` pod with the following command:

```yaml
kubectl apply -f - <<EOF
---
apiVersion: v1
kind: Pod
metadata:
  name: curl
spec:
  containers:
  - name: curl
    image: curlimages/curl:8.17.0
    command: ['sleep', '3600']
EOF
```

To inspect the metrics exported by an instance, you need
to connect to port 9187 of the target pod. You will need to know the pod's
IP address, which you can find easily by running `kubectl get pod -o wide`.
The following generic command will run `curl` on the desired pod:

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

If you enabled TLS metrics, run instead:

```shell
kubectl exec -ti curl -- curl -sk https://${POD_IP}:9187/metrics
```

To access the metrics of the operator, you need to point
to the pod where the operator is running, and use TCP port 8080 as target.

When you're done inspecting metrics, please remember to delete the `curl` pod:

```shell
kubectl delete -f curl.yaml
```

## Auxiliary resources

:::info[Important]
    These resources are provided for illustration and experimentation, and do
    not represent any kind of recommendation for your production system
:::

In the [`doc/src/samples/monitoring/`](https://github.com/cloudnative-pg/cloudnative-pg/tree/main/docs/src/samples/monitoring)
directory you will find a series of sample files for observability.
Please refer to [Part 4 of the quickstart](quickstart.md#part-4-monitor-clusters-with-prometheus-and-grafana)
section for context:

- `kube-stack-config.yaml`: a configuration file for the kube-stack helm chart
  installation. It ensures that Prometheus listens for all PodMonitor resources.
- `prometheusrule.yaml`: a `PrometheusRule` with alerts for CloudNativePG.
  NOTE: this does not include inter-operation with notification services. Please refer
  to the [Prometheus documentation](https://prometheus.io/docs/alerting/latest/alertmanager/).
- `podmonitor.yaml`: a `PodMonitor` for the CloudNativePG Operator deployment.

In addition, we provide the "raw" sources for the Prometheus alert rules in the
`alerts.yaml` file.

A Grafana dashboard for CloudNativePG clusters and operator, is kept in the
dedicated repository [`cloudnative-pg/grafana-dashboards`](https://github.com/cloudnative-pg/grafana-dashboards/tree/main)
as a dashboard JSON configuration:
[`grafana-dashboard.json`](https://github.com/cloudnative-pg/grafana-dashboards/blob/main/charts/cluster/grafana-dashboard.json).
The file can be downloaded, and imported into Grafana
(menus: Dashboard > New > Import).

For a general reference on the settings available on `kube-prometheus-stack`,
you can execute `helm show values prometheus-community/kube-prometheus-stack`.
Please refer to the
[kube-prometheus-stack](https://github.com/prometheus-community/helm-charts/tree/main/charts/kube-prometheus-stack)
page for more detail.
