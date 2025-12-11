# Release procedure

This section describes how to release a new set of supported versions of
CloudNativePG, which should be done by one of the project's maintainers.
It is a semi-automated process that requires human supervision.

You can only release stable versions from a release branch, that is a branch
in the Git repository called `release-X.Y`, e.g., `release-1.28`, which
corresponds to a minor release.

The release procedure must be repeated for all the supported minor releases.

```diagram
------+---------------------------------------------> main (trunk development)
       \             \
        \             \
         \             \             LATEST RELEASE
          \             \                                           ^
           \             \----------+---------------> release-X.Y   |
            \                                                       | SUPPORTED
             \                                                      | RELEASES
              \                                                     | = the minor
               \                                                    |   release +
                +-------------------+---------------> release-X.Y-1 |   3 months
                                                                    v
```

## Release branches

A release branch must always originate from the *trunk* (`main` branch),
and corresponds to a new minor release.

Development happens on the trunk (`main` branch), and the maintainers cherry-pick
bug fixes in the actively supported release branches.
Sometimes, bug fixes might originate in the release branch as well.
Release notes for patch/security versions are directly maintained in the release
branches.

## Preparing the release

One or two weeks before the release, you should start planning the following
activities:

- **Feature freeze:** Get a clear idea of what tickets are going into the
  release and what tickets we are waiting on (hopefully a few), and make sure to
  put the focus on finishing those in time. No new features should land in the
  last few days without previous validation from the team.

- **Supported releases:** Make sure that you update the supported releases page
  in [`docs/src/supported_releases.md`](../docs/src/supported_releases.md),
  and that the maintainers approve the changes.

- **Check on backporting:** Make sure to cherry-pick any code that requires
  backporting to the various release branches ahead of time. Doing that will
  also help you to compile the release notes. Note that
  backporting/cherry-picking should be done as soon as possible. Delaying
  backporting increases the risk of conflicts due to code drift.

- **CI/CD health for branches:** the release branches should have no or few
  failures. Make sure to run full E2E tests on each branch. The nightly smoke
  test on the branches only covers a few combinations.

- **Release notes:** You should create/update the release note documents in
  `docs/src/release_notes/` for each version to release. Remember to
  update [`docs/src/release_notes.md`](../docs/src/release_notes.md)
  and [`.github/ISSUE_TEMPLATE/bug.yml`](../.github/ISSUE_TEMPLATE/bug.yml).
  These changes should go in a PR against `main`, and get maintainer approval.
  Look at the template file to get an idea of how to start a new minor release
  version document. As a token of appreciation for the Community’s efforts, make sure to
  include the contributor’s GitHub handle alongside their PRs, if mentioned.
  First-time contributors should also be highlighted in the dedicated "New Contributors" section.

- **Capabilities page:** in case of a new minor release, ensure that the
  operator capability levels page in
  [`docs/src/operator_capability_levels.md`](../docs/src/operator_capability_levels.md)
  is up-to-date and approved by the maintainers. This also applies to the
  [`docs/src/index.md`](../docs/src/index.md) page, and the OLM CSV file
  [`config/olm-manifests/bases/cloudnative-pg.clusterserviceversion.yaml`](config/olm-manifests/bases/cloudnative-pg.clusterserviceversion.yaml).

