# Cloud Native PostgreSQL release procedure

## Initial verification

- make sure release notes for the release have been updated
  in `docs/src/release_notes.md` and have been approved by
  the product manager (Adam)
- make sure that the operator capability levels page has been
  updated in `docs/src/operator_capability_levels.md` and approved
  by the product manager (Adam)

**NOTE:** some tasks require interaction and approval with product management.
Please plan for this ahead of the release.

## Release steps

The following steps assume version 1.7.0 as the one to be released. Alter the
instructions accordingly for your version.

1. Run `hack/release.sh 1.7.0`.
1. Approve the PR that is automatically generated.

### What's missing

- Automation of Helm chart release (???)

## Post-release operations

- Inform the `cloud-dev` chat
- Inform the `docs` chat
- Release version in the Portal
- Create a ticket to update the current version of the
  slides for Cloud Native PostgreSQL
- Create a pull request in UPM substrate by changing the version in
  https://github.com/EnterpriseDB/upm-substrate/blob/main/config/azure/base/cnp-operator/kustomization.yaml

## Helm chart release walkthrough:

- copy the output of the following command to `charts/cloud-native-postgresql/templates/crds/crds.yaml` in the cloud-native-postgresql-helm chart: `kustomize build config/helm`
- diff the new release version with the previous one (e.g.: `vimdiff releases/postgresql-operator-1.9.1.yaml releases/postgresql-operator-1.9.2.yaml` using your IDE of choice)
- port any diff to the templates in the helm chart accordingly
- proceed with the release process described in the `RELEASE.md` file in the cloud-native-postgresql-helm repository.

## OpenShift

For OpenShift release, please check the [OCP Certified](https://github.com/EnterpriseDB/cloud-native-postgresql-ocp-certified/blob/main/RELEASE.md#release) repository with the instructions.