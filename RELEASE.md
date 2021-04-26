# Cloud Native PostgreSQL release procedure

**Prerequisites:**

- make sure release notes for the release have been updated
  in `docs/src/release_notes.md`
- make sure that the operator capability levels page has been
  updated in `docs/src/operator_capability_levels.md`
- make sure your environment is configured to run
  [GoReleaser](https://goreleaser.com/environment/)
  with API tokens before starting the release process. You need to authorize
  the tokens for the EnterpriseDB organization on GitHub.

**Steps:**

The following steps assume version 1.3.0 as the one to be released. Alter the
instructions accordingly.

1. Run `hack/release.sh 1.3.0` and follow the instructions
2. Create pull request for branch `release/v1.3.0`
3. Have the pull request approved and merged
4. Create the tag with release notes.
   From the repo [releases](https://github.com/EnterpriseDB/cloud-native-postgresql/releases),
   click on [Draft a new release](https://github.com/EnterpriseDB/cloud-native-postgresql/releases/new)
   and fill the fields:
   - Tag version: v1.3.0
   - Target: main
   - Title: Release 1.3.0
   - Cut-and-paste release notes as description
5. Repeat the previous step on [kubectl-cnp](https://github.com/EnterpriseDB/kubectl-cnp)
   project
6. Run `git fetch -Pp --force`
7. Run `git checkout main`
8. As a clean repo is needed, please notice that everything in the repo will be
   erased.
   - Run `git reset origin/main --hard`
   - Run `git clean -fd`
9. Run `goreleaser release --rm-dist`

**Post-release operations:**

- Inform the `cloud-dev` chat
- Release version in the Portal
- Create a ticket to update the current version of the
  slides for Cloud Native PostgreSQL
