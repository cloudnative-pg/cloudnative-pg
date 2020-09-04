# E2E testing

The E2E tests create a `kind` instance and run the tests inside it.
The tests can be run executing

``` bash
make e2e-test
```

from the project root directory if the host is allowed to run privileged
docker containers and to pull required images.

The following environment variables can be exported to change the behaviour
of the tests:

* `BUILD_IMAGE`: if `false` skip image building and just use existing `IMG`
* `CONTROLLER_IMG`: The image name to pull if `BUILD_IMAGE=false`
* `POSTGRES_IMAGE_NAME`: The image used to run PostgreSQL pods (default
   from `pkg/versions/versions.go`)
* `PRESERVE_CLUSTER`: do not remove the `kind` cluster after the end of the
  tests (default: `false`);
* `PRESERVE_NAMESPACES`: space separated list of namespace to be kept after
  the tests. Only useful if specified with `PRESERVE_CLUSTER=true`.
* `KIND_VERSION`: use a specific version of `kind` (default: use the latest);
* `KUBECTL_VERSION`: use a specific version of `kubectl` (default: use the
  latest);
* `K8S_VERSION`: the version of Kubernetes we are testing
* `DEBUG`: display the `bash` commands executed (default: `false`).
