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

## Local test on Kind

You can test the operator locally on kind running

``` bash
run-e2e-kind.sh
```

It will take care of creating a Kind cluster and run the tests on it.
In addition to the environment variables for the script,
the following ones can be defined:

* `PRESERVE_CLUSTER`: true to prevent K8S from destroying the kind cluster.
    Default: `false`.
* `K8S_VERSION`: the version of K8S to run. Default: `v1.19.1`.
* `KIND_VERSION`: the version of Kind. Defaults to the latest release.
* `BUILD_IMAGE`: true to build the Dockerfile and load it on kind,
    false to get the image from a registry. Default: `false`.
* `LOG_DIR`: the directory where the container logs are exported. Default:
    `kind-logs` directory in the project root.

`run-e2e-kind.sh` forces `E2E_DEFAULT_STORAGE_CLASS=standard`
