# Connection Pooling

CloudNativePG provides native support for connection pooling with
[PgBouncer](https://www.pgbouncer.org/), one of the most popular open source
connection poolers for PostgreSQL, through the `Pooler` CRD.

In a nutshell, a `Pooler` in CloudNativePG is a deployment of
PgBouncer pods that sits between your applications and a PostgreSQL service
(for example the `rw` service), creating a separate, scalable, configurable,
and highly available **database access layer**.

## Architecture

The following diagram highlights how the introduction of a database access
layer based on PgBouncer changes the architecture of CloudNativePG,
like an additional blade in a Swiss Army knife. Instead of directly connecting
to the PostgreSQL primary service, applications can now connect to the
equivalent service for PgBouncer, enabling reuse of existing connections for
faster performance and better resource management on the PostgreSQL side.

![Applications writing to the single primary via PgBouncer](./images/pgbouncer-architecture-rw.png)

## Quickstart

The easiest way to explain how CloudNativePG implements a PgBouncer
pooler is through an example:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Pooler
metadata:
  name: pooler-example-rw
spec:
  cluster:
    name: cluster-example

  instances: 3
  type: rw
  pgbouncer:
    poolMode: session
    parameters:
      max_client_conn: "1000"
      default_pool_size: "10"
```

!!! Important
    Pooler name should never match with any Cluster name within the same namespace.

This creates a new `Pooler` resource called `pooler-example-rw` (the name is
arbitrary) that is strictly associated with the Postgres `Cluster` resource called
`cluster-example` and pointing to the primary, identified by the read/write
service (`rw`, therefore `cluster-example-rw`).

The `Pooler` must live in the same namespace of the Postgres cluster.
It consists of a Kubernetes deployment of 3 pods running the
[latest stable image of PgBouncer](https://ghcr.io/cloudnative-pg/pgbouncer),
configured with the [`session` pooling mode](https://www.pgbouncer.org/config.html#pool-mode)
and accepting up to 1000 connections each - with a default pool size of 10
user/database pairs towards PostgreSQL.

!!! Important
    The `Pooler` only sets the `*` fallback database in PgBouncer, meaning
    that all parameters in the connection strings passed from the client are
    relayed to the PostgreSQL server (please refer to ["Section [databases]"
    in PgBouncer's documentation](https://www.pgbouncer.org/config.html#section-databases)).

Additionally, CloudNativePG automatically creates a secret with the
same name of the pooler containing the configuration files used with PgBouncer.

!!! Seealso "API reference"
    For details, please refer to [`PgBouncerSpec` section](api_reference.md#PgBouncerSpec)
    in the API reference.


## Pooler resource lifecycle

`Pooler` resources are not `Cluster`-managed resources. You are supposed to
create poolers manually when they are needed. Additionally, you can deploy
multiple poolers per PostgreSQL Cluster.

What is important to note is that the lifecycles of the `Cluster` and the
`Pooler` resources are currently independent: the deletion of the `Cluster`
doesn't imply the automatic deletion of the `Pooler`, and viceversa.

!!! Important
    Now that you know how a `Pooler` works, you have full freedom in terms of
    possible architectures: you can have clusters without poolers, clusters with
    a single pooler, or clusters with several poolers (i.e. one per application).

## Security

Any PgBouncer pooler is transparently integrated with CloudNativePG
support for in-transit encryption via **TLS connections**, both on the client
(application) and server (PostgreSQL) side of the pool.

Specifically, PgBouncer automatically reuses the certificates of the PostgreSQL
server. Moreover, it uses TLS client certificate authentication to connect
to the PostgreSQL server to run the `auth_query` for clients' password
authentication (see the ["Authentication" section](#authentication) below).

Containers run as the `pgbouncer` system user, and access to the `pgbouncer`
database is only allowed via local connections, through `peer` authentication.

### Certificates

By default, PgBouncer pooler will use the same certificates that are used by the
cluster itself, but if the user provides those certificates the pooler will accept
secrets with the following format:

1. Basic Auth
2. TLS
3. Opaque

In the Opaque case, it will look for specific keys that needs to be used, those keys
are the following:

* tls.crt
* tls.key

So we can treat this secret as a TLS secret, and start from there.

## Authentication

**Password based authentication** is the only supported method for clients of
PgBouncer in CloudNativePG.

Internally, our implementation relies on PgBouncer's `auth_user` and `auth_query` options. Specifically, the operator:

- creates a standard user called `cnpg_pooler_pgbouncer` in the PostgreSQL server
- creates the lookup function in the `postgres` database and grants execution
  privileges to the `cnpg_pooler_pgbouncer` user (PoLA)
- issues a TLS certificate for this user
- sets `cnpg_pooler_pgbouncer` as the `auth_user`
- configures PgBouncer to use the TLS certificate to authenticate
  `cnpg_pooler_pgbouncer` against the PostgreSQL server
- removes all the above when it detects that a cluster does not have
  any pooler associated to it

!!! Important
    If you specify your own secrets the operator will not automatically integrate the Pooler.

To manually integrate the Pooler, in the case that you have specified your own
secrets, you must run the following queries from inside your cluster.

First, you must create the role:


```sql
CREATE ROLE cnpg_pooler_pgbouncer WITH LOGIN;
```

Then, for each application database, grant the permission for
`cnpg_pooler_pgbouncer` to connect to it:

```sql
GRANT CONNECT ON DATABASE { database name here } TO cnpg_pooler_pgbouncer;
```

Finally, connect in each application database, then create the authentication
function inside each of the application databases:

```sql
CREATE OR REPLACE FUNCTION user_search(uname TEXT)
  RETURNS TABLE (usename name, passwd text)
  LANGUAGE sql SECURITY DEFINER AS
  'SELECT usename, passwd FROM pg_shadow WHERE usename=$1;';

REVOKE ALL ON FUNCTION user_search(text)
  FROM public;

GRANT EXECUTE ON FUNCTION user_search(text)
  TO cnpg_pooler_pgbouncer;
```


## PodTemplates

You can take advantage of pod templates specification in the `template`
section of a `Pooler` resource. For details, please refer to [`PoolerSpec`
section](api_reference.md#PoolerSpec) in the API reference.

Through templates you can configure pods as you like, including fine
control over affinity and anti-affinity rules for pods and nodes.
By default, containers use images from `ghcr.io/cloudnative-pg/pgbouncer`.

Here an example of Pooler specifying PodAntiAffinity:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Pooler
metadata:
  name: pooler-example-rw
spec:
  cluster:
    name: cluster-example
  instances: 3
  type: rw

  template:
    metadata:
      labels:
        app: pooler
    spec:
      containers: []
      affinity:
        podAntiAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
          - labelSelector:
              matchExpressions:
              - key: app
                operator: In
                values:
                - pooler
            topologyKey: "kubernetes.io/hostname"
```

!!! Note
    `.spec.template.spec.containers` has to be explicitly set to `[]` when not modified, as it's a required field for a `PodSpec`.
    If `.spec.template.spec.containers` is not set the kubernetes api-server will return the following error when trying to apply the manifest:
    `error validating "pooler.yaml": error validating data: ValidationError(Pooler.spec.template.spec): missing required field "containers"`



Here an example setting resources and changing the used image:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Pooler
metadata:
  name: pooler-example-rw
spec:
  cluster:
    name: cluster-example
  instances: 3
  type: rw

  template:
    metadata:
      labels:
        app: pooler
    spec:
      containers:
        - name: pgbouncer
          image: my-pgbouncer:latest
          resources:
            requests:
              cpu: "0.1"
              memory: 100Mi
            limits:
              cpu: "0.5"
              memory: 500Mi
```

## High Availability (HA)

Thanks to Kubernetes' deployments, you can configure your pooler to run
on a single instance or over multiple pods. The exposed service will
make sure that your clients are randomly distributed over the available
pods running PgBouncer - which will then automatically manage and reuse
connections towards the underlying server (if using the `rw` service)
or servers (if using the `ro` service with multiple replicas).

!!! Warning
    Please be aware of network hops in case your infrastructure spans
    multiple availability zones with high latency across them. Consider
    for example the case of your application running in zone 2,
    connecting to PgBouncer running in zone 3, pointing to the PostgreSQL
    primary in zone 1. 

## PgBouncer configuration options

The operator manages most of the [configuration options for PgBouncer](https://www.pgbouncer.org/config.html), allowing you to modify only a subset of them.

!!! Warning
    You are responsible to correctly set the value of each option, as the operator
    does not validate them.

Below you can find a list of the PgBouncer options you are allowed to
customize. Each of them contains a link to the PgBouncer documentation for that
specific parameter. Unless differently stated here, the default values are the
ones directly set by PgBouncer:

- [`application_name_add_host`](https://www.pgbouncer.org/config.html#application_name_add_host)
- [`autodb_idle_timeout`](https://www.pgbouncer.org/config.html#autodb_idle_timeout)
- [`client_idle_timeout`](https://www.pgbouncer.org/config.html#client_idle_timeout)
- [`client_login_timeout`](https://www.pgbouncer.org/config.html#client_login_timeout)
- [`default_pool_size`](https://www.pgbouncer.org/config.html#default_pool_size)
- [`disable_pqexec`](https://www.pgbouncer.org/config.html#disable_pqexec)
- [`idle_transaction_timeout`](https://www.pgbouncer.org/config.html#idle_transaction_timeout)
- [`ignore_startup_parameters`](https://www.pgbouncer.org/config.html#ignore_startup_parameters):
  to be appended to `extra_float_digits,options` - required by CNP
- [`log_connections`](https://www.pgbouncer.org/config.html#log_connections)
- [`log_disconnections`](https://www.pgbouncer.org/config.html#log_disconnections)
- [`log_pooler_errors`](https://www.pgbouncer.org/config.html#log_pooler_errors)
- [`log_stats`](https://www.pgbouncer.org/config.html#log_stats): by default
  disabled (`0`), given that statistics are already collected by the Prometheus
  export as described in the ["Monitoring"](#monitoring) section below
- [`max_client_conn`](https://www.pgbouncer.org/config.html#max_client_conn)
- [`max_db_connections`](https://www.pgbouncer.org/config.html#max_db_connections)
- [`max_user_connections`](https://www.pgbouncer.org/config.html#max_user_connections)
- [`min_pool_size`](https://www.pgbouncer.org/config.html#min_pool_size)
- [`query_timeout`](https://www.pgbouncer.org/config.html#query_timeout)
- [`query_wait_timeout`](https://www.pgbouncer.org/config.html#query_wait_timeout)
- [`reserve_pool_size`](https://www.pgbouncer.org/config.html#reserve_pool_size)
- [`reserve_pool_timeout`](https://www.pgbouncer.org/config.html#reserve_pool_timeout)
- [`server_check_delay`](https://www.pgbouncer.org/config.html#server_check_delay)
- [`server_check_query`](https://www.pgbouncer.org/config.html#server_check_query)
- [`server_connect_timeout`](https://www.pgbouncer.org/config.html#server_connect_timeout)
- [`server_fast_close`](https://www.pgbouncer.org/config.html#server_fast_close)
- [`server_idle_timeout`](https://www.pgbouncer.org/config.html#server_idle_timeout)
- [`server_lifetime`](https://www.pgbouncer.org/config.html#server_lifetime)
- [`server_login_retry`](https://www.pgbouncer.org/config.html#server_login_retry)
- [`server_reset_query`](https://www.pgbouncer.org/config.html#server_reset_query)
- [`server_reset_query_always`](https://www.pgbouncer.org/config.html#server_reset_query_always)
- [`server_round_robin`](https://www.pgbouncer.org/config.html#server_round_robin)
- [`stats_period`](https://www.pgbouncer.org/config.html#stats_period)
- [`tcp_keepalive`](https://www.pgbouncer.org/config.html#tcp_keepalive)
- [`tcp_keepcnt`](https://www.pgbouncer.org/config.html#tcp_keepcnt)
- [`tcp_keepidle`](https://www.pgbouncer.org/config.html#tcp_keepidle)
- [`tcp_keepintvl`](https://www.pgbouncer.org/config.html#tcp_keepintvl)
- [`tcp_user_timeout`](https://www.pgbouncer.org/config.html#tcp_user_timeout)
- [`verbose`](https://www.pgbouncer.org/config.html#verbose)

Customizations of the PgBouncer configuration are written
declaratively in the `.spec.pgbouncer.parameters` map.

The operator reacts to the changes in the Pooler specification,
and every PgBouncer instance reloads the updated configuration
without disrupting the service.

!!! Warning
    Every PgBouncer pod will have the same configuration, aligned
    with the parameters in the specification. A mistake in these
    parameters could disrupt the operability of the **whole Pooler**.
    The operator **does not** validate the value of any option.

## Monitoring

The PgBouncer implementation of the `Pooler` comes with a default
Prometheus exporter that automatically makes available several
metrics having the `cnpg_pgbouncer_` prefix, by running:

- `SHOW LISTS` (prefix: `cnpg_pgbouncer_lists`)
- `SHOW POOLS` (prefix: `cnpg_pgbouncer_pools`)
- `SHOW STATS` (prefix: `cnpg_pgbouncer_stats`)

Similarly to the CloudNativePG instance, the exporter runs on port
`9127` of each pod running PgBouncer, and also provides metrics related to the
Go runtime (with prefix `go_*`). You can debug the exporter on a pod running
PgBouncer through the following command:

```console
kubectl exec -ti <PGBOUNCER_POD> -- curl 127.0.0.1:9127/metrics
```

An example of the output for `cnpg_pgbouncer` metrics:

```text
# HELP cnpg_pgbouncer_collection_duration_seconds Collection time duration in seconds
# TYPE cnpg_pgbouncer_collection_duration_seconds gauge
cnpg_pgbouncer_collection_duration_seconds{collector="Collect.up"} 0.002443168

# HELP cnpg_pgbouncer_collections_total Total number of times PostgreSQL was accessed for metrics.
# TYPE cnpg_pgbouncer_collections_total counter
cnpg_pgbouncer_collections_total 1

# HELP cnpg_pgbouncer_last_collection_error 1 if the last collection ended with error, 0 otherwise.
# TYPE cnpg_pgbouncer_last_collection_error gauge
cnpg_pgbouncer_last_collection_error 0

# HELP cnpg_pgbouncer_lists_databases Count of databases.
# TYPE cnpg_pgbouncer_lists_databases gauge
cnpg_pgbouncer_lists_databases 1

# HELP cnpg_pgbouncer_lists_dns_names Count of DNS names in the cache.
# TYPE cnpg_pgbouncer_lists_dns_names gauge
cnpg_pgbouncer_lists_dns_names 0

# HELP cnpg_pgbouncer_lists_dns_pending Not used.
# TYPE cnpg_pgbouncer_lists_dns_pending gauge
cnpg_pgbouncer_lists_dns_pending 0

# HELP cnpg_pgbouncer_lists_dns_queries Count of in-flight DNS queries.
# TYPE cnpg_pgbouncer_lists_dns_queries gauge
cnpg_pgbouncer_lists_dns_queries 0

# HELP cnpg_pgbouncer_lists_dns_zones Count of DNS zones in the cache.
# TYPE cnpg_pgbouncer_lists_dns_zones gauge
cnpg_pgbouncer_lists_dns_zones 0

# HELP cnpg_pgbouncer_lists_free_clients Count of free clients.
# TYPE cnpg_pgbouncer_lists_free_clients gauge
cnpg_pgbouncer_lists_free_clients 49

# HELP cnpg_pgbouncer_lists_free_servers Count of free servers.
# TYPE cnpg_pgbouncer_lists_free_servers gauge
cnpg_pgbouncer_lists_free_servers 0

# HELP cnpg_pgbouncer_lists_login_clients Count of clients in login state.
# TYPE cnpg_pgbouncer_lists_login_clients gauge
cnpg_pgbouncer_lists_login_clients 0

# HELP cnpg_pgbouncer_lists_pools Count of pools.
# TYPE cnpg_pgbouncer_lists_pools gauge
cnpg_pgbouncer_lists_pools 1

# HELP cnpg_pgbouncer_lists_used_clients Count of used clients.
# TYPE cnpg_pgbouncer_lists_used_clients gauge
cnpg_pgbouncer_lists_used_clients 1

# HELP cnpg_pgbouncer_lists_used_servers Count of used servers.
# TYPE cnpg_pgbouncer_lists_used_servers gauge
cnpg_pgbouncer_lists_used_servers 0

# HELP cnpg_pgbouncer_lists_users Count of users.
# TYPE cnpg_pgbouncer_lists_users gauge
cnpg_pgbouncer_lists_users 2

# HELP cnpg_pgbouncer_pools_cl_active Client connections that are linked to server connection and can process queries.
# TYPE cnpg_pgbouncer_pools_cl_active gauge
cnpg_pgbouncer_pools_cl_active{database="pgbouncer",user="pgbouncer"} 1

# HELP cnpg_pgbouncer_pools_cl_cancel_req Client connections that have not forwarded query cancellations to the server yet.
# TYPE cnpg_pgbouncer_pools_cl_cancel_req gauge
cnpg_pgbouncer_pools_cl_cancel_req{database="pgbouncer",user="pgbouncer"} 0

# HELP cnpg_pgbouncer_pools_cl_waiting Client connections that have sent queries but have not yet got a server connection.
# TYPE cnpg_pgbouncer_pools_cl_waiting gauge
cnpg_pgbouncer_pools_cl_waiting{database="pgbouncer",user="pgbouncer"} 0

# HELP cnpg_pgbouncer_pools_maxwait How long the first (oldest) client in the queue has waited, in seconds. If this starts increasing, then the current pool of servers does not handle requests quickly enough. The reason may be either an overloaded server or just too small of a pool_size setting.
# TYPE cnpg_pgbouncer_pools_maxwait gauge
cnpg_pgbouncer_pools_maxwait{database="pgbouncer",user="pgbouncer"} 0

# HELP cnpg_pgbouncer_pools_maxwait_us Microsecond part of the maximum waiting time.
# TYPE cnpg_pgbouncer_pools_maxwait_us gauge
cnpg_pgbouncer_pools_maxwait_us{database="pgbouncer",user="pgbouncer"} 0

# HELP cnpg_pgbouncer_pools_pool_mode The pooling mode in use. 1 for session, 2 for transaction, 3 for statement, -1 if unknown
# TYPE cnpg_pgbouncer_pools_pool_mode gauge
cnpg_pgbouncer_pools_pool_mode{database="pgbouncer",user="pgbouncer"} 3

# HELP cnpg_pgbouncer_pools_sv_active Server connections that are linked to a client.
# TYPE cnpg_pgbouncer_pools_sv_active gauge
cnpg_pgbouncer_pools_sv_active{database="pgbouncer",user="pgbouncer"} 0

# HELP cnpg_pgbouncer_pools_sv_idle Server connections that are unused and immediately usable for client queries.
# TYPE cnpg_pgbouncer_pools_sv_idle gauge
cnpg_pgbouncer_pools_sv_idle{database="pgbouncer",user="pgbouncer"} 0

# HELP cnpg_pgbouncer_pools_sv_login Server connections currently in the process of logging in.
# TYPE cnpg_pgbouncer_pools_sv_login gauge
cnpg_pgbouncer_pools_sv_login{database="pgbouncer",user="pgbouncer"} 0

# HELP cnpg_pgbouncer_pools_sv_tested Server connections that are currently running either server_reset_query or server_check_query.
# TYPE cnpg_pgbouncer_pools_sv_tested gauge
cnpg_pgbouncer_pools_sv_tested{database="pgbouncer",user="pgbouncer"} 0

# HELP cnpg_pgbouncer_pools_sv_used Server connections that have been idle for more than server_check_delay, so they need server_check_query to run on them before they can be used again.
# TYPE cnpg_pgbouncer_pools_sv_used gauge
cnpg_pgbouncer_pools_sv_used{database="pgbouncer",user="pgbouncer"} 0

# HELP cnpg_pgbouncer_stats_avg_query_count Average queries per second in last stat period.
# TYPE cnpg_pgbouncer_stats_avg_query_count gauge
cnpg_pgbouncer_stats_avg_query_count{database="pgbouncer"} 1

# HELP cnpg_pgbouncer_stats_avg_query_time Average query duration, in microseconds.
# TYPE cnpg_pgbouncer_stats_avg_query_time gauge
cnpg_pgbouncer_stats_avg_query_time{database="pgbouncer"} 0

# HELP cnpg_pgbouncer_stats_avg_recv Average received (from clients) bytes per second.
# TYPE cnpg_pgbouncer_stats_avg_recv gauge
cnpg_pgbouncer_stats_avg_recv{database="pgbouncer"} 0

# HELP cnpg_pgbouncer_stats_avg_sent Average sent (to clients) bytes per second.
# TYPE cnpg_pgbouncer_stats_avg_sent gauge
cnpg_pgbouncer_stats_avg_sent{database="pgbouncer"} 0

# HELP cnpg_pgbouncer_stats_avg_wait_time Time spent by clients waiting for a server, in microseconds (average per second).
# TYPE cnpg_pgbouncer_stats_avg_wait_time gauge
cnpg_pgbouncer_stats_avg_wait_time{database="pgbouncer"} 0

# HELP cnpg_pgbouncer_stats_avg_xact_count Average transactions per second in last stat period.
# TYPE cnpg_pgbouncer_stats_avg_xact_count gauge
cnpg_pgbouncer_stats_avg_xact_count{database="pgbouncer"} 1

# HELP cnpg_pgbouncer_stats_avg_xact_time Average transaction duration, in microseconds.
# TYPE cnpg_pgbouncer_stats_avg_xact_time gauge
cnpg_pgbouncer_stats_avg_xact_time{database="pgbouncer"} 0

# HELP cnpg_pgbouncer_stats_total_query_count Total number of SQL queries pooled by pgbouncer.
# TYPE cnpg_pgbouncer_stats_total_query_count gauge
cnpg_pgbouncer_stats_total_query_count{database="pgbouncer"} 3

# HELP cnpg_pgbouncer_stats_total_query_time Total number of microseconds spent by pgbouncer when actively connected to PostgreSQL, executing queries.
# TYPE cnpg_pgbouncer_stats_total_query_time gauge
cnpg_pgbouncer_stats_total_query_time{database="pgbouncer"} 0

# HELP cnpg_pgbouncer_stats_total_received Total volume in bytes of network traffic received by pgbouncer.
# TYPE cnpg_pgbouncer_stats_total_received gauge
cnpg_pgbouncer_stats_total_received{database="pgbouncer"} 0

# HELP cnpg_pgbouncer_stats_total_sent Total volume in bytes of network traffic sent by pgbouncer.
# TYPE cnpg_pgbouncer_stats_total_sent gauge
cnpg_pgbouncer_stats_total_sent{database="pgbouncer"} 0

# HELP cnpg_pgbouncer_stats_total_wait_time Time spent by clients waiting for a server, in microseconds.
# TYPE cnpg_pgbouncer_stats_total_wait_time gauge
cnpg_pgbouncer_stats_total_wait_time{database="pgbouncer"} 0

# HELP cnpg_pgbouncer_stats_total_xact_count Total number of SQL transactions pooled by pgbouncer.
# TYPE cnpg_pgbouncer_stats_total_xact_count gauge
cnpg_pgbouncer_stats_total_xact_count{database="pgbouncer"} 3

# HELP cnpg_pgbouncer_stats_total_xact_time Total number of microseconds spent by pgbouncer when connected to PostgreSQL in a transaction, either idle in transaction or executing queries.
# TYPE cnpg_pgbouncer_stats_total_xact_time gauge
cnpg_pgbouncer_stats_total_xact_time{database="pgbouncer"} 0
```

Like for `Clusters`, a specific `Pooler` can be monitored using the
[Prometheus Operator's](https://github.com/prometheus-operator/prometheus-operator) resource
[PodMonitor](https://github.com/prometheus-operator/prometheus-operator/blob/v0.47.1/Documentation/api.md#podmonitor).
A `PodMonitor` correctly pointing to a `Pooler` can be automatically created by the operator by setting
`.spec.monitoring.enablePodMonitor` to `true` in the `Pooler` resource itself (default: false).

!!! Important
    Any change to the `PodMonitor` created automatically will be overridden by the Operator at the next reconciliation
    cycle, in case you need to customize it, you can do so as described below.

To deploy a `PodMonitor` for a specific Pooler manually, you can just define it as follows, changing it as needed:
```yaml
apiVersion: monitoring.coreos.com/v1
kind: PodMonitor
metadata:
  name: <POOLER_NAME>
spec:
  selector:
    matchLabels:
      cnpg.io/poolerName: <POOLER_NAME>
  podMetricsEndpoints:
  - port: metrics
```

## Logging

Logs are directly sent to standard output, in JSON format, like in the
following example:

```json
{
  "level": "info",
  "ts": SECONDS.MICROSECONDS,
  "msg": "record",
  "pipe": "stderr",
  "record": {
    "timestamp": "YYYY-MM-DD HH:MM:SS.MS UTC",
    "pid": "<PID>",
    "level": "LOG",
    "msg": "kernel file descriptor limit: 1048576 (hard: 1048576); max_client_conn: 100, max expected fd use: 112"
  }
}
```

## Pausing connections

The `Pooler` specification allows you to take advantage of PgBouncer's `PAUSE`
and `RESUME` commands, using only declarative configuration - via the `paused`
option, by default set to `false`. When set to `true`, the operator internally
invokes the `PAUSE` command in PgBouncer, which:

1. closes all active connections towards the PostgreSQL server, after waiting for the queries to complete
2. pauses any new connection coming from the client

When the `paused` option is set back to `false`, the operator will invoke the
`RESUME` command in PgBouncer, re-opening the taps towards the PostgreSQL
service defined in the `Pooler`.

!!! Seealso "PAUSE"
    For further information, please refer to the
    [`PAUSE` section in the PgBouncer documentation](https://www.pgbouncer.org/usage.html#pause-db).

!!! Important
    In future versions, the switchover operation will be fully integrated
    with the PgBouncer pooler, and take advantage of the `PAUSE`/`RESUME`
    features to reduce the perceived downtime by client applications.
    At the moment, you can achieve the same results by setting the `paused`
    attribute to `true`, then issuing the switchover command through the
    [`cnpg` plugin](cnpg-plugin.md#promote), and finally restoring the `paused`
    attribute to `false`.

## Limitations

### Single PostgreSQL cluster

The current implementation of the pooler is designed to work as part of a
specific CloudNativePG cluster (a service, to be precise). It is not
possible at the moment to create a pooler that spans over multiple clusters.

### Controlled configurability

CloudNativePG transparently manages several configuration options
that are used for the PgBouncer layer to communicate with PostgreSQL. Such
options are not configurable from outside and include TLS certificates,
authentication settings, `databases` section, and `users` section. Also,
considering the specific use case for the single PostgreSQL cluster, the
adopted criteria is to explicitly list the options that can be configured by
users.

!!! Note
    We have reasons to believe that the adopted solution addresses the majority of
    use cases, while leaving room for the future implementation of a separate
    operator for PgBouncer to complete the gamma with more advanced and customized
    scenarios.

