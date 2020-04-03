This section describes the structure of a *Kubernetes manifest* to be used
to instantiate a PostgreSQL cluster using the Cloud Native PostgreSQL Operator.

A PostgreSQL cluster can be defined using a Kubernetes manifest in *YAML* according to the structure declared by the `Cluster` Custom Resource Definition.

On the top level both individual parameters and parameter groups can be defined. Parameter names are written in camelCase.

## PostgreSQL Cluster metadata

As any other object in Kubernetes, a PostgreSQL cluster has a `metadata` section which allows user to specify the following properties:

- `namespace`: a DNS compatible label used to group objects
- `name`: a string that uniquely identifies this object within the current namespace in the Kubernetes cluster

## PostgreSQL Cluster parameters

A PostgreSQL cluster object can be defined through the following parameters available in the `spec` key of the manifest:

- `affinity`: affinity/anti-affinity rules for Pods
- `applicationConfiguration`: configuration of the PostgreSQL cluster (*required*)
- `description`: description of the PostgreSQL cluster
- `imageName`: name of the container image for PostgreSQL
- `imagePullSecretName`: secret for pulling the PostgreSQL image
- `instances`: number of instances required in the cluster, with `instances - 1` replicas (**required**)
- `postgresql`: configuration of the PostgreSQL server (*required*)
- `primaryUpdateStrategy`: strategy to update the primary as part of a rolling update: automated (`unsupervised`)
   or manually triggered (`supervised`)
- `resources`: resources requirements of every generated Pod
- `startDelay`: allowed time in seconds for a PostgreSQL instance to successfully start up (default 30)
- `stopDelay`: allowed time in seconds for a PostgreSQL instance to gracefully shut down (default 30)
- `storage`: configuration of the storage of PostgreSQL instances

## Application configuration

Application oriented information, such as database name, is delegated to the `applicationConfiguration` section of the manifest, with the following mandatory parameters, in alphabetical order:

- `database`: name of the PostgreSQL database in the cluster (e.g. `app`)
- `owner`: name of the owner of the PostgreSQL database

## PostgreSQL server configuration

Each PostgreSQL instance can be configured in the `postgresql` section of the manifest, through the following mandatory options:

- `parameters`: PostgreSQL configuration options to be added to the `postgresql.conf` file
- `pg_hba`: PostgreSQL Host Based Authentication rules, as an array of lines to be appended to the `pg_hba.conf` file

## Resources

Cloud Native PostgreSQL allows administrators to control and manage resource usage by the pods of the cluster,
through the `resources` section of the manifest, with two knobs:

- `requests`: initial requirement
- `limits`: maximum usage, in case of dynamic increase of resource needs

For example, you can request an initial amount of RAM of 32MiB (scalable to 128MiB) and 50m of CPU (scalable to 100m) as follows:

```yaml
  resources:
    requests:
      memory: "32Mi"
      cpu: "50m"
    limits:
      memory: "128Mi"
      cpu: "100m"
```

[//]: # ( TODO: we may want to explain what happens to a pod that excedes the resource limits: CPU -> trottle; MEMORY -> kill )

!!! Seealso "Managing Compute Resources for Containers"
    For more details on resource management, please refer to the
    ["Managing Compute Resources for Containers"](https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/)
    page from the Kubernetes documentation.

## Storage configuration

- `pvcTemplate`: template to be used to generate the Persistent Volume Claim
- `size`: size of the storage (*required* if not already specified in the PVC template)
- `storageClass`: `StorageClass` to use to contain PostgreSQL database (aka `PGDATA`);
   the storage class is applied after evaluating the PVC template, if available

!!! Seealso "See also"
   Please refer to the ["Configuration samples" page](samples.md) for examples on storage configuration.

