# Configuration Samples

In this section, you can find some examples of configuration files to set up your PostgreSQL `Cluster`.

* [`cluster-example.yaml`](samples/cluster-example.yaml):
   a basic example of `Cluster` that uses the default storage class. For demonstration and experimentation purposes
   on a personal Kubernetes cluster with Minikube or Kind as described in the ["Quickstart"](quickstart.md).
* [`cluster-example-custom.yaml`](samples/cluster-example-custom.yaml):
   a basic example of `Cluster` that uses the default storage class and custom parameters for `postgresql.conf` and
   `pg_hba.conf` files
* [`cluster-storage-class.yaml`](samples/cluster-storage-class.yaml):
   a basic example of `Cluster` that uses a specified storage class
* [`cluster-pvc-template.yaml`](samples/cluster-pvc-template.yaml):
   a basic example of `Cluster` that uses a persistent volume claim template.
* [`cluster-example-full.yaml`](samples/cluster-example-full.yaml):
   an example of `Cluster` that sets most of the available options
* [`cluster-pod-template.yaml`](samples/cluster-pod-template.yaml):
   an example of `Cluster` with a Pod template defining labels,
   annotations and additional containers.

For a list of available options, please refer to the ["API Reference" page](api_reference.md).
