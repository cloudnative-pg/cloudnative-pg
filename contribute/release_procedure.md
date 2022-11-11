# Release procedure

This section describes how to release a new set of supported versions of
CloudNativePG, and should be done by one of the maintainers of the project.  It
is a semi-automated process which requires human supervision.

You can only release from a release branch, that is a branch in the
Git repository called `release-X.Y`, i.e. `release-1.16`, which corresponds
to a minor release.

The release procedure must be repeated for all the supported minor releases,
usually 3:

- the current one (`release-X.Y`)
- the previous one (`release-X.Y` -1)
- the *"End of Life"* one (`release-X.Y` -2) - normally for an additional month
  after the first release of the current minor.

```diagram
------+---------------------------------------------> main (trunk development)
       \             \
        \             \
         \             \             LATEST RELEASE
          \             \                                           ^
           \             \----------+---------------> release-X.Y   |
            \                                                       | SUPPORTED
             \                                                      | RELEASES
              \                                                     | = the two
               \                                                    |   last
                +-------------------+---------------> release-X.Y-1 |   releases
                                                                    v
```

## Release branches

A release branch must always originate from the *trunk* (`main` branch),
and corresponds to a new minor release.

Development happens on the trunk (`main` branch), and bug fixes are
cherry-picked in the actively supported release branches by the maintainers.
Sometimes, bug fixes might originate in the release branch as well.
Release notes for patch/security versions are maintained in the release branch
directly.

## Preparing the release

One or two weeks before the release, you should start planning the following
activities:

- **Feature freeze:** Get a clear idea of what tickets are going into the
  release, what tickets we are waiting on (hopefully few), and make sure to
  put the focus on finishing those in time.

- **Supported releases:** Make sure that you update the supported releases' page
  in `docs/src/supported_releases.md` and the changes are approved by the
  maintainers

- **Check on backporting:** Make sure any code that should be backported to the
  various release branches is cherry-picked ahead of time. This will also help
  you compile the release notes.

- **Release notes:** You should create/update the release note documents in
  `docs/src/release_notes/` for each of the versions to release. Remember to
  update `docs/src/release_notes.md`.
  These changes should go in a PR against `main`, and get maintainer approval.

- **Capabilities page:** in case of a new minor release, make sure that the
  operator capability levels page has been updated in
  `docs/src/operator_capability_levels.md` and approved by the maintainers

- **Documentation on website:** Remember that after the release, you will
  need to update the documentation in the
  [website project](https://github.com/cloudnative-pg/cloudnative-pg.github.io)
  for each of the supported releases. (See the section **Documentation on the
  website** below)

<!-- TODO: we should create an issue template with a checklist for the release process -->

## Updating release notes on the branches

Once you are done with the items in the "Preparing the release" section, you
should add the release notes to each of the release branches.

For existing release branches, get the content for the release notes from
`main`, add to the relevant documents, commit and push directly.
Be careful not to "show the future" in this process.
Say you're releasing versions 1.18.0, 1.17.2 and 1.16.4. In the `release-1.17`
release branch, you should update the `v1.16.md` and `v1.17.md` documents, but
NOT create `v1.18.md`. In the `release-1.16` branch, you should update the
`v1.16.md` document but NOT the `v1.17.md` document, nor `v1.18.md`.

**IMPORTANT**. If you're creating a new minor release, the "backporting" of
release notes described in this section should be skipped. Since you already
created the release notes for the new minor in `main`, and you will create the
new release branch off of `main`, the release notes are done for free.

## If creating a new minor release: create a new release branch from main

NOTE: the instructions in the previous sections should have been completed ahead
of this. I.e. all cherry-picks should be done, documents should be up to date,
and release notes should have been merged in `main`.

A new release branch is created starting from the most updated commit in the
trunk by a maintainer:

```bash
git checkout main
git pull --rebase
git checkout -b release-X.Y
git push --set-upstream origin release-X.Y
```

## Release steps

Once the code in the release branch is stable and ready to be released, you can
proceed with the supervised process.

**IMPORTANT:** You need to issue the commands below from each release branch.
If you are releasing a new minor version, you should have created the new
release branch as per the previous section.

As a maintainer, you need to repeat this process for each of the supported
releases of CloudNativePG:

1. Run `hack/release.sh X.Y.Z` (e.g. `hack/release.sh 1.16.0`)
2. Quickly review the PR that is automatically generated by the script and
   approve it
3. Merge the PR, making sure that the commit message title is:
   `Version tag to X.Y.Z`, without prefixes (e.g.: `Version tag to 1.16.0`)
4. Wait until all [GitHub Actions](https://github.com/cloudnative-pg/cloudnative-pg/actions)
   complete successfully.
5. Perform manual smoke tests to verify that installation instructions work on
   your workstation: with a local `kind` cluster up, you should be able to
   install the operator with the instructions from
   ["Installation"](../docs/src/installation_upgrade.md),
   create a multi-instance cluster, verify it becomes
   healthy, and once healthy, you can execute `psql` in the primary and interact
   with with the database.
6. In case of a new **minor** release, merge the new release commit on `main`
   with `git merge --ff-only release-X.Y` followed by `git push`

## Documentation on the website

The documentation, including the release notes, is created in the current `cloudnative-pg` repository, but in order to publish it in the
[CloudNativePG public webpage](https://cloudnative-pg.io), it needs to be ported
to the [`cloudnative-pg.github.io`](https://github.com/cloudnative-pg/cloudnative-pg.github.io)
repository.

The [`README`](https://github.com/cloudnative-pg/cloudnative-pg.github.io#readme)
in that repository has complete instructions on the deployment of documentation,
for new minor releases as well as patch releases.

Please follow the instructions, and once done, also think of creating a blog
post announcing the new releases that can be shared in various channels.

## Helm chart release

After having created a new release of CloudNativePG you need to create a release
of the `cloudnative-pg` and `cnpg-sandbox` charts, whose definitions can be
found in the [cloudnative-pg/charts](https://github.com/cloudnative-pg/charts)
repository.

The following is a rough outline of the steps to be taken in that direction. The
[RELEASE.md](https://github.com/cloudnative-pg/charts/blob/main/RELEASE.md)
document inside the `charts` repo contains an in-depth discussion of the
process, please refer to it.

1. Copy the output of `kustomize build config/helm` to `charts/cloudnative-pg/templates/crds/crds.yaml`
   in the `charts` repository (keeping the template guards).
2. Diff the new release version with the previous one
   (e.g.: `vimdiff releases/cnpg-1.17.0.yaml releases/cnpg-1.17.1.yaml`)
3. Port any difference found in the previous step to the items in the
   `templates` folder, in the helm chart.
4. Proceed with the release process as described in the `RELEASE.md`
   file in the `charts` repository.
