# Cloud Native PostgreSQL

**Cloud Native PostgreSQL** is a stack designed by
[2ndQuadrant](https://www.2ndquadrant.com) to manage PostgreSQL
workloads on Kubernetes, particularly optimised for Private Cloud environments
with Local Persistent Volumes (PV).

## How to create a development environment

To develop the BDR operator, you will need an UNIX based operating system that
support the following softwares, which must be available in the `PATH`
environment variable:

- Go 1.13+ compiler
- GNU Make
- [Kind](https://kind.sigs.k8s.io/)
- [golangci-lint](https://github.com/golangci/golangci-lint)

On Mac OS X, you can install the above components through `brew`:

    brew install go kind golangci/tap/golangci-lint

You can invoke the compilation procedure with:

    make

!!! note
    Kustomize version v3.8.2 and greater is not compatible with the current version
    of the build system. In case you have it installed it is advised to remove it
    and let the build system to download a compatible version of the software.

## Quickstart for local testing of a git branch

If you want to deploy a cluster using the operator from your current git branch,
you can use the following commands:

```bash
kind create cluster --name pg
kubectl create namespace postgresql-operator-system
kubectl create secret docker-registry \
    -n postgresql-operator-system \
    postgresql-operator-pull-secret \
    --docker-server=internal.2ndq.io \
    --docker-username=$GITLAB_TOKEN_USERNAME \
    --docker-password=$GITLAB_TOKEN_PASSWORD
make deploy CONTROLLER_IMG=internal.2ndq.io/k8s/cloud-native-postgresql:$(git symbolic-ref --short HEAD | tr / _)
kubectl apply -f docs/src/samples/cluster-example.yaml
```

Replace `$GITLAB_TOKEN_USERNAME` and `$GITLAB_TOKEN_PASSWORD` with one with the permission to pull
from the gitlab docker registry.

# How to upgrade the list of licenses

To generate or update the `licenses` folder run the following command:

```bash
make licenses
```

# Release process

* Update the `NEWS` file for the new version. A command like
  `git log --pretty=oneline v0.1.0..master` where `v0.1.0`
  is the latest released version will be useful.

* run `hack/release.sh v0.2.0` where `v0.2.0`
  is the new version to be released.

* Create the release on the Portal and upload the manifest generated in
  the previous point in `releases/postgresql-operator-0.2.0.yaml`

* Update the official documentation by updating the
  [cnp-docs-packaging](ssh://git@git.2ndquadrant.com/it/ci/packaging/cnp-docs-packaging.git)
  source and creating a new tag named after the version and the packaging version
  (i.e. `v0.2.0-1`). Then, run a new build from
  [Jenkins](https://ci.2ndquadrant.com/jenkins/job/cloud-native-postgresql-docs/job/cloud-native-postgresql-docs/)
  (you can reuse an existing release build task and change the tag name).

* Add the new release to `releases.map` in the [k8s-release
  repo](https://gitlab.2ndquadrant.com/release/k8s) and update the
  metadata about the latest image:

```
# CNP <VERSION>
k8s/cloud-native-postgresql:<VERSION>=cloud-native-postgresql-operator:<VERSION>

# Meta
k8s/cloud-native-postgresql:<VERSION>=cloud-native-postgresql-operator:latest
```

  When you commit the new file GitLab will copy the images to the production
  repository.
