# Logging

The operator is designed to log in JSON format directly to standard output,
including PostgreSQL logs.

Each log entry has the following fields:

- `level`: log level (`info`, `notice`, ...)
- `ts`: the timestamp (epoch with microseconds)
- `logger`: the type of the record (e.g. `postgres` or `pg_controldata`)
- `msg`: the actual message or the keyword `record` in case the message is parsed in JSON format
- `record`: the actual record (with structure that varies depending on the
  `logger` type)
- `logging_podName`: the pod where the log was created generated

## Operator log

A log level can be specified in the cluster spec with the option `logLevel` and
can be set to any of `error`, `warning`, `info`(default), `debug` or `trace`.

At the moment, the log level can only be set when an instance starts and can not be
changed at runtime. If the value is changed in the cluster spec after the cluster
was started, this will take effect only in the new pods and not the old ones.

## PostgreSQL log

Each entry in the PostgreSQL log is a JSON object having the `logger` key set
to `postgres` and the structure described in the following example:

```json
{
  "level": "info",
  "ts": 1619781249.7188137,
  "logger": "postgres",
  "msg": "record",
  "record": {
    "log_time": "2021-04-30 11:14:09.718 UTC",
    "user_name": "",
    "database_name": "",
    "process_id": "25",
    "connection_from": "",
    "session_id": "608be681.19",
    "session_line_num": "1",
    "command_tag": "",
    "session_start_time": "2021-04-30 11:14:09 UTC",
    "virtual_transaction_id": "",
    "transaction_id": "0",
    "error_severity": "LOG",
    "sql_state_code": "00000",
    "message": "database system was interrupted; last known up at 2021-04-30 11:14:07 UTC",
    "detail": "",
    "hint": "",
    "internal_query": "",
    "internal_query_pos": "",
    "context": "",
    "query": "",
    "query_pos": "",
    "location": "",
    "application_name": "",
    "backend_type": "startup"
  },
  "logging_pod": "cluster-example-1",
}
```

Internally, the operator relies on the PostgreSQL CSV log format. Please refer
to the PostgreSQL documentation for more information about the [CSV log
format](https://www.postgresql.org/docs/current/runtime-config-logging.html).

## PGAudit logs

CloudNativePG has transparent and native support for
[PGAudit](https://www.pgaudit.org/) on PostgreSQL clusters.

All you need to do is add the required `pgaudit` parameters to the `postgresql`
section in the configuration of the cluster.

!!! Important
    It is unnecessary to add the PGAudit library to `shared_preload_libraries`.
    The library will be added automatically by CloudNativePG based on the
    presence of `pgaudit.*` parameters in the postgresql configuration.
    The operator will detect and manage the addition and removal of the
    library from `shared_preload_libraries`.

The operator also takes care of creating and removing the extension from all
the available databases in the cluster.

!!! Important
    CloudNativePG runs the `CREATE EXTENSION` and
    `DROP EXTENSION` command in all databases in the cluster that accept
    connections.

Here is an example of a PostgreSQL 13 `Cluster` deployment which will result in
`pgaudit` being enabled with the requested configuration:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-example
spec:
  instances: 3
  imageName: quay.io/enterprisedb/postgresql:13

  postgresql:
    parameters:
      "pgaudit.log": "all, -misc"
      "pgaudit.log_catalog": "off"
      "pgaudit.log_parameter": "on"
      "pgaudit.log_relation": "on"

  storage:
    size: 1Gi
```

The audit CSV logs entries returned by PGAudit are then parsed and routed to
stdout in JSON format, similarly to all the remaining logs:

- `.logger` is set to `pgaudit`
- `.msg` is set to `record`
- `.record` contains the whole parsed record as a JSON object, similar to
  `logging_collector` logs - except for `.record.audit`, which contains the
  PGAudit CSV message formatted as a JSON object

See the example below:

```json
{
  "level": "info",
  "ts": 1627394507.8814096,
  "logger": "pgaudit",
  "msg": "record",
  "record": {
    "log_time": "2021-07-27 14:01:47.881 UTC",
    "user_name": "postgres",
    "database_name": "postgres",
    "process_id": "203",
    "connection_from": "[local]",
    "session_id": "610011cb.cb",
    "session_line_num": "1",
    "command_tag": "SELECT",
    "session_start_time": "2021-07-27 14:01:47 UTC",
    "virtual_transaction_id": "3/336",
    "transaction_id": "0",
    "error_severity": "LOG",
    "sql_state_code": "00000",
    "backend_type": "client backend",
    "audit": {
      "audit_type": "SESSION",
      "statement_id": "1",
      "substatement_id": "1",
      "class": "READ",
      "command": "SELECT FOR KEY SHARE",
      "statement": "SELECT pg_current_wal_lsn()",
      "parameter": "<none>"
    }
  },
  "logging_pod": "cluster-example-1",
}
```

Please refer to the
[PGAudit documentation](https://github.com/pgaudit/pgaudit/blob/master/README.md#format) <!-- wokeignore:rule=master -->
for more details about each field in a record.

## Other logs

All logs that are produced by the operator and its instances are in JSON
format, with `logger` set accordingly to the process that produced them.
Therefore, all the possible `logger` values are the following ones:

- `barman-cloud-wal-archive`: from `barman-cloud-wal-archive` directly
- `barman-cloud-wal-restore`: from `barman-cloud-wal-restore` directly
- `initdb`: from running `initdb`
- `pg_basebackup`: from running `pg_basebackup`
- `pg_controldata`: from running `pg_controldata`
- `pg_ctl`: from running any `pg_ctl` subcommand
- `pg_rewind`: from running `pg_rewind`
- `pgaudit`: from PGAudit extension
- `postgres`: from the `postgres` instance (having `msg` different than `record`)
- `wal-archive`: from the `wal-archive` subcommand of the instance manager
- `wal-restore`: from the `wal-restore` subcommand of the instance manager

Except for `postgres` that has the aforementioned structures,
all other possible values just have `msg` set to the escaped message that is
logged.
