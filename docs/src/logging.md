# Logging

The operator is designed to log in JSON format directly to standard
output, including PostgreSQL logs.

Each log entry has the following fields:

- `level`: log level (`info`, `notice`, ...)
- `ts`: the timestamp (epoch with microseconds)
- `logger`: the type of the record (e.g. `postgres` or `pg_controldata`)
- `msg`: the actual message or the keyword `record` in case the message is
  parsed in JSON format
- `record`: the actual record (with structure that varies depending on the
  `logger` type)

## PostgreSQL log

Each entry in the PostgreSQL log is a JSON object having the `logger` key set to `postgres` and the structure described in the following example:

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
  }
}
```

Internally, the operator relies on the PostgreSQL CSV log format.
Please refer to the PostgreSQL documentation for more information
about the [CSV log format](https://www.postgresql.org/docs/current/runtime-config-logging.html).

## Other logs

All logs that are produced by the operator and its instances are in JSON format, with `logger` set accordingly to the process
that produced them. Therefore, all the possible `logger` values are the following ones:

- `barman-cloud-wal-archive`
- `barman-cloud-wal-restore`
- `initdb`
- `pg_basebackup`
- `pg_controldata`
- `pg_ctl`
- `pg_rewind`
- `postgres`

Except for `postgres` that has the aforementioned structures, all other possible values
just have `msg` set to the escaped message that is logged.
