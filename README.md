# Cloud Native PostgreSQL

**Cloud Native PostgreSQL** is a stack designed by
[EnterpriseDB](https://www.enterprisedb.com) to manage PostgreSQL
workloads on Kubernetes, particularly optimised for Private Cloud environments
with Local Persistent Volumes (PV).

## How to create a development environment

To develop the operator, you will need a UNIX based operating system that
support the following software, which must be available in the `PATH`
environment variable:

- Go 1.16+ compiler
- GNU Make
- [Kind](https://kind.sigs.k8s.io/) v0.11.x or greater
- [golangci-lint](https://github.com/golangci/golangci-lint)

On Mac OS X, you can install the above components through `brew`:

    brew install go kind golangci/tap/golangci-lint

**⚠️ Note:**
Kind version 0.11.x is required at the time of this writing.  If you already
have kind, run `brew upgrade kind` to update to the latest version.

You can invoke the compilation procedure with:

    make

**⚠️ Note:**
Kustomize version v3.8.2 and greater is not compatible with the current version
of the build system. You should remove Kustomize and let the build system
download a compatible version if Kustomize is already installed.

## Quickstart for local testing of a git branch

If you want to deploy a cluster using the operator from your current git
branch, you can use the procedure described in this paragraph.

You must set the `QUAY_USERNAME` and `QUAY_PASSWORD` environment variables to the
values found in your Account Settings in quay.io. Navigate to https://quay.io/
then click on your name in the upper-right and select "Account Settings". Ensure
the User Settings tab on the left is selected, and click on "CLI Password" at the
top of the settings. Enter your quay.io password to see your credentials.
Select "Encrypted Password" on left and copy the "Username", and the "Encrypted Password"
values into the environment variables `QUAY_USERNAME` and `QUAY_PASSWORD` respectively
in your shell. You can now use the following commands:

* `make dev-init` to create a local cluster
* `make dev-clean` to clean up the local cluster
* `make dev-reset` to reinitialize the local cluster

# How to upgrade the list of licenses

To generate or update the `licenses` folder run the following command:

```bash
make licenses
```

# Release process

* Update the `NEWS` file for the new version. A command like
  `git log --pretty=oneline v0.1.0..main` where `v0.1.0`
  is the latest released version will be useful.

* Run `hack/release.sh v0.2.0` where `v0.2.0`
  is the new version to be released.

* Create the release on the Portal and upload the manifest generated in
  the previous point in `releases/postgresql-operator-0.2.0.yaml`

* Update the official documentation by updating the
  [cnp-docs-packaging](ssh://git@git.2ndquadrant.com/it/ci/packaging/cnp-docs-packaging.git)
  source and creating a new tag named after the version and the packaging version
  (i.e. `v0.2.0-1`). Then, run a new build from
  [Jenkins](https://ci.2ndquadrant.com/jenkins/job/cloud-native-postgresql-docs/job/cloud-native-postgresql-docs/)
  (you can reuse an existing release build task and change the tag name).

* Upload the images on the Quay.io repository

```
skopeo copy docker://internal.2ndq.io/k8s/cloud-native-postgresql:v0.2.0 docker://quay.io/enterprisedb/cloud-native-postgresql:v0.2.0
skopeo copy docker://internal.2ndq.io/k8s/cloud-native-postgresql:v0.2.0 docker://quay.io/enterprisedb/cloud-native-postgresql:latest
```
