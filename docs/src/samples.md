In this section you can find some examples of configuration files to setup your PostgreSQL `Cluster`.

* [`cluster-example.yaml`](samples/cluster-example.yaml):
   basic example of `Cluster` that uses the default storage class. For demonstration and experimentation purposes
   on a personal Kubernetes cluster with Minikube or Kind as described in the ["Quickstart"](quickstart.md).
* [`cluster-example-custom.yaml`](samples/cluster-example-custom.yaml):
   basic example of `Cluster` that uses the default storage class and custom parameters for `postgresql.conf` and
   `pg_hba.conf` files
* [`cluster-storage-class.yaml`](samples/cluster-storage-class.yaml):
   basic example of `Cluster` that uses a specified storage class.
* [`cluster-pvc-template.yaml`](samples/cluster-pvc-template.yaml):
   basic example of `Cluster` that uses a persistent volume claim template.

For a list of available options, please refer to the ["Custom Resource Definitions" page](crd.md).
