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

- Automation of `OpenShift` release
- Automation of Helm chart release (???)

## Post-release operations

- Inform the `cloud-dev` chat
- Inform the `docs` chat
- Release version in the Portal
- Create a ticket to update the current version of the
  slides for Cloud Native PostgreSQL
- Create a pull request in UPM substrate by changing the version in
  https://github.com/EnterpriseDB/upm-substrate/blob/main/config/azure/base/cnp-operator/kustomization.yaml