- **Documentation on website:** Remember that after the release, you will
  need to update the documentation in the
  [website project](https://github.com/cloudnative-pg/cloudnative-pg.github.io)
  for each of the supported releases. (See the section **Documentation on the
  website** below)

<!-- TODO: we should create an issue template with a checklist for the release process -->

## Updating release notes on the branches

**IMPORTANT:** If you're creating a new minor release, the "backporting" of
release notes described in this section should be skipped *for the new minor*.
Since you already  created the release notes for the new minor in `main`, and
you will create the  new release branch off of `main`, the release notes are
done for free.

Once you are done with the items in the "Preparing the release" section, and you
have committed the release notes on `main`, you
should add the release notes to each of the release branches.
The easiest way is to use cherry-picking, but first note the following caveat:

Be careful not to "show the future" in the release branches.
Say you're releasing versions 1.18.0, 1.17.2, and 1.16.4. In the `release-1.17`
release branch, you should update the `v1.16.md` and `v1.17.md` documents, but
**NOT** create `v1.18.md`. In the `release-1.16` branch, you should update the
`v1.16.md` document, but **NOT** the `v1.17.md` document, nor `v1.18.md`.

So, say you're in branch `release-1.18`. You can `git cherry-pick` the commit
from `main` that has the release notes.
Take care to:

1. Update the commit title so you don't show the future. I.e drop more recent
  releases from the `doc: release notes for X.Y.Z, ...`
2. `git rm` the files for the more recent releases. You should already have them
  as a conflict when you do the cherry pick.
3. Continue with the cherry pick once conflicts are solved. Commit and push.

**IMPORTANT:** When moving a release into EOL, we are moving its release
notes file from `docs/src/release_notes/` into `docs/src/release_notes/old/`.
This should NOT be done in the release branch that we are EOL'ing. \
E.g. when we are EOL'ing release 1.17, we should move the file `v1.17.md` into
the `old/` subdirectory in release branches `release-1.18` and `release-1.19`,
but in `release-1.17`, the file should stay in `release_notes/`.

## If creating a new minor release: create a new release branch from main

**IMPORTANT:** The instructions in the previous sections should have been completed ahead
of this. I.e., all cherry-picks should be done, documents should be up-to-date,
and the release notes should have been merged in `main`.

A new release branch is created starting from the most updated commit in the
trunk by a maintainer:

```bash
git checkout main
git pull --rebase
git checkout -b release-X.Y
git push --set-upstream origin release-X.Y
```

This procedure must happen immediately before starting the release.

**IMPORTANT:** Now we add support for the automatic backporting of merged pull requests from main to the new release branch.
Once the new release branch is created, go back to `main` and submit a pull
request to update the
[backport](https://github.com/cloudnative-pg/cloudnative-pg/blob/main/.github/workflows/backport.yml),
[continuous delivery](https://github.com/cloudnative-pg/cloudnative-pg/blob/main/.github/workflows/continuous-delivery.yml),
[continuous integration](https://github.com/cloudnative-pg/cloudnative-pg/blob/main/.github/workflows/continuous-integration.yml)
and [Renovate](https://github.com/cloudnative-pg/cloudnative-pg/blob/main/.github/renovate.json5)
workflows to support the new release branch.
And also remember to update the [github issue template](https://github.com/cloudnative-pg/cloudnative-pg/blob/main/.github/ISSUE_TEMPLATE/bug.yml).

## Release steps

Once the code in the release branch is stable and ready to be released, you can
proceed with the supervised process.

**IMPORTANT:** If you are creating a new minor release, those steps should immediately
follow the creation of the new release branch from main, described in the previous section.
I.e., the just created release branch must have the same commit hash as main.

**IMPORTANT:** You must issue the commands below from each release branch.
If you are releasing a new minor version, you should have created the new
release branch as per the previous section.

**IMPORTANT:** You should follow the steps below in order of ascending release,
i.e. you should start with the least advanced release, and end with the most
advanced. In fact, you should not even trigger the most advanced release until
the other ones have been published. This ensures the `latest` tag in the image
repos will point to the right version.

As a maintainer, you need to repeat this process for each of the supported
releases of CloudNativePG:

1. Run `hack/release.sh X.Y.Z` (e.g., `hack/release.sh 1.16.0`)
2. Quickly review the PR that is automatically generated by the script and
   approve it
3. Merge the PR, making sure that the commit message title is:
   `Version tag to X.Y.Z`, without prefixes (e.g.: `Version tag to 1.16.0`)
   **IMPORTANT**, if you make commits to the PR, the merge message
   proposed by GH will be different. Make sure the title is
   `Version tag to X.Y.Z`, otherwise some of the automation won't work.
4. Wait until all [GitHub Actions](https://github.com/cloudnative-pg/cloudnative-pg/actions)
   complete successfully.
5. Perform manual smoke tests to verify that installation instructions work on
   your workstation: with a local `kind` cluster up, you should be able to
   install the operator with the instructions from
   ["Installation"](../docs/src/installation_upgrade.md),
   create a multi-instance cluster, verify it becomes
   healthy, and once healthy, you can execute `psql` in the primary and interact
   with the database.
6. If the current release is the latest **minor** release and the branch is new
   (the patch version is 0), merge the new release commit on `main`
   with `git merge --ff-only release-X.Y` followed by `git push`.
   Say you're releasing 1.18.0 and the latest minor release branch is
   release-1.18; merge the release commit from that branch on main.
   **NOTE:** this needs to be done by a maintainer.
7. If the current release is the latest **minor** release, but the branch is not new
   (the patch version is 1 or higher), cherry-pick the new release commit on `main`
   with `git cherry-pick release-X.Y` followed by `git push`.
   Say you're releasing 1.18.1, 1.17.2, and 1.16.4; the latest minor release branch
   is release-1.18; cherry-pick the release commit from that branch on main.
   **NOTE:** this needs to be done by a maintainer.

## Documentation on the website

The documentation, including the release notes, is created in the `cloudnative-pg`
repository but published in the [CloudNativePG public webpage](https://cloudnative-pg.io)
through the [`cloudnative-pg.github.io`](https://github.com/cloudnative-pg/cloudnative-pg.github.io)
repository.

The [`README`](https://github.com/cloudnative-pg/cloudnative-pg.github.io#readme)
in that repository has complete instructions on the deployment of documentation
for new minor releases as well as patch releases.

Please follow the instructions, and once done, also think of creating a blog
post announcing the new releases that can be shared in various channels.

## Helm chart release

After creating a new release of CloudNativePG, you need to release
the `cloudnative-pg` chart, which  resides in the
[cloudnative-pg/charts](https://github.com/cloudnative-pg/charts) repository.

The following is a rough outline of the steps to be taken in that direction. The
[RELEASE.md](https://github.com/cloudnative-pg/charts/blob/main/RELEASE.md)
document inside the `charts` repo contains an in-depth discussion of the
process, please refer to it.

1. Copy the output of `kustomize build config/helm` to `charts/cloudnative-pg/templates/crds/crds.yaml`
   in the `charts` repository (keeping the template guards).
2. Diff the new release version from the previous one
   (e.g.: `vimdiff releases/cnpg-1.17.0.yaml releases/cnpg-1.17.1.yaml`)
3. Port any difference found in the previous step to the items in the
   `templates` folder in the helm chart.
4. Proceed with the release process as described in the `RELEASE.md`
   file in the `charts` repository.

## Versions in the bug issue template

Once the release is completed, make sure that both the CloudNativePG versions
and the Kubernetes ones are aligned with the supported release page.

Open the `.github/ISSUE_TEMPLATES/bug.yml` file and update it accordingly.

## Release candidate

It's possible to create a release candidate (RC) for a new upcoming
minor release.
Unlike stable releases, a release candidate will be released just for one
version, as such the release process doesn't have to be repeated for all
the supported release branches.

**IMPORTANT:** Release candidates should always be released from the
`main` branch. A release branch for a new minor should only be created
once we are releasing the first stable version. This is necessary to
ensure the newly created release branch is a descendant of the `main`
branch.

To release a RC you can follow the [Release steps](#release-steps) until
point 5, taking care to use a valid semantic version when running the first
step (e.g., `hack/release.sh 1.16.0-rc1`).
See [Semantic Versioning 2.0.0 - item 9](https://semver.org/#spec-item-9) to
check for valid release candidate identifiers.

**NOTE:** Release candidates can only be installed via the YAML manifest,
other installation methods such as Helm Chart or OLM are currently not supported.
