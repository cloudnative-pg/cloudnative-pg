# Running E2E tests on your environment

[Continuous Integration](https://cloud.google.com/architecture/devops/devops-tech-continuous-integration)
and [Continuous Testing](https://cloud.google.com/architecture/devops/devops-tech-test-automation)
are two fundamental capabilities and cultural principles on which CloudNativePG
has been built.

As a result, we have built a solid and portable way to run End-To-End tests on
CloudNativePG. For example, we need to make sure that every commit does not
break the existing behaviour of the operator, for **all supported versions of
PostgreSQL** on **all supported versions of Kubernetes**.

This framework is made up by two important components:

- a local and disposable Kubernetes cluster built with `kind` or `k3d` on which
  to run the E2E tests
- a set of E2E tests to be run on an existing Kubernetes cluster
  (including the one above)

## The local Kubernetes cluster for testing

The [`hack/setup-cluster.sh`](../../hack/setup-cluster.sh) bash script is
responsible for creating a local Kubernetes cluster to be used for both
development and testing purposes.

> **IMPORTANT:** Make sure you have followed the instructions included in
> ["Setting up your development environment for CloudNativePG"](../development_environment/README.md).


You can create a new Kubernetes cluster with:

``` shell
hack/setup-cluster.sh <OPTIONAL_FLAGS> create
```

You can also build the operator image and load it in the local Kubernetes
cluster. For example, to load the operator image you can run:

``` shell
hack/setup-cluster.sh load
```

To deploy the operator:

``` shell
hack/setup-cluster.sh deploy
```

To cleanup everything:

``` shell
hack/setup-cluster.sh destroy
```

All flags have corresponding environment variables labeled `(Env:...` in the table below.

| Flags                               | Usage                       |
|-------------------------------------|-----------------------------|
| -e    / --engine <CLUSTER_ENGINE>   | Use the specified Kubernetes engine (e.g., `-e k3d`). (Env: `CLUSTER_ENGINE`)                                   |
| -k    / --k8s-version <K8S_VERSION> | Use the specified Kubernetes full version number (e.g., `-k v1.30.0`). (Env: `K8S_VERSION`)                                   |
| -n    / --nodes \<NODES>            | Create a cluster with the required number of nodes. Used only during "create" command. Default: 3 (Env: `NODES`)              |

> **NOTE:** on ARM64 architecture like Apple M1/M2/M3, `kind` provides different
> images for AMD64 and ARM64 nodes. If the **x86/amd64 emulation** is not enabled,
> the `./hack/setup-cluster.sh` script will correctly detect the architecture
> and pass the `DOCKER_DEFAULT_PLATFORM=linux/arm64` environment variable to Docker
> to use the ARM64 node image.
> If you want to explicitly use the **x86/amd64 emulation**, you need to set
> the `DOCKER_DEFAULT_PLATFORM=linux/amd64` environment variable before
> calling the `./hack/setup-cluster.sh` script.


## Profiling tools

In addition to deploying the operator, `hack/setup-cluster.sh`
can enable two powerful profiling tools: *pprof* and *pyroscope*.

After deploying the operator, run:

``` shell
hack/setup-cluster.sh pyroscope
```

This creates a Pyroscope deployment and service on port 4040 in the
`pyroscope` namespace:

``` shell
kubectl get svc -n pyroscope pyroscope

NAME        TYPE        CLUSTER-IP    EXTERNAL-IP   PORT(S)    AGE
pyroscope   ClusterIP   10.96.29.42   <none>        4040/TCP   59m
```

It also enables the pprof server (`--pprof-server` flag), adds Pyroscope
scraping annotation to the operator, and allows "profiles.grafana.com/*"
to be inherited by PostgreSQL instances.

To enable pprof and Pyroscope scraping on PostgreSQL instances, add these
annotations to your Cluster:

``` yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-example
  annotations:
    # Enable pproof server
    alpha.cnpg.io/enableInstancePprof: "true"

    # Pyroscope configuration
    profiles.grafana.com/memory.scrape: "true"
    profiles.grafana.com/memory.port: "6060"
    profiles.grafana.com/cpu.scrape: "true"
    profiles.grafana.com/cpu.port: "6060"
    profiles.grafana.com/goroutine.scrape: "true"
    profiles.grafana.com/goroutine.port: "6060"
spec:
  instances: 3
  ...
```

To access the Pyroscope web UI, port-forward:

``` shell
kubectl port-forward -n pyroscope svc/pyroscope 4040
```

Then open [http://localhost:4040](http://localhost:4040).

See [operator config](../../docs/src/operator_conf.md#profiling-tools)
and [instance-pprof](../../docs/src/troubleshooting.md#visualizing-and-analyzing-profiling-data)
for details.

If issues arise, verify that teh Pyroscope annotations are on the pods
and the `--pprof-server` flag is present in the pod command arguments.

## E2E tests suite

E2E testing is performed by running the
[`hack/e2e/run-e2e.sh`](../../hack/e2e/run-e2e.sh) bash script, making sure
you have a Kubernetes cluster and `kubectl` is configured to point to it
(this should be the default when you are running tests locally as per the
instructions in the above section).

The script can be configured through the following environment variables:

* `CONTROLLER_IMG`: the controller image to deploy on K8s
* `POSTGRES_IMG`: the PostgreSQL image used by default in the clusters
* `E2E_PRE_ROLLING_UPDATE_IMG`: test a rolling upgrade from this version to the
  latest minor
* `E2E_DEFAULT_STORAGE_CLASS`: default storage class, depending on the provider
* `E2E_CSI_STORAGE_CLASS`: csi storage class to be used together with volume snapshots, depending on the provider,
  must be set if `E2E_DEFAULT_STORAGE_CLASS` is not a csi storage class
* `E2E_DEFAULT_VOLUMESNAPSHOT_CLASS`: default volume snapshot class, depending on the provider,
  need to match with `E2E_CSI_STORAGE_CLASS`
* `AZURE_STORAGE_ACCOUNT`: Azure storage account to test backup and restore, using Barman Cloud on Azure
  blob storage
* `AZURE_STORAGE_KEY`: Azure storage key to test backup and restore, using Barman Cloud on Azure
  blob storage
* `TEST_DEPTH`: maximum test level included in the run.
   From `0` (only critical tests) to `4` (all the tests), default `2`
* `FEATURE_TYPE`: Feature type key to run e2e based on feature labels.Ex: smoke, basic, security... details
  can be fetched from labels file [`tests/labels.go`](../../tests/labels.go)

If the `CONTROLLER_IMG` is in a private registry, you'll also need to define
the following variables to create a pull secret:

* `DOCKER_SERVER`: the registry containing the image
* `DOCKER_USERNAME`: the registry username
* `DOCKER_PASSWORD`: the registry password

Additionally, you can specify a DockerHub mirror to be used by
specifying the following variable

* `DOCKER_REGISTRY_MIRROR`: DockerHub mirror URL (i.e. https://mirror.gcr.io)

To run E2E testing you can also use `TEST_UPGRADE_TO_V1=false make e2e-test-kind`.
Replace `-kind` with `-k3d` to run it on `k3d`.

### Using feature type test selection/filter

All the current test cases are labeled with features. Which can be selected
by exporting value `FEATURE_TYPE` and running any script. By default, if test level is not
exported, it will select all medium test cases from the feature type provided.

| Currently Available Feature Types |
|-----------------------------------|
| `disruptive`                      |
| `performance`                     |
| `upgrade`                         |
| `smoke`                           |
| `basic`                           |
| `service-connectivity`            |
| `self-healing`                    |
| `backup-restore`                  |
| `snapshot`                        |
| `operator`                        |
| `observability`                   |
| `replication`                     |
| `plugin`                          |
| `postgres-configuration`          |
| `pod-scheduling`                  |
| `cluster-metadata`                |
| `recovery`                        |
| `importing-databases`             |
| `storage`                         |
| `security`                        |
| `maintenance`                     |
| `tablespaces`                     |
| `publication-subscription`        |
| `declarative-databases`           |
| `postgres-major-upgrade`          |
| `image-volume-extensions`         |

ex:
```shell
export FEATURE_TYPE=smoke,basic,service-connectivity
```
This will run smoke, basic and service connectivity e2e.
One or many can be passed as value with comma separation without spaces.

### Wrapper scripts for E2E testing

There is a script available that wraps cluster setup and execution of
tests for `kind` and `k3d`. It embeds `hack/setup-cluster.sh` and
`hack/e2e/run-e2e.sh` to create a local Kubernetes cluster and then
run E2E tests on it.

There is also a script to run E2E tests on an existing Kubernetes
cluster. It tries to detect the appropriate defaults for
storage class and volume snapshot class environment variables by
looking at the annotation of the default storage class and the volume
snapshot class.

#### On kind

You can test the operator locally on `kind` with:

```shell
CLUSTER_ENGINE=kind run-e2e-local.sh
```

It will take care of creating a `kind` cluster and run the tests on it.

We have also provided a shortcut to this script in the main `Makefile`:

```shell
make e2e-test-kind
```

#### On k3d

You can test the operator locally on `k3d` with:

```shell
CLUSTER_ENGINE=k3d run-e2e-local.sh
```

It will take care of creating a `k3d` cluster and run the tests on it.

We have also provided a shortcut to this script in the main `Makefile`:

```shell
make e2e-test-k3d
```

#### On existing cluster

You can test the operator on an existing Kubernetes cluster with:

``` bash
run-e2e-existing-cluster.sh
```

The script will try detecting the storage class and volume snapshot class to use
by looking at the following annotations and environment variables:

* `storageclass.kubernetes.io/is-default-class: "true"` for the default storage class to use
* `E2E_CSI_STORAGE_CLASS` variable for the default CSI storage class to use. The default is `csi-hostpath-sc`
* `storage.kubernetes.io/default-snapshot-class: "$SNAPSHOT_CLASS_NAME"` for the default volume snapshot class
   to use with the storage class provided in the `E2E_CSI_STORAGE_CLASS` environment variable.

The clusters created by `setup-cluster.sh` script will have the correct storage class and volume snapshot class
detected automatically.

The script will then run the tests on the existing cluster.

We have also provided a shortcut to this script in the main `Makefile`:

```shell
make e2e-test-existing-cluster
```

#### Environment variables

In addition to the environment variables for the script,
the following ones can be defined:

* `PRESERVE_CLUSTER`: true to prevent the script from destroying the Kubernetes cluster.
  Default: `false`
* `PRESERVE_NAMESPACES`: space separated list of namespace to be kept after
  the tests. Only useful if specified with `PRESERVE_CLUSTER=true`
* `K8S_VERSION`: the version of K8s to run. Default: `v1.30.0`
* `BUILD_IMAGE`: true to build the Dockerfile and load it on kind,
  false to get the image from a registry. Default: `false`
* `LOG_DIR`: the directory where the container logs are exported. Default:
  `_logs/` directory in the project root

`run-e2e-local.sh` forces `E2E_DEFAULT_STORAGE_CLASS=standard` in case of `kind`
and `E2E_DEFAULT_STORAGE_CLASS=local-path` in case of `k3d`.

By default, the script uses the `setup-cluster.sh` script to initialize the cluster using
the `kind` engine.

### Running E2E tests on a fork of the repository

**For maintainers and organization members:** If you fork the repository and want to run the tests on your fork, you can do so
by running the `/test` command in a Pull Request opened in your forked repository.
`/test` is used to trigger a run of the end-to-end tests in the GitHub Actions.
Only users who have `write` permission to the repository can use this command.

**For external contributors:** You can run local e2e tests using:
- `FEATURE_TYPE=smoke,basic make e2e-test-kind` for smoke and basic tests
- `TEST_DEPTH=0 make e2e-test-kind` for critical tests only  
- `TEST_DEPTH=1 make e2e-test-kind` for critical and high priority tests

> NOTE:
> to run the same on `k3d` simply replace the above `make` commands with `make e2e-test-k3d`.

Maintainers will handle comprehensive cloud-based E2E testing during the pull request review process.

Options supported are:

- test_level (`level` or `tl` for short)
  Each e2e test defines its own importance from 0(highest) to 4(lowest).
  With `test_level` you can choose which tests the suite should be running.
  If `test_level` is set, all tests with that importance or higher will be triggered.
  E.g. selecting `1` will only run level `1` (high) and `0` (highest) tests.
  Available values are:
  - 0: highest
  - 1: high
  - 2: medium
  - 3: low
  - 4: lowest (default)

- depth (`d` for short)
  Depth determines the matrix of K8S_VERSION x POSTGRES_VERSION jobs where E2E tests will be executed.
  Default value is `main`. Available values are:
  - push:
    * oldest K8S_VERSION x oldest POSTGRES_VERSION
    * latest K8S_VERSION x latest POSTGRES_VERSION
    * no cloud providers
  - main:
    * each K8S_VERSION x oldest POSTGRES_VERSION
    * each K8S_VERSION x latest POSTGRES_VERSION
    * latest K8S_VERSION x each POSTGRES_VERSION
    * On cloud providers: latest K8S_VERSION x latest POSTGRES_VERSION
  - pull_request:
    * same as `main`
  - schedule:
    * same as `main`
    * On cloud providers: each K8S_VERSION x latest POSTGRES_VERSION

- feature_type (`type` or `ft` for short)
  A label to select a subset of E2E tests to be run, divided by functionality.
  Empty value means no filter is applied. Default value is empty.
  Available options are: [Relate to the feature types section](#using-feature-type-test-selectionfilter)

- log_level (`ll` for short)
  Defines the log value for CloudNativePG operator, which will be specified as the value for the `--log-value`
  argument in the operator command. Available values are:
  - error
  - warning
  - info
  - debug (default)
  - trace

Example:
1. Trigger an e2e test to run all test cases with `lowest` test level.
   We want to cover most Kubernetes x Postgres combinations.
   ```
      /test tl=4 d=schedule
   ```
2. Run only smoke and upgrade tests
   ```
      /test type=smoke,upgrade
   ```

## Storage class for volume snapshots on Kind

In order to enable testing of Kubernetes volume snapshots on a local Kind
Cluster, we are installing the `csi-hostpath-sc` storage class and the
`csi-hostpath-snapclass` volume snapshot class.
