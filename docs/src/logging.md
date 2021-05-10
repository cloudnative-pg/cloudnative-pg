# Logging

The operator is designed to log in JSON format directly to standard
output, including PostgreSQL logs.

Each log entry has the following fields:

- `level`: log level (`info`, `notice`, ...)
- `ts`: the timestamp (epoch with microseconds)
- `msg`: the type of the record (e.g. `postgres` or `pg_controldata`)
- `record`: the actual record (with structure that varies depending on the
  `msg` type)

## PostgreSQL log

Each entry in the PostgreSQL log is a JSON object having the `msg` key set to `postgres` and the structure described in the following example:

```json
{
  "level": "info",
  "ts": 1619713056.1872551,
  "msg": "postgres",
  "record": {
    "log_time": "2021-04-29 16:17:36.187 UTC",
    "user_name": "",
    "database_name": "",
    "process_id": "24",
    "connection_from": "",
    "session_id": "608adc1f.18",
    "session_line_num": "2",
    "command_tag": "",
    "session_start_time": "2021-04-29 16:17:35 UTC",
    "virtual_transaction_id": "",
    "transaction_id": "0",
    "error_severity": "LOG",
    "sql_state_code": "00000",
    "message": "entering standby mode",
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
on the [CSV log format](https://www.postgresql.org/docs/current/runtime-config-logging.html).

