# Running E2E tests on your environment

[Continuous Integration](https://cloud.google.com/architecture/devops/devops-tech-continuous-integration)
and [Continous Testing](https://cloud.google.com/architecture/devops/devops-tech-test-automation)
are two fundamental capabilities and cultural principles on which CloudNativePG
has been built.

As a result, we have built a solid and portable way to run End-To-End tests on
CloudNativePG. For example, we need to make sure that every commit does not
break the existing behaviour of the operator, for **all supported versions of
PostgreSQL** on **all supported versions of Kubernetes**.

This framework is made up by two important components:

- a local and disposable Kubernetes cluster built with `kind` (default) or
  `k3d` on which to run the E2E tests
- a set of E2E tests to be run on an existing Kubernetes cluster
  (including the one above)

## The local Kubernetes cluster for testing

The [`hack/setup-cluster.sh`](../../../hack/setup-cluster.sh) bash script is
responsible for creating a local Kubernetes cluster to be used for both
development and testing purposes.

> **IMPORTANT:** Make sure you have followed the instructions included in
> ["Setting up your development environment for CloudNativePG"](../development_environment/README.md).


You can create a new Kubernetes cluster with:

```console
hack/setup-cluster.sh <OPTIONAL_FLAGS> create
```

You can also build the operator image and load it in the local Kubernetes
cluster. For example, to load the operator image you can run:

```console
hack/setup-cluster.sh load
```

To deploy the operator:

```console
hack/setup-cluster.sh deploy
```

To cleanup everything:

```console
hack/setup-cluster.sh destroy
```

All flags have corresponding environment variables labeled `(Env:...` in the table below.

| Flags                                | Usage                                                                                                                         |
|--------------------------------------|-------------------------------------------------------------------------------------------------------------------------------|
| -r\|--registry                       | Enable local registry. (Env: `ENABLE_REGISTRY`)                                                                               |
| -e\|--engine <CLUSTER_ENGINE>        | Use the provided ENGINE to run the cluster. Available options are 'kind' and 'k3d'. Default 'kind'. (Env: `CLUSTER_ENGINE`)   |
| -k\|--k8s-version <K8S_VERSION>      | Use the specified Kubernetes full version number (e.g., `-k v1.23.1`). (Env: `K8S_VERSION`)                                   |
| -n\|--nodes <NODES>                  | Create a cluster with the required number of nodes. Used only during "create" command. Default: 3 (Env: `NODES`)              |


> **NOTE:** if you want to use custom engine and registry settings, please make
> sure that they are consistent through all invocations either via command line
> options or by defining the respective environment variables

## E2E tests suite

E2E testing is performed by running the
[`hack/e2e/run-e2e.sh`](../../../hack/e2e/run-e2e.sh) bash script, making sure
you have a Kubernetes cluster and `kubectl` is configured to point to it
(this should be the default when you are running tests locally as per the
instructions in the above section).

The script can be configured through the following environment variables:

* `CONTROLLER_IMG`: the controller image to deploy on K8s
* `POSTGRES_IMG`: the PostgreSQL image used by default in the clusters
* `E2E_PRE_ROLLING_UPDATE_IMG`: test a rolling upgrade from this version to the
  latest minor
* `E2E_DEFAULT_STORAGE_CLASS`: default storage class, depending on the provider
* `AZURE_STORAGE_ACCOUNT`: Azure storage account to test backup and restore, using Barman Cloud on Azure 
   blob storage
* `AZURE_STORAGE_KEY`: Azure storage key to test backup and restore, using Barman Cloud on Azure
  blob storage

If the `CONTROLLER_IMG` is in a private registry, you'll also need to define
the following variables to create a pull secret:

* `DOCKER_SERVER`: the registry containing the image
* `DOCKER_USERNAME`: the registry username
* `DOCKER_PASSWORD`: the registry password

Additionally, you can specify a DockerHub mirror to be used by
specifying the following variable

* `DOCKER_REGISTRY_MIRROR`: DockerHub mirror URL (i.e. https://mirror.gcr.io)

To run E2E testing you can also use:

|                    kind                        |                     k3d                         |
|------------------------------------------------|-------------------------------------------------|
| `TEST_UPGRADE_TO_V1=false make e2e-test-kind`  | `TEST_UPGRADE_TO_V1=false make e2e-test-k3d`    |


### Wrapper scripts for E2E testing

There are currently two available scripts that wrap setup of the cluster and
execution of tests. One is for `kind` and one is for `k3d`. They simply embed
`hack/setup-cluster.sh` and `hack/e2e/run-e2e.sh` to create a local Kubernetes
cluster and then run E2E tests on it.

#### On kind

You can test the operator locally on `kind` with:

``` bash
run-e2e-kind.sh
```

It will take care of creating a `kind` cluster and run the tests on it.

#### On k3d

You can test the operator locally on `k3d` with:

``` bash
run-e2e-k3d.sh
```

> **NOTE:** error messages, like the example below, that will be shown during
> cluster creation are **NOT** an issue:

```
Error response from daemon: manifest for rancher/k3s:v1.20.0-k3s5 not found: manifest unknown: manifest unknown
```

The script will take care of creating a K3d cluster and then run the tests on it.

#### Environment variables

In addition to the environment variables for the script,
the following ones can be defined:

* `PRESERVE_CLUSTER`: true to prevent the script from destroying the kind cluster.
  Default: `false`
* `PRESERVE_NAMESPACES`: space separated list of namespace to be kept after
  the tests. Only useful if specified with `PRESERVE_CLUSTER=true`
* `K8S_VERSION`: the version of K8s to run. Default: `v1.23.1`
* `KIND_VERSION`: the version of `kind`. Defaults to the latest release
* `BUILD_IMAGE`: true to build the Dockerfile and load it on kind,
  false to get the image from a registry. Default: `false`
* `LOG_DIR`: the directory where the container logs are exported. Default:
  `_logs/` directory in the project root

`run-e2e-kind.sh` forces `E2E_DEFAULT_STORAGE_CLASS=standard` while
`run-e2e-k3d.sh` forces `E2E_DEFAULT_STORAGE_CLASS=local-path`

Both scripts use the `setup-cluster.sh` script, in order to initialize the cluster
choosing between `kind` or K3d engine.
