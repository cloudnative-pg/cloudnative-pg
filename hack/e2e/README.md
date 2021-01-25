# E2E testing

E2E testing is performing running the `run-e2e.sh` script after setting
up a Kubernetes cluster and configuring `kubectl` to use it.

The script can be configured through the following environment variables:

* `CONTROLLER_IMG`: the controller image to deploy on K8s
* `POSTGRES_IMG`: the postgresql image used by default in the clusters
* `E2E_PRE_ROLLING_UPDATE_IMG`: test a rolling upgrade from this version to the
     latest minor
* `E2E_DEFAULT_STORAGE_CLASS`: default storage class, depending on the provider

If the `CONTROLLER_IMG` is in a private registry, you'll also need to define
the following variables to create a pull secret:

* `DOCKER_SERVER`: the registry containing the image
* `DOCKER_USERNAME`: the registry username
* `DOCKER_PASSWORD`: the registry password

Additionally you can specify a DockerHub mirror to be used by
specifying the following variable

* `DOCKER_REGISTRY_MIRROR`: DockerHub mirror URL (i.e. https://mirror.gcr.io)

## Local test

### On kind

You can test the operator locally on kind running

``` bash
run-e2e-kind.sh
```

It will take care of creating a Kind cluster and run the tests on it.

### On k3d

You can test the operator locally on k3d running

``` bash
run-e2e-k3d.sh
```

NOTE: error messages, like the example below, that will be shown during cluster creation are **NOT** an issue:

```
Error response from daemon: manifest for rancher/k3s:v1.20.0-k3s5 not found: manifest unknown: manifest unknown
```

It will take care of creating a K3d cluster and run the tests on it.

In addition to the environment variables for the script,
the following ones can be defined:

* `PRESERVE_CLUSTER`: true to prevent K8S from destroying the kind cluster.
    Default: `false`.
* `PRESERVE_NAMESPACES`: space separated list of namespace to be kept after
  the tests. Only useful if specified with `PRESERVE_CLUSTER=true`.
* `K8S_VERSION`: the version of K8S to run. Default: `v1.20.0`.
* `KIND_VERSION`: the version of Kind. Defaults to the latest release.
* `BUILD_IMAGE`: true to build the Dockerfile and load it on kind,
    false to get the image from a registry. Default: `false`.
* `LOG_DIR`: the directory where the container logs are exported. Default:
    `_logs/` directory in the project root.

`run-e2e-kind.sh` forces `E2E_DEFAULT_STORAGE_CLASS=standard` while `run-e2e-k3d.sh` forces `E2E_DEFAULT_STORAGE_CLASS=local-path`

Both scripts use the `setup-cluster.sh` script to initialize the cluster
choosing between Kind or K3d engine.

## Local cluster

`setup-cluster.sh` can be used to create a local cluster for developer
purposes, specifying the environment variable `CLUSTER_ENGINE`, as
follows:

``` bash
CLUSTER_ENGINE=k3d setup-cluster.sh
```
By default it will use Kind as engine.

When running this script standalone, it can take the following set of variables: `BUILD_IMAGE`, `K8S_VERSION`, `CLUSTER_NAME`
