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

## Prerequisites

- make sure your environment is configured to run
  [GoReleaser](https://goreleaser.com/environment/)
  with API tokens before starting the release process. You need to authorize
  the tokens for the EnterpriseDB organization on GitHub.

## Release steps

The following steps assume version 1.7.0 as the one to be released. Alter the
instructions accordingly for your version.

1. Run `hack/release.sh 1.7.0`.
1. Approve the PR that is automatically generated.
1. Create the tag with release notes on [kubectl-cnp](https://github.com/EnterpriseDB/kubectl-cnp).
   From the repo [releases](https://github.com/EnterpriseDB/kubectl-cnp/releases),
   click on [Draft a new release](https://github.com/EnterpriseDB/kubectl-cnp/releases/new)
   and fill the fields:
   1. Tag version: v1.7.0
   1. Target: main
   1. Title: Release 1.7.0
   1. Cut-and-paste release notes as description
1. Run `git fetch -Pp --force`
1. Run `git checkout main`
1. As a clean repo is needed, please notice that everything in the repo will be
   erased.
   1. Run `git reset origin/main --hard`
   1. Run `git clean -fd`
1. Run `goreleaser release --rm-dist`

### What's missing

- Automation of `kubectl cnp` release
- Automation of `OpenShift` release
- Automation of Helm chart release (???)

## Post-release operations

- Inform the `cloud-dev` chat
- Release version in the Portal
- Create a ticket to update the current version of the
  slides for Cloud Native PostgreSQL
