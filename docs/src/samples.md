In this section you can find some examples of configuration files to setup your PostgreSQL `Cluster`.

* [`cluster-emptydir.yaml`](samples/cluster-emptydir.yaml):
   basic example of `Cluster` that uses `emptyDir` local storage. For demonstration and experimentation purposes
   on a personal Kubernetes cluster with Minikube or Kind as described in the ["Quickstart"](quickstart.md).
* [`cluster-storage-class.yaml`](samples/cluster-storage-class.yaml):
   basic example of `Cluster` that uses a specified storage class.
* [`cluster-pvc-template.yaml`](samples/cluster-pvc-template.yaml):
   basic example of `Cluster` that uses a persistent volume claim template.

For a list of available options, please refer to the ["Custom Resource Definitions" page](crd.md).
