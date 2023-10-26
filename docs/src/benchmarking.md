# Benchmarking

The CNPG kubectl plugin provides an easy way to benchmark a PostgreSQL deployment in Kubernetes using CloudNativePG.

Benchmarking focuses on two aspects:

- The **database**, by relying on [pgbench](https://www.postgresql.org/docs/current/pgbench.html)
- The **storage**, by relying on [fio](https://fio.readthedocs.io/en/latest/fio_doc.html)

!!! IMPORTANT
    Run `pgbench` and `fio` in a staging or preproduction environment.
    Don't use these plugins in a production environment, as it might have
    catastrophic consequences on your databases and the other
    workloads/applications that run in the same shared environment.

### pgbench

The `kubectl` CNPG plugin command `pgbench` executes a user-defined `pgbench` job
against an existing Postgres cluster.

Using the `--dry-run` flag, you can generate the manifest of the job for later
modification or execution.

A common command structure with `pgbench` is:

```shell
kubectl cnpg pgbench \
  -n <namespace> <cluster-name> \
  --job-name <pgbench-job> \
  --db-name <db-name> \
  -- <pgbench options>
```

!!! IMPORTANT
    See the [`pgbench` documentation](https://www.postgresql.org/docs/current/pgbench.html)
    for information about the specific options to use in your jobs.

This example creates a job called `pgbench-init`. It initializes the `app` database in a cluster named `cluster-example` for `pgbench`
OLTP-like purposes. Th scale factor is 1000.

```shell
kubectl cnpg pgbench \
  --job-name pgbench-init \
  cluster-example \
  -- --initialize --scale 1000
```

!!! Note
    This example generates a database with 100000000 records, which uses approximately 13GB
    of space on disk.

To see the progress of the job:

```shell
kubectl logs jobs/pgbench-run
```

This example creates a job called `pgbench-run` that executes `pgbench`
against the previously initialized database for 30 seconds. It uses a single
connection.

```shell
kubectl cnpg pgbench \
  --job-name pgbench-run \
  cluster-example \
  -- --time 30 --client 1 --jobs 1
```

This example runs `pgbench` against an existing database by using the
`--db-name` flag and the `pgbench` namespace:

```shell
kubectl cnpg pgbench \
  --db-name pgbench \
  --job-name pgbench-job \
  cluster-example \
  -- --time 30 --client 1 --jobs 1
```

If you want to run a `pgbench` job on a specific worker node, you can use
the `--node-selector` option. Suppose you want to run the previous
initialization job on a node having the `workload=pgbench` label. You can run:

```shell
kubectl cnpg pgbench \
  --db-name pgbench \
  --job-name pgbench-init \
  --node-selector workload=pgbench \
  cluster-example \
  -- --initialize --scale 1000
```

You can fetch the job status by running:

```
kubectl get job/pgbench-job -n <namespace>

NAME       COMPLETIONS   DURATION   AGE
job-name   1/1           15s        41s
```

After the job is complete, you can gather the results:

```
kubectl logs job/pgbench-job -n <namespace>
```

### fio

The kubectl CNPG plugin command `fio` executes a fio job with default values
and read operations.
Using the `--dry-run` flag, you can generate the manifest of the job for later
modification or execution.

!!! Note
    The kubectl plugin command `fio` creates a deployment with predefined
    fio job values using a ConfigMap. If you want to provide custom job values, we
    recommend generating a manifest using the `--dry-run` flag and providing your
    custom job values in the generated ConfigMap.

This example shows the default usage:

```shell
kubectl cnpg fio <fio-name>
```

This example uses custom values:

```shell
kubectl cnpg fio <fio-name> \
  -n <namespace>  \
  --storageClass <name> \
  --pvcSize <size>
```

This example runs the `fio` command against a `StorageClass` named
`standard` and `pvcSize: 2Gi` in the `fio` namespace:

```shell
kubectl cnpg fio fio-job \
  -n fio  \
  --storageClass standard \
  --pvcSize 2Gi
```

To fetch the deployment status:

```shell
kubectl get deployment/fio-job -n fio

NAME          READY   UP-TO-DATE   AVAILABLE   AGE
fio-job        1/1     1            1           14s

```

After running the kubectl plugin command `fio`, it:

1. Creates a PVC.
1. Creates a ConfigMap representing the configuration of a fio job.
1. Creates a fio deployment composed of a single pod. The pod runs fio on
   the PVC, creates graphs after completing the benchmark, and starts serving the
   generated files with a web server. We use the
   [`fio-tools`](https://github.com/wallnerryan/fio-tools`) image for that.

The pod created by the deployment is ready when it starts serving the
results. You can forward the port of the pod created by the deployment:

```
kubectl port-forward -n <namespace> deployment/<fio-name> 8000
```

You can then use a browser and connect to [http://localhost:8000/](http://localhost:8000/) to get the data.

The default 8k block size was chosen to emulate a PostgreSQL workload.
Disks that cap the amount of available IOPS can show very different throughput
values when you change this parameter.

The diagram shows an example of sequential writes on a local disk
mounted on a dedicated Kubernetes node
(1-hour benchmark):

![Sequential writes bandwidth](images/write_bw.1-2Draw.png)

After all testing is done, you can delete fio deployment and resources:

```shell
kubectl cnpg fio <fio-job-name> --dry-run | kubectl delete -f -
```

Make sure to use the same name that was used to create the fio deployment and add a namespace, if applicable.
