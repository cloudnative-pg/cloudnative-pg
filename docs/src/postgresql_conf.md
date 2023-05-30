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

These settings are the same across all instances.

!!! Warning
    Please don't use the `ALTER SYSTEM` query to change the configuration of
    the PostgreSQL instances in an imperative way. Changing some of the options
    that are normally controlled by the operator might indeed lead to an
    unpredictable/unrecoverable state of the cluster.
    Moreover, `ALTER SYSTEM` changes are not replicated across the cluster.

A reference for custom settings usage is included in the samples, see
[`cluster-example-custom.yaml`](samples/cluster-example-custom.yaml).

## The `postgresql` section

The PostgreSQL instance in the pod starts with a default `postgresql.conf` file,
to which these settings are automatically added:

```text
listen_addresses = '*'
include custom.conf
```

The `custom.conf` file will contain the user-defined settings in the
`postgresql` section, as in the following example:

```yaml
  # ...
  postgresql:
    parameters:
      shared_buffers: "1GB"
  # ...
```

!!! Seealso "PostgreSQL GUCs: Grand Unified Configuration"
    Refer to the PostgreSQL documentation for
    [more information on the available parameters](https://www.postgresql.org/docs/current/runtime-config.html),
    also known as GUC (Grand Unified Configuration).

The content of `custom.conf` is automatically generated and maintained by the
operator by applying the following sections in this order:

- Global default parameters
- Default parameters that depend on the PostgreSQL major version
- User-provided parameters
- Fixed parameters

The **global default parameters** are:

```text
dynamic_shared_memory_type = 'posix'
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
shared_memory_type = 'mmap' # for PostgreSQL >= 12 only
wal_keep_size = '512MB' # for PostgreSQL >= 13 only
wal_keep_segments = '32' # for PostgreSQL <= 12 only
wal_sender_timeout = '5s'
wal_receiver_timeout = '5s'
```

!!! Warning
    It is your duty to plan for WAL segments retention in your PostgreSQL
    cluster and properly configure either `wal_keep_size` or `wal_keep_segments`,
    depending on the server version, based on the expected and observed workloads.

    Alternatively, if the only streaming replication clients are the replica instances
    running in the High Availability cluster, you can take advantage of the
    replication slots feature, which adds support for replication slots at the
    cluster level. You can enable the feature with the
    `replicationSlots.highAvailability` option (for more information, please refer to the
    ["Replication" section](replication.md#replication-slots-for-high-availability).)

    Without replication slots nor continuous backups in place, configuring
    `wal_keep_size` or `wal_keep_segments` is the only way to
    protect standbys from falling out of sync.
    If a standby did fall out of sync it would produce error
    messages like:
    `"could not receive data from WAL stream: ERROR: requested WAL segment ************************ has already been removed"`.
    This will require you to dedicate a part of your `PGDATA`, or the volume
    dedicated to storing WAL files, to keep older WAL segments for streaming
    replication purposes.

The following parameters are **fixed** and exclusively controlled by the operator:

```text
archive_command = '/controller/manager wal-archive %p'
archive_mode = 'on'
full_page_writes = 'on'
hot_standby = 'true'
listen_addresses = '*'
port = '5432'
restart_after_crash = 'false'
ssl = 'on'
ssl_ca_file = '/controller/certificates/client-ca.crt'
ssl_cert_file = '/controller/certificates/server.crt'
ssl_key_file = '/controller/certificates/server.key'
unix_socket_directories = '/controller/run'
wal_level = 'logical'
wal_log_hints = 'on'
```

Since the fixed parameters are added at the end, they can't be overridden by the
user via the YAML configuration. Those parameters are required for correct WAL
archiving and replication.

### Replication settings

The `primary_conninfo`, `restore_command`,  and `recovery_target_timeline`
parameters are managed automatically by the operator according to the state of
the instance in the cluster.

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

In CloudNativePG the `shared_preload_libraries` option is empty by
default. Although you can override the content of `shared_preload_libraries`,
we recommend that only expert Postgres users take advantage of this option.

!!! Important
    In case a specified library is not found, the server fails to start,
    preventing CloudNativePG from any self-healing attempt and requiring
    manual intervention. Please make sure you always test both the extensions and
    the settings of `shared_preload_libraries` if you plan to directly manage its
    content.

CloudNativePG is able to automatically manage the content of the
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

As anticipated in the previous section, CloudNativePG automatically
manages the content in `shared_preload_libraries` for some well-known and
supported extensions. The current list includes:

- `auto_explain`
- `pg_stat_statements`
- `pgaudit`
- `pg_failover_slots`

Some of these libraries also require additional objects in a database before
using them, normally views and/or functions managed via the `CREATE EXTENSION`
command to be run in a database (the `DROP EXTENSION` command typically removes
those objects).

For such libraries, CloudNativePG automatically handles the creation
and removal of the extension in all databases that accept a connection in the
cluster, identified by the following query:

```sql
SELECT datname FROM pg_database WHERE datallowconn
```

!!! Note
    The above query also includes template databases like `template1`.

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
      auto_explain.log_min_duration: "10s"
  # ...
```

!!! Note
    Enabling auto_explain can lead to performance issues. Please refer to [`the auto explain documentation`](https://www.postgresql.org/docs/current/auto-explain.html)


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
      pg_stat_statements.max: "10000"
      pg_stat_statements.track: all
  # ...
```

As explained previously, the operator will automatically add
`pg_stat_statements` to `shared_preload_libraries` and run `CREATE EXTENSION IF
NOT EXISTS pg_stat_statements` on each database, enabling you to run queries
against the `pg_stat_statements` view.

#### Enabling `pgaudit`

The `pgaudit` extension provides detailed session and/or object audit logging via the standard PostgreSQL logging facility.

CloudNativePG has transparent and native support for
[PGAudit](https://www.pgaudit.org/) on PostgreSQL clusters. For further information, please refer to the ["PGAudit" logs section.](logging.md#pgaudit-logs)

You can enable `pgaudit` by adding to the configuration a parameter
that starts with `pgaudit.` as in the following example excerpt:

```yaml
#
postgresql:
  parameters:
    pgaudit.log: "all, -misc"
    pgaudit.log_catalog: "off"
    pgaudit.log_parameter: "on"
    pgaudit.log_relation: "on"
#
```

#### Enabling `pg_failover_slots`

The [`pg_failover_slots`](https://github.com/EnterpriseDB/pg_failover_slots)
extension by EDB ensures that logical replication slots can survive a
failover scenario. Failovers are normally implemented using physical
streaming replication, like in the case of CloudNativePG.

You can enable `pg_failover_slots` by adding to the configuration a parameter
that starts with `pg_failover_slots.`: as explained above, the operator will
transparently manage the `pg_failover_slots` entry in the
`shared_preload_libraries` option depending on this.

Please refer to [`the `pg_failover_slots` documentation`](https://www.enterprisedb.com/docs/pg_extensions/pg_failover_slots)
for details on this extension.

Additionally, for each database that you intend to you use with `pg_failover_slots`
you need to add an entry in the `pg_hba` section that enables each replica to
connect to the primary.
For example, suppose that you want to use the `app` database with `pg_failover_slots`,
you need to add this entry in the `pg_hba` section:

``` yaml
  postgresql:
    pg_hba:
      - hostssl app streaming_replica all cert

## The `pg_hba` section

`pg_hba` is a list of PostgreSQL Host Based Authentication rules
used to create the `pg_hba.conf` used by the pods.

Since the first matching rule is used for authentication, the `pg_hba.conf` file
generated by the operator can be seen as composed of four sections:

1. Fixed rules
2. User-defined rules
3. Optional LDAP section
4. Default rules

Fixed rules:

```text
local all all peer

hostssl postgres streaming_replica all cert
hostssl replication streaming_replica all cert
```

Default rules:

```text
host all all all <default-authentication-method>
```

From PostgreSQL 14 the default value of the `password_encryption`
database parameter is set to `scram-sha-256`. Because of that,
the default authentication method is `scram-sha-256` from this
PostgreSQL version.

PostgreSQL 13 and older will use `md5` as the default authentication
method.

The resulting `pg_hba.conf` will look like this:

```text
local all all peer

hostssl postgres streaming_replica all cert
hostssl replication streaming_replica all cert

<user defined rules>
<user defined LDAP>

host all all all scram-sha-256 # (or md5 for PostgreSQL version <= 13)
```

Refer to the PostgreSQL documentation for [more information on `pg_hba.conf`](https://www.postgresql.org/docs/current/auth-pg-hba-conf.html).

Inside the cluster manifest, `pg_hba` lines are added as list items
in `spec.postgresql.pg_hba`, as in the following excerpt:

``` yaml
  postgresql:
    pg_hba:
      - hostssl app app 10.244.0.0/16 md5
```

In the above example we are enabling access for the `app` user to the `app`
database using MD5 password authentication (you can use `scram-sha-256`
if you prefer) via a secure channel (`hostssl`).

### LDAP Configuration

Under the `postgres` section of the cluster spec there is an optional `ldap` section available to define an LDAP
configuration to be converted into a rule added into the `pg_hba.conf` file.

This will support two modes: `simple bind` mode which requires specifying a `server`, `prefix` and `suffix` in the LDAP 
section and the `search+bind` mode which requires specifying `server`, `baseDN`, `binDN`, and a `bindPassword` which is
a secret containing the ldap password. Additionally, in `search+bind` mode you have the option to specify a
`searchFilter` or `searchAttribute`. If no `searchAttribute` is specified the default one of `uid` will be used.

Additionally, both modes allow the specification of a `scheme` for ldapscheme and a `port`. Neither scheme nor port are
required, however. 

This section filled out for search+bind could look as follows:

```yaml
postgresql:
  ldap:
    server: 'openldap.default.svc.cluster.local'
    bindSearchAuth:
      baseDN: 'ou=org,dc=example,dc=com'
      bindDN: 'cn=admin,dc=example,dc=com'
      bindPassword:
        name: 'ldapBindPassword'
        key: 'data'
      searchAttribute: 'uid'
```

## Changing configuration

You can apply configuration changes by editing the `postgresql` section of
the `Cluster` resource.

After the change, the cluster instances will immediately reload the
configuration to apply the changes.
If the change involves a parameter requiring a restart, the operator will
perform a rolling upgrade.

## Dynamic Shared Memory settings

PostgreSQL supports a few implementations for dynamic shared memory
management through the
[`dynamic_shared_memory_type`](https://www.postgresql.org/docs/current/runtime-config-resource.html#GUC-DYNAMIC-SHARED-MEMORY-TYPE)
configuration option. In CloudNativePG we recommend to limit ourselves to
any of the following two values:

- `posix`: which relies on POSIX shared memory allocated using `shm_open` (default setting)
- `sysv`: which is based on System V shared memory allocated via `shmget`

In PostgreSQL, this setting is particularly important for memory allocation in parallel queries.
For details, please refer to this
[thread from the `pgsql-general` mailing list](https://www.postgresql.org/message-id/CA%2BhUKGJOj7qzDLxeFPVvto8YEWop6FSQoTYPO9Z6Ee%3Di-nPS_Q%40mail.gmail.com).

### POSIX shared memory

The default setting of `posix` should be enough in most cases, considering that
the operator automatically mounts a *memory-bound `EmptyDir` volume* called `shm`
under `/dev/shm`. You can verify the size of such volume inside the running
Postgres container with:

```sh
mount | grep shm
```

You should get something similar to the following output:

```console
shm on /dev/shm type tmpfs (rw,nosuid,nodev,noexec,relatime,size=******)
```

### System V shared memory

In case your Kubernetes cluster has a high enough value for the `SHMMAX`
and `SHMALL` parameters, you can also set:

```yaml
dynamic_shared_memory_type: "sysv"
```

You can check the `SHMMAX`/`SHMALL` from inside a PostgreSQL container, by running:

```sh
ipcs -lm
```

For example:

```console
------ Shared Memory Limits --------
max number of segments = 4096
max seg size (kbytes) = 18014398509465599
max total shared memory (kbytes) = 18014398509481980
min seg size (bytes) = 1
```

As you can see, the very high number of `max total shared memory` recommends
setting `dynamic_shared_memory_type` to `sysv`.

An alternate method is to run:

```sh
cat /proc/sys/kernel/shmall
cat /proc/sys/kernel/shmmax
```

## Fixed parameters

Some PostgreSQL configuration parameters should be managed exclusively by the
operator. The operator prevents the user from setting them using a webhook.

Users are not allowed to set the following configuration parameters in the
`postgresql` section:

- `allow_system_table_mods`
- `archive_cleanup_command`
- `archive_command`
- `archive_mode`
- `bonjour`
- `bonjour_name`
- `cluster_name`
- `config_file`
- `data_directory`
- `data_sync_retry`
- `event_source`
- `external_pid_file`
- `full_page_writes`
- `hba_file`
- `hot_standby`
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

