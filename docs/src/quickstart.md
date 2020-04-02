This section describes how to test a PostgreSQL cluster on your laptop/computer,
using a local Kubernetes cluster in
[Minikube](https://kubernetes.io/docs/setup/learning-environment/minikube/) or
[Kind](https://kind.sigs.k8s.io/) via Cloud Native PostgreSQL.
Like any other Kubernetes application, Cloud Native PostgreSQL is deployed using
regular manifests written in YAML.

!!! Warning
    The instructions contained in this section are for demonstration,
    testing and practice purposes only and must not be used in production.

Cloud Native PostgreSQL has been tested on two widespread tools for running
Kubernetes locally, available on major platforms such as Linux, Mac OS X
and Windows:

- [Minikube](https://kubernetes.io/docs/setup/learning-environment/minikube/)
- [Kind](https://kind.sigs.k8s.io/)

By following the instructions in this page you should be able to start a PostgreSQL
cluster on your local Kubernetes installation and experiment with it.

!!! Important
    Make sure that you have `kubectl` installed on your machine in order
    to connect to the Kubernetes cluster.

## Part 1 - Setup the local Kubernetes playground

The first part is about installing Minikube and/or Kind. Please spend some time
reading about which of the two systems proceed with. Once you have setup one or the
other, please proceed with part 2.

### Minikube

Minikube is a tool that makes it easy to run Kubernetes locally. Minikube runs a
single-node Kubernetes cluster inside a Virtual Machine (VM) on your laptop for
users looking to try out Kubernetes or develop with it day-to-day. Normally, it
is used in conjunction with VirtualBox.

You can find more information in the official [Kubernetes documentation on how to
install Minikube](https://kubernetes.io/docs/tasks/tools/install-minikube) in your personal local environment.
When you installed it run the following command to create  minikube cluster:

```sh
minikube start
```

This will create the Kubernetes cluster and you will be ready to use it.
Verify that it works with the following command:

```sh
kubectl get nodes
```

You will see one node called `minikube`.

### Kind

If you do not want to use a virtual machine hypervisor, then Kind is a tool for running
local Kubernetes clusters using Docker container "nodes" (Kind stands for "Kubernetes IN Docker" indeed).

Install `kind` on your environment following the instructions in the [Quickstart](https://kind.sigs.k8s.io/docs/user/quick-start),
then create a Kubernetes cluster with:

```sh
kind create cluster --name pg
```

## Part 2 - Install Cloud Native PostgreSQL

Now that you have a Kubernetes installation up and running on your laptop,
you can proceed with Cloud Native PostgreSQL installation.

Locate the latest release of Cloud Native PostgreSQL from the
["Cloud Native PostgreSQL" page available in the 2ndQuadrant Portal](https://access.2ndquadrant.com/customer_portal/sw/cloud-native-postgresql/).
Follow the installation instructions and run the `kubectl` command that you are presented.

!!! Important
    Please contact your 2ndQuadrant account manager if you do not have access to the Kubernetes manifests of Cloud Native PostgreSQL.

Once you have run the `kubectl` command, Cloud Native PostgreSQL will be installed in your Kubernetes cluster.
You can verify that with:

```sh
kubectl get deploy -n postgresql-operator-system postgresql-operator-controller-manager
```

## Part 3 - Deploy a PostgreSQL cluster

As with any other deployment in Kubernetes, in order to deploy a PostgreSQL cluster
you need to apply a configuration file that defines your desired `Cluster`.

The [`cluster-emptydir.yaml`](samples/cluster-emptydir.yaml) sample file
defines a simple `Cluster` with an `emptyDir` local volume:

```yaml
# Example of PostgreSQL cluster using emptyDir volumes
apiVersion: postgresql.k8s.2ndq.io/v1alpha1
kind: Cluster
metadata:
  name: cluster-emptydir
spec:
  instances: 3

  # Configuration of the application that will be used by
  # this PostgreSQL cluster
  applicationConfiguration:
    database: app
    owner: app

  # Example update strategy:
  # wait: wait for a manual update of the primary
  # switchover: switchover the primary and update the old primary
  primaryUpdateStrategy: switchover

  # PostgreSQL server configuration
  postgresql:
    # Example of configuration parameters for PostgreSQL
    parameters:
      - max_worker_processes = 20
      - max_parallel_workers = 20
      - max_replication_slots = 20
      - hot_standby = true
      - wal_keep_segments = 8

    # Example of host based authentication directives
    pg_hba:
      # Grant local access
      - local all all trust
      # Grant local network access (within k8s cluster)
      - host all all 10.0.0.0/8 trust
      - host all all 172.0.0.0/8 trust
      # Grant local network replication access (within k8s cluster)
      - host replication all 10.0.0.0/8 trust
      - host replication all 172.0.0.0/8 trust
      # Require md5 authentication elsewhere
      - host all all all md5
      - host replication all all md5
```

This will create a `Cluster` called `cluster-emptydir` with a PostgreSQL
primary, two replicas, and a database called `app` owned by the `app` PostgreSQL user.

!!! Note "There's more"
    For more detailed information about the available options, please refer
    to the ["Custom Resource Definitions" section](crd.md).

In order to create the 3-node PostgreSQL cluster, you need to run the following command:

```sh
kubectl apply -f cluster-emptydir.yaml
```

You can check that the pods are being created with the `get pods` command:

```sh
kubectl get pods
```
