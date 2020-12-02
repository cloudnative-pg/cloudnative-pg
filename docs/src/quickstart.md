# Quickstart

This section describes how to test a PostgreSQL cluster on your laptop/computer
using Cloud Native PostgreSQL on a local Kubernetes cluster in
[Minikube](https://kubernetes.io/docs/setup/learning-environment/minikube/) or
[Kind](https://kind.sigs.k8s.io/).

RedHat OpenShift Container Platform users can test the certified operator for
Cloud Native PostgreSQL on the [Red Hat CodeReady Containers (CRC)](https://developers.redhat.com/products/codeready-containers/overview)
for OpenShift.

!!! Warning
    The instructions contained in this section are for demonstration,
    testing, and practice purposes only and must not be used in production.

Like any other Kubernetes application, Cloud Native PostgreSQL is deployed using
regular manifests written in YAML.

By following the instructions on this page you should be able to start a PostgreSQL
cluster on your local Kubernetes/Openshift installation and experiment with it.

!!! Important
    Make sure that you have `kubectl` installed on your machine in order
    to connect to the Kubernetes cluster, or `oc` if using CRC for OpenShift.
    Please follow the Kubernetes documentation on [how to install `kubectl`](https://kubernetes.io/docs/tasks/tools/install-kubectl/) or the Openshift one on [how to install `oc`](https://docs.openshift.com/container-platform/4.6/cli_reference/openshift_cli/getting-started-cli.html).


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

[Download RedHat CRC](https://developers.redhat.com/products/codeready-containers/overview)
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

## Part 2 - Install Cloud Native PostgreSQL

Now that you have a Kubernetes or OpenShift installation up and running
on your laptop, you can proceed with Cloud Native PostgreSQL installation.


### Kubernetes

Download the [latest operator manifest](samples/postgresql-operator-0.5.0.yaml)
and run:

```sh
kubectl apply -f postgresql-operator-0.5.0.yaml
```

Once you have run the `kubectl` command, Cloud Native PostgreSQL will be installed in your Kubernetes cluster.
You can verify that with:

```sh
kubectl get deploy -n postgresql-operator-system postgresql-operator-controller-manager
```

### Openshift

#### Using the web interface

Log in to the console as `kubeadmin` and navigate to the  `Operator → OperatorHub` page.

Find the `Cloud Native PostgreSQL` box scrolling or using the search filter.

Select the operator and click `Install`. Click `Install` again in the following
`Install Operator`, using the default settings. For an in-depth explanation of
those settings, see the [Openshift documentation](https://docs.openshift.com/container-platform/4.6/operators/admin/olm-adding-operators-to-cluster.html#olm-installing-from-operatorhub-using-web-console_olm-adding-operators-to-a-cluster).

The operator will soon be available in all the namespaces.

### Using the cli

You can apply the subscription on [`subscription.yaml`](samples/subscription.yaml)
to install the operator in all the namespaces.
Download the yaml file and run:

```sh
oc apply -f subscription.yaml
```

The operator will soon be available in all the namespaces.

More information on
[how to install operators via CLI](https://docs.openshift.com/container-platform/4.6/operators/admin/olm-adding-operators-to-cluster.html#olm-installing-operator-from-operatorhub-using-cli_olm-adding-operators-to-a-cluster)
is available in the Openshift documentation.

## Part 3 - Deploy a PostgreSQL cluster

As with any other deployment in Kubernetes, to deploy a PostgreSQL cluster
you need to apply a configuration file that defines your desired `Cluster`.

The [`cluster-example.yaml`](samples/cluster-example.yaml) sample file
defines a simple `Cluster` using the default storage class to allocate
disk space:

```yaml
# Example of PostgreSQL cluster
apiVersion: postgresql.k8s.enterprisedb.io/v1alpha1
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
