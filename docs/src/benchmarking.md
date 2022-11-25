# Benchmarking

### pgbench

The kubectl plugin command `pgbench` executes a user-defined pgbench job on an existing Postgres Cluster.
The command also accepts the `--dry-run` command, this will output the job manifest without applying it.

Example usage:
```
kubectl cnpg pgbench <cluster-name> --pgbench-job-name <pgbench-job> --db-name <db-name> -n <NAMESPACE> -- --time 30 --client 1 --jobs 1
```

Example of how to run it against a `Cluster` named `cluster-example` in the `pgbench` namespace:
kubectl cnpg pgbench \
   -n pgbench cluster-example \
   --pgbench-job-name pgbench-job \
   -- --time 30 --client 1 --jobs 1
```

Example of how to run it on an existing database by using the `--db-name` flag and
the `pgbench` namespace:
```
kubectl cnpg pgbench \
  -n pgbench cluster-example \
  --db-name pgbench \
  --pgbench-job-name pgbench-job \
  -- --time 30 --client 1 --jobs 1
```

The job status can be fetched by running:
```
kubectl get job/pgbench-job -n <namespace>
```
```
NAME               COMPLETIONS   DURATION   AGE
pgbench-job-name   1/1           15s        41s
```

Once the job is completed the results can be gathered by executing:

```
kubectl logs job/pgbench-job -n <namespace>
```
