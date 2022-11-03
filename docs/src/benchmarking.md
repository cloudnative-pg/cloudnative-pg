# Benchmarking

### pgbench

[pgbench](https://www.postgresql.org/docs/current/pgbench.html) is the default benchmarking application for PostgreSQL.
We are introducing kubectl plugin command `pgbench` to execute a user-defined pgbench job on an existing PostgreSQL
database cluster or user can just execute a `--dry-run` as well.

```
kubectl cnpg pgbench <cluster-name> --pgbench-job-name <pgbench-job> --db-name <db-name> -- --time 30 --client 1 --jobs 1
```
**Example** : Suppose you already have PostgreSQL database cluster setup named `cluster-example`, and you want to
benchmark this, run `pgbench` using kubectl plugin with following command:

```
kubectl cnpg pgbench cluster-example --pgbench-job-name pgbench-job -- --time 30 --client 1 --jobs 1 -n NAMESPACE
```

Get created job object details:
```
kubectl get job/pgbench-job -n NAMESPACE
```
```
NAME               COMPLETIONS   DURATION   AGE
pgbench-job-name   1/1           15s        41s
```

You can gather the results after the job is completed:

```
kubectl logs job/pgbench-job -n NAMESPACE
```

Below is an example of `pgbench` output of the above mention command with 30 sec benchmark,
one client, with one parallel job:

```
starting vacuum...end.
transaction type: builtin: TPC-B (sort of)>
scaling factor: 1
query mode: simple
number of clients: 1
number of threads: 1
duration: 30 s
number of transactions actually processed: 17879
latency average = 1.678 ms
initial connection time = 9.570 ms
tps = 596.103413 (without initial connection time)
```

If you want to execute a `pgbench` job on user-defined database, then first we need to create a database and provide
`--db-name` while running `pgbench` command.

You can refer below command to run `pgbench` job on cluster `cluster-example` with database named `pgbench`.
```
kubectl cnpg pgbench cluster-example --db-name pgbench --pgbench-job-name pgbench-job -- --time 30 --client 1 --jobs 1 -n NAMESPACE
```
