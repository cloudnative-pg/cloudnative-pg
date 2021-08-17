# PostgreSQL Configuration

Users that are familiar with PostgreSQL are aware of the existence of the following two files
to configure an instance:

- `postgresql.conf`: main run-time configuration file of PostgreSQL
- `pg_hba.conf`: clients authentication file

Due to the concepts of declarative configuration and immutability of the PostgreSQL
containers, users are not allowed to directly touch those files. Configuration
is possible through the `postgresql` section of the `Cluster` resource definition
by defining custom `postgresql.conf` and `pg_hba.conf` settings via the
`parameters` and the `pg_hba` keys.
A reference for custom settings usage is included in the samples, see
[`cluster-example-custom.yaml`](samples/cluster-example-custom.yaml).

These settings are the same across all instances.

!!! Warning
    **OpenShift users:** due to a current limitation of the OpenShift user interface,
    it is possible to change PostgreSQL settings from the YAML pane only.

## The `postgresql` section

The PostgreSQL instance in the pod starts with a default `postgresql.conf` file,
to which these settings are automatically added:

```text
listen_addresses = '*'
include custom.conf
```

The `custom.conf` file will contain the user-defined settings. Refer to the
PostgreSQL documentation for [more information on the available parameters](https://www.postgresql.org/docs/current/runtime-config.html).
The content of `custom.conf` is automatically generated and maintained by the
operator by applying the following sections in this order:

- Global default parameters
- Default parameters that depend on the PostgreSQL major version
- User-provided parameters
- Fixed parameters

The **global default parameters** are:

```text
logging_collector = 'on'
log_destination = 'csvlog'
log_directory = '/controller/log'
log_filename = 'postgres'
log_rotation_age = '0'
log_rotation_size = '0'
log_truncate_on_rotation = 'false'
max_parallel_workers = '32'
max_replication_slots = '32'
max_worker_processes = '32'
```

The **default parameters for PostgreSQL 13 or higher** are:

```text
wal_keep_size = '512MB'
```

The **default parameters for PostgreSQL 10 to 12** are:

```text
wal_keep_segments = '32'
```

!!! Warning
    It is your duty to plan for WAL segments retention in your PostgreSQL
    cluster and properly configure either `wal_keep_segments` or `wal_keep_size`,
    depending on the server version, based on the expected and observed workloads.
    Until Cloud Native PostgreSQL supports replication slots, and if you don't have
    continuous backup in place, this is the only way at the moment that protects
    from the case of a standby falling out of sync and returning error messages like:
    `"could not receive data from WAL stream: ERROR: requested WAL segment ************************ has already been removed"`.
    This will require you to dedicate a part of your `PGDATA` to keep older
    WAL segments for streaming replication purposes.

The following parameters are **fixed** and exclusively controlled by the operator:

```text
archive_command = '/controller/manager wal-archive %p'
archive_mode = 'on'
archive_timeout = '5min'
full_page_writes = 'on'
hot_standby = 'true'
listen_addresses = '*'
port = '5432'
ssl = 'on'
ssl_ca_file = '/controller/certificates/client-ca.crt'
ssl_cert_file = '/controller/certificates/server.crt'
ssl_key_file = '/controller/certificates/server.key'
unix_socket_directories = '/var/run/postgresql'
wal_level = 'logical'
wal_log_hints = 'on'
```

Since the fixed parameters are added at the end, they can't be overridden by the
user via the YAML configuration. Those parameters are required for correct WAL
archiving and replication.

### Replication settings

The `primary_conninfo` and `recovery_target_timeline` parameters are managed
automatically by the operator according to the state of the instance in
the cluster.

```text
primary_conninfo = 'host=cluster-example-rw user=postgres dbname=postgres'
recovery_target_timeline = 'latest'
```

### Log control settings

The operator requires PostgreSQL to output its log in CSV format, and the
instance manager automatically parses it and outputs it in JSON format.
For this reason, all log settings in PostgreSQL are fixed and cannot be
changed.

For further information, please refer to the ["Logging" section](logging.md).

### Shared Preload Libraries

The `shared_preload_libraries` option in PostgreSQL exists to specify one or
more shared libraries to be pre-loaded at server start, in the form of a
comma-separated list. Typically, it is used in PostgreSQL to load those
extensions that need to be available to most database sessions in the whole system
(e.g. `pg_stat_statements`).

In Cloud Native PostgreSQL the `shared_preload_libraries` option is empty by
default. Although you can override the content of `shared_preload_libraries`,
we recommend that only expert Postgres users take advantage of this option.

!!! Important
    In case a specified library is not found, the server fails to start,
    preventing Cloud Native PostgreSQL from any self-healing attempt and requiring
    manual intervention. Please make sure you always test both the extensions and
    the settings of `shared_preload_libraries` if you plan to directly manage its
    content.

Cloud Native PostgreSQL is able to automatically manage the content of the
`shared_preload_libraries` option for some of the most used PostgreSQL
extensions (see the ["Managed extensions"](#managed-extensions) section below
for details).

Specifically, as soon as the operator notices that a configuration parameter
requires one of the managed libraries, it will automatically add the needed
library. The operator will also remove the library as soon as no actual parameter
requires it.

!!! Important
    Please always keep in mind that removing libraries from
    `shared_preload_libraries` requires a restart of all instances in the cluster
    in order to be effective.

You can provide additional `shared_preload_libraries` via
`.spec.postgresql.shared_preload_libraries` as a list of strings: the operator
will merge them with the ones that it automatically manages.

### Managed extensions

As anticipated in the previous section, Cloud Native PostgreSQL automatically
manages the content in `shared_preload_libraries` for some well-known and
supported extensions. The current list includes:

- `pg_stat_statements`
- `pgaudit`

Some of these libraries also require additional objects in a database before
using them, normally views and/or functions managed via the `CREATE EXTENSION`
command to be run in a database (the `DROP EXTENSION` command typically removes
those objects).

For such libraries, Cloud Native PostgreSQL automatically handles the creation
and removal of the extension in all databases that accept a connection in the
cluster, identified by the following query:

```sql
SELECT datname FROM pg_database WHERE datallowconn
```

!!! Note
    The above query also includes template databases like `template1`.

#### Enabling `pg_stat_statements`

The [`pg_stat_statements`](https://www.postgresql.org/docs/current/pgstatstatements.html)
extension is one of the most important capabilities available in PostgreSQL for
real-time monitoring of queries.

You can enable `pg_stat_statements` by adding to the configuration a parameter
that starts with `pg_stat_statements.` as in the following example excerpt:

```yaml
  # ...
  postgresql:
    parameters:
      pg_stat_statements.max: 10000
      pg_stat_statements.track: all
  # ...
```

As explained previously, the operator will automatically add
`pg_stat_statements` to `shared_preload_libraries` and run `CREATE EXTENSION IF
NOT EXISTS pg_stat_statements` on each database, enabling you to run queries
against the `pg_stat_statements` view.

#### Enabling `auto_explain`

The [`auto_explain`](https://www.postgresql.org/docs/current/auto-explain.html)
extension provides a means for logging execution plans of slow statements
automatically, without having to manually run `EXPLAIN` (helpful for tracking
down un-optimized queries).

You can enable `auto_explain` by adding to the configuration a parameter
that starts with `auto_explain.` as in the following example excerpt (which
automatically logs execution plans of queries that take longer than 10 seconds
to complete):

```yaml
  # ...
  postgresql:
    parameters:
      auto_explain.log_min_duration: '10s'
  # ...
```

## The `pg_hba` section

`pg_hba` is a list of PostgreSQL Host Based Authentication rules
used to create the `pg_hba.conf` used by the pods.

Since the first matching rule is used for authentication, the `pg_hba.conf` file
generated by the operator can be seen as composed of three sections:

1. Fixed rules
2. User-defined rules
3. Default rules

Fixed rules:

```text
local all all peer

hostssl postgres streaming_replica all cert
hostssl replication streaming_replica all cert
```

Default rules:

```text
host all all all md5
```

The resulting `pg_hba.conf` will look like this:

```text
local all all peer

hostssl postgres streaming_replica all cert
hostssl replication streaming_replica all cert

<user defined rules>

host all all all md5
```

Refer to the PostgreSQL documentation for [more information on `pg_hba.conf`](https://www.postgresql.org/docs/current/auth-pg-hba-conf.html).

## Changing configuration

You can apply configuration changes by editing the `postgresql` section of
the `Cluster` resource.

After the change, the cluster instances will immediately reload the
configuration to apply the changes.
If the change involves a parameter requiring a restart, the operator will
perform a rolling upgrade.

## Fixed parameters

Some PostgreSQL configuration parameters should be managed exclusively by the
operator. The operator prevents the user from setting them using a webhook.

Users are not allowed to set the following configuration parameters in the
`postgresql` section:

- `allow_system_table_mods`
- `archive_cleanup_command`
- `archive_command`
- `archive_mode`
- `archive_timeout`
- `bonjour`
- `bonjour_name`
- `cluster_name`
- `config_file`
- `data_directory`
- `data_sync_retry`
- `dynamic_shared_memory_type`
- `event_source`
- `external_pid_file`
- `full_page_writes`
- `hba_file`
- `hot_standby`
- `huge_pages`
- `ident_file`
- `jit_provider`
- `listen_addresses`
- `log_destination`
- `log_directory`
- `log_file_mode`
- `log_filename`
- `log_rotation_age`
- `log_rotation_size`
- `log_truncate_on_rotation`
- `logging_collector`
- `port`
- `primary_conninfo`
- `primary_slot_name`
- `promote_trigger_file`
- `recovery_end_command`
- `recovery_min_apply_delay`
- `recovery_target`
- `recovery_target_action`
- `recovery_target_inclusive`
- `recovery_target_lsn`
- `recovery_target_name`
- `recovery_target_time`
- `recovery_target_timeline`
- `recovery_target_xid`
- `restart_after_crash`
- `restore_command`
- `shared_memory_type`
- `shared_preload_libraries`
- `ssl`
- `ssl_ca_file`
- `ssl_cert_file`
- `ssl_ciphers`
- `ssl_crl_file`
- `ssl_dh_params_file`
- `ssl_ecdh_curve`
- `ssl_key_file`
- `ssl_max_protocol_version`
- `ssl_min_protocol_version`
- `ssl_passphrase_command`
- `ssl_passphrase_command_supports_reload`
- `ssl_prefer_server_ciphers`
- `stats_temp_directory`
- `synchronous_standby_names`
- `syslog_facility`
- `syslog_ident`
- `syslog_sequence_numbers`
- `syslog_split_messages`
- `unix_socket_directories`
- `unix_socket_group`
- `unix_socket_permissions`
- `wal_level`
- `wal_log_hints`

