# Connection Pooling

Cloud Native PostgreSQL provides native support for connection pooling with
[PgBouncer](https://www.pgbouncer.org/), one of the most popular open source
connection poolers for PostgreSQL, through the `Pooler` CRD.

In a nutshell, a `Pooler` in Cloud Native PostgreSQL is a deployment of
PgBouncer pods that sits between your applications and a PostgreSQL service
(for example the `rw` service), creating a separate, scalable, configurable,
and highly available **database access layer**.

## Architecture

The following diagram highlights how the introduction of a database access
layer based on PgBouncer changes the architecture of Cloud Native PostgreSQL,
like an additional blade in a Swiss Army knife. Instead of directly connecting
to the PostgreSQL primary service, applications can now connect to the
equivalent service for PgBouncer, enabling reuse of existing connections for
faster performance and better resource management on the PostgreSQL side.

![Applications writing to the single primary via PgBouncer](./images/pgbouncer-architecture-rw.png)

## Quickstart

The easiest way to explain how Cloud Native PostgreSQL implements a PgBouncer
pooler is through an example:

```yaml
apiVersion: postgresql.k8s.enterprisedb.io/v1
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
      max_client_connections: "1000"
      default_pool_size: "10"
```

This creates a new `Pooler` resource called `pooler-example-rw` (the name is
arbitrary) that is strictly associated with the Postgres `Cluster` resource called
`cluster-example` and pointing to the primary, identified by the read/write
service (`rw`, therefore `cluster-example-rw`).

The `Pooler` must live in the same namespace of the Postgres cluster.
It consists of a Kubernetes deployment of 3 pods running the
[latest stable image of PgBouncer](https://quay.io/repository/enterprisedb/pgbouncer),
configured with the [`session` pooling mode](https://www.pgbouncer.org/config.html#pool-mode)
and accepting up to 1000 connections each - with a default pool size of 10
user/database pairs towards PostgreSQL.

!!! Important
    The `Pooler` only sets the `*` fallback database in PgBouncer, meaning
    that all parameters in the connection strings passed from the client are
    relayed to the PostgreSQL server (please refer to ["Section [databases]"
    in PgBouncer's documentation](https://www.pgbouncer.org/config.html#section-databases)).

Additionally, Cloud Native PostgreSQL automatically creates a secret with the
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

Any PgBouncer pooler is transparently integrated with Cloud Native PostgreSQL
support for in-transit encryption via **TLS connections**, both on the client
(application) and server (PostgreSQL) side of the pool.

Specifically, PgBouncer automatically reuses the certificates of the PostgreSQL
server. Moreover, it uses TLS client certificate authentication to connect
to the PostgreSQL server to run the `auth_query` for clients' password
authentication (see the ["Authentication" section](#authentication) below).

Containers run as the `pgbouncer` system user, and access to the `pgbouncer`
database is only allowed via local connections, through `peer` authentication.

## Authentication

**Password based authentication** is the only supported method for clients of
PgBouncer in Cloud Native PostgreSQL.

Internally, our implementation relies on PgBouncer's `auth_user` and `auth_query` options. Specifically, the operator:

- creates a standard user called `cnp_pooler_pgbouncer` in the PostgreSQL server
- creates the lookup function in the `postgres` database and grants execution
  privileges to the `cnp_pooler_pgbouncer` user (PoLA)
- issues a TLS certificate for this user
- sets `cnp_pooler_pgbouncer` as the `auth_user`
- configures PgBouncer to use the TLS certificate to authenticate
  `cnp_pooler_pgbouncer` against the PostgreSQL server
- removes all the above when it detects that a cluster does not have
  any pooler associated to it

## PodTemplates

You can take advantage of pod templates specification in the `template`
section of a `Pooler` resource. For details, please refer to [`PoolerSpec`
section](api_reference.md#PoolerSpec) in the API reference.

Through templates you can configure pods as you like, including fine
control over affinity and anti-affinity rules for pods and nodes.
By default, containers use images from `quay.io/enterprisedb/pgbouncer`.

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
- [`verbose`](https://www.pgbouncer.org/config.html#verbose)

## Monitoring

The PgBouncer implementation of the `Pooler` comes with a default
Prometheus exporter that automatically makes available several
metrics having the `cnp_pgbouncer_` prefix, by running:

- `SHOW LISTS` (prefix: `cnp_pgbouncer_lists`)
- `SHOW POOLS` (prefix: `cnp_pgbouncer_pools`)
- `SHOW STATS` (prefix: `cnp_pgbouncer_stats`)

Similarly to the Cloud Native PostgreSQL instance, the exporter runs on port
`9187` of each pod running PgBouncer, and also provides metrics related to the
Go runtime (with prefix `go_*`). You can debug the exporter on a pod running
PgBouncer through the following command:

```console
kubectl exec -ti <PGBOUNCER_POD> -- curl 127.0.0.1:9187/metrics
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

## Limitations

### Single PostgreSQL cluster

The current implementation of the pooler is designed to work as part of a
specific Cloud Native PostgreSQL cluster (a service, to be precise). It is not
possible at the moment to create a pooler that spans over multiple clusters.

### Controlled configurability

Cloud Native PostgreSQL transparently manages several configuration options
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

Currently, any configuration change requires a complete rollout of all PgBouncer pods.
