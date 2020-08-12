# Cloud Native PostgreSQL

**Cloud Native PostgreSQL** is a stack designed by
[2ndQuadrant](https://www.2ndquadrant.com) to manage PostgreSQL
workloads on Kubernetes, particularly optimised for Private Cloud environments
with Local Persistent Volumes (PV).

## Quickstart for local testing (TODO)

Temporary information on how to test PG Operator using private images on Quay.io:

```bash
kind create cluster --name pg
make deploy CONTROLLER_IMG=internal.2ndq.io/k8s/cloud-native-postgresql:$(git symbolic-ref --short HEAD | tr / _)
kubectl apply -f docs/src/samples/cluster-emptydir.yaml
```

# How to upgrade the list of licenses

To generate the `licenses` folder you'll need **go-licenses**, and you can
install it with:

```bash
go get github.com/google/go-licenses
```

Then, simply:

```bash
make licenses
```

# Release process

* Update the `NEWS` file for the new version. A command like
  `git log --pretty=oneline v0.1.0..master` where `v0.1.0`
  is the latest released version will be useful.

* Update the version number in `pkg/versions/versions.go`, inside
  the `Version` and the `DefaultOperatorImageName` constants.

* Update the `version` tag in `Dockerfile` `LABEL` command with
  the new version (without the starting `v`). Update also the
  `revision` tag if appropriate.

* Update the OpenShift ClusterServiceVersion object in
  `config/manifests/bases/cloud-native-postgresql.clusterserviceversion.yaml`
  file with the new version.

* Create a new YAML definition for the new version and add it to the
  commit:

```
# If 0.2.0 is the version to be released:
export OPERATOR_VERSION=0.2.0
export CONTROLLER_IMG=2ndq.io/release/k8s/cloud-native-postgresql-operator:v0.2.0
make yaml_manifest
```

* Create a Git tag and wait for the CI to build the operator image:

```
git tag -sam "Release 0.2.0" v0.2.0
git push --tags
```

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
