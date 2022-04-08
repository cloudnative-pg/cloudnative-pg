# Quickstart

This section describes how to test a PostgreSQL cluster on your laptop/computer
using CloudNativePG on a local Kubernetes cluster in
[Minikube](https://kubernetes.io/docs/setup/learning-environment/minikube/) or
[Kind](https://kind.sigs.k8s.io/).

Red Hat OpenShift Container Platform users can test the certified operator for
CloudNativePG on the [Red Hat CodeReady Containers (CRC)](https://developers.redhat.com/products/codeready-containers/overview)
for OpenShift.

!!! Warning
    The instructions contained in this section are for demonstration,
    testing, and practice purposes only and must not be used in production.

Like any other Kubernetes application, CloudNativePG is deployed using
regular manifests written in YAML.

By following the instructions on this page you should be able to start a PostgreSQL
cluster on your local Kubernetes/Openshift installation and experiment with it.

!!! Important
    Make sure that you have `kubectl` installed on your machine in order
    to connect to the Kubernetes cluster, or `oc` if using CRC for OpenShift.
    Please follow the Kubernetes documentation on [how to install `kubectl`](https://kubernetes.io/docs/tasks/tools/install-kubectl/)
    or the Openshift one on [how to install `oc`](https://docs.openshift.com/container-platform/4.6/cli_reference/openshift_cli/getting-started-cli.html).


!!! Note
    If you are running Openshift, use `oc` every time `kubectl` is mentioned
    in this documentation. `kubectl` commands are compatible with `oc` ones.

## Part 1 - Setup the local Kubernetes/Openshift playground

The first part is about installing Minikube, Kind, or CRC. Please spend some time
reading about the systems and decide which one to proceed with.
After setting up one of them, please proceed with part 2.

### Minikube

Minikube is a tool that makes it easy to run Kubernetes locally. Minikube runs a
single-node Kubernetes cluster inside a Virtual Machine (VM) on your laptop for
users looking to try out Kubernetes or develop with it day-to-day. Normally, it
is used in conjunction with VirtualBox.

You can find more information in the official [Kubernetes documentation on how to
install Minikube](https://kubernetes.io/docs/tasks/tools/install-minikube) in your local personal environment.
When you installed it, run the following command to create a minikube cluster:

```sh
minikube start
```

This will create the Kubernetes cluster, and you will be ready to use it.
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

### CodeReady Containers (CRC)

[Download Red Hat CRC](https://developers.redhat.com/products/codeready-containers/overview)
and move the binary inside a directory in your `PATH`.

You can then run the following commands:
```
crc setup
crc start
```

The `crc start` output will explain how to proceed. You'll then need to
execute the output of the `crc oc-env` command.
After that, you can log in as `kubeadmin` with the printed `oc login`
command. You can also open the web console running `crc console`.
User and password are the same as for the `oc login` command.

CRC doesn't come with a StorageClass, so one has to be configured.
You can follow the [Dynamic volume provisioning wiki page](https://github.com/code-ready/crc/wiki/Dynamic-volume-provisioning)
and install `rancher/local-path-provisioner`.

## Part 2 - Install CloudNativePG

Now that you have a Kubernetes or OpenShift installation up and running
on your laptop, you can proceed with CloudNativePG installation.

Please refer to the ["Installation"](installation_upgrade.md) section and then proceed
with the deployment of a PostgreSQL cluster.

## Part 3 - Deploy a PostgreSQL cluster

As with any other deployment in Kubernetes, to deploy a PostgreSQL cluster
you need to apply a configuration file that defines your desired `Cluster`.

The [`cluster-example.yaml`](samples/cluster-example.yaml) sample file
defines a simple `Cluster` using the default storage class to allocate
disk space:

```yaml
# Example of PostgreSQL cluster
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-example
spec:
  instances: 3

  # Example of rolling update strategy:
  # - unsupervised: automated update of the primary once all
  #                 replicas have been upgraded (default)
  # - supervised: requires manual supervision to perform
  #               the switchover of the primary
  primaryUpdateStrategy: unsupervised

  # Require 1Gi of space
  storage:
    size: 1Gi
```

!!! Note "There's more"
    For more detailed information about the available options, please refer
    to the ["API Reference" section](api_reference.md).

In order to create the 3-node PostgreSQL cluster, you need to run the following command:

```sh
kubectl apply -f cluster-example.yaml
```

You can check that the pods are being created with the `get pods` command:

```sh
kubectl get pods
```

By default, the operator will install the latest available minor version
of the latest major version of PostgreSQL when the operator was released.
You can override this by setting the `imageName` key in the `spec` section of
the `Cluster` definition. For example, to install PostgreSQL 12.5:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
   # [...]
spec:
   # [...]
   imageName: quay.io/enterprisedb/postgresql:12.5
   #[...]
```

!!! Important
    The immutable infrastructure paradigm requires that you always
    point to a specific version of the container image.
    Never use tags like `latest` or `13` in a production environment
    as it might lead to unpredictable scenarios in terms of update
    policies and version consistency in the cluster.
    For strict deterministic and repeatable deployments, you can add the digests
    to the image name, through the `<image>:<tag>@sha256:<digestValue>` format.
