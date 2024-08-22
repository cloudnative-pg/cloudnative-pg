# Supported releases

<!-- Inspired by https://github.com/istio/istio.io/blob/933b896c/content/en/docs/releases/supported-releases/index.md -->
<!-- Inspired by https://github.com/cert-manager/website/blob/009c5e41/content/docs/installation/supported-releases.md -->

*This page lists the status, timeline and policy for currently supported
releases of CloudNativePG*.

We are committed to providing support for the latest minor release, with a
dedication to launching a new minor release every two months. Each release
remains fully supported until reaching its designated "End of Life" date, as
outlined in the [support status table for CloudNativePG releases](#support-status-of-cloudnativepg-releases).
This includes an additional 3-month assistance window to facilitate seamless
upgrade planning.

Supported releases of CloudNativePG include releases that are in the active
maintenance window and are patched for security and bug fixes.

Subsequent patch releases on a minor release contain backward-compatible changes only.


* [Support policy](#support-policy)
* [Naming scheme](#naming-scheme)
* [Support status of CloudNativePG releases](#support-status-of-cloudnativepg-releases)
* [What we mean by support](#what-we-mean-by-support)

## Support Policy

CloudNativePG produces new builds for each commit.

Approximately every two months, we create a minor release that undergoes
several additional tests and a thorough release qualification process. We
release patch versions for issues found in supported minor releases.

Before an official release, at least one Release Candidate (RC) is built for
[preview testing](preview_version.md).
Additional release candidates may be issued if new bugs are discovered.
The Release Candidates are announced on the Slack channel to encourage
community testing before the final release.
The maintainers provide 1-2 weeks for community testing, and if no objections
are raised, the final release is announced.

Different types of releases represent varying levels of product quality and
assistance from the CloudNativePG community. For details on the support
provided by the community, see [What we mean by support](#what-we-mean-by-support).

| Type              | Support level                                                                                                         | Quality and recommended Use                                                                                    |
|-------------------|-----------------------------------------------------------------------------------------------------------------------|----------------------------------------------------------------------------------------------------------------|
| Development Build | No support                                                                                                            | Dangerous, might not be fully reliable. Useful to experiment with.                                             |
| Release Candidate | No support                                                                                                            | [Preview version: Not production-ready](preview_version.md). Released for experimentation and testing.         |
| Minor Release     | Support provided until 3 months after the N+1 minor release (ex. 1.23 supported until 3 months after 1.24.0 is released)|
| Patch             | Same as the corresponding minor release                                                                               | Users are encouraged to adopt patch releases as soon as they are available for a given release.                |
| Security Patch    | Same as a patch, however, it doesn't contain any additional code other than the security fix from the previous patch  | Given the nature of security fixes, users are **strongly** encouraged to adopt security patches after release. |

You can find available releases on the [releases page](https://github.com/cloudnative-pg/cloudnative-pg/releases).

You can find high-level more information for each minor and patch release in
the [release notes](release_notes.md).

Sure, here’s an improved version of the naming scheme section:

## Naming Scheme

Our naming scheme follows [Semantic Versioning 2.0.0](https://semver.org/) and
is structured as follows:

```
<major>.<minor>.<patch>
```

- `<minor>` is incremented for each release.
- `<patch>` counts the number of patches for the current `<minor>` release,
  representing small changes relative to the `<minor>` release.

Release candidates are indicated by an additional `-<pre-release>` identifier
following the patch version, as specified in [Semantic Versioning 2.0.0 - item #9](https://semver.org/#spec-item-9).

Git tags for versions are prefixed with `v`.

## Support status of CloudNativePG releases

| Version         | Currently supported  | Release date      | End of life         | Supported Kubernetes versions | Tested, but not supported | Supported Postgres versions |
|-----------------|----------------------|-------------------|---------------------|-------------------------------|---------------------------|-----------------------------|
| 1.24.x          | Yes                  | August 22, 2024   | ~ February, 2025    | 1.28, 1.29, 1.30, 1.31        | 1.27                      | 12 - 16                     |
| 1.23.x          | Yes                  | April 24, 2024    | ~ November, 2024    | 1.27, 1.28, 1.29              | 1.30, 1.31                | 12 - 16                     |
| main            | No, development only |                   |                     |                               |                           | 12 - 16                     |

The list of supported Kubernetes versions in the table depends on what
the CloudNativePG maintainers think is reasonable to support and to test.

At the moment, the CloudNativePG community doesn't support or test any
additional Kubernetes distribution, like Red Hat OpenShift. This might change
in the future and, in that case, that would be reflected in an official policy
written by the CloudNativePG maintainers.

### Supported PostgreSQL versions

The list of supported Postgres versions in the previous table generally depends on
what PostgreSQL versions were supported by the community at the time the minor
version of CloudNativePG was released.

See the PostgreSQL [Versioning Policy](https://www.postgresql.org/support/versioning/)
page for more information about supported versions.

!!! Info
    Starting from November 14, 2024, [Postgres 12 is no longer supported](https://www.postgresql.org/about/news/postgresql-164-158-1413-1316-1220-and-17-beta-3-released-2910/).

We also recommend that you regularly update your PostgreSQL operand images and
use the latest minor release for the major version you have in use, as not upgrading
is riskier than upgrading. As a result, when opening an issue with an older minor
version of PostgreSQL, we might not be able to help you.

## Upcoming releases

| Version         | Release date          | End of life               |
|-----------------|-----------------------|---------------------------|
| 1.25.0          | Nov/Dec, 2024         | May/Jun, 2025             |
| 1.26.0          | Mar, 2025             | Aug/Sep, 2025             |
| 1.27.0          | Jun, 2025             | Dec, 2025                 |

!!! Note
    Feature freeze occurs 1-2 weeks before the release, at which point a
    release candidate version is built and distributed for testing, as described
    earlier.

!!! Important
    Dates in the future are uncertain and might change. This applies to Kubernetes versions, too.
    Updates and changes on the release schedule will be communicated in the
    [Release updates](https://github.com/cloudnative-pg/cloudnative-pg/discussions/categories/release-updates)
    discussion in the main GitHub repository.

## Old releases

| Version         | Release date      | End of life         | Compatible Kubernetes versions |
|-----------------|-------------------|---------------------|--------------------------------|
| 1.22.x          | December 21, 2023 | July 24, 2024       | 1.26, 1.27, 1.28               |
| 1.21.x          | October 12, 2023  | Jun 12, 2024        | 1.25, 1.26, 1.27, 1.28         |
| 1.20.x          | April 27, 2023    | January 21, 2024    | 1.24, 1.25, 1.26, 1.27         |
| 1.19.x          | February 14, 2023 | November 3, 2023    | 1.23, 1.24, 1.25, 1.26         |
| 1.18.x          | Nov 10, 2022      | June 12, 2023       | 1.23, 1.24, 1.25, 1.26, 1.27   |
| 1.17.x          | September 6, 2022 | March 20, 2023      | 1.22, 1.23, 1.24               |
| 1.16.x          | July 7, 2022      | December 21, 2022   | 1.22, 1.23, 1.24               |
| 1.15.x          | April 21, 2022    | October 6, 2022     | 1.21, 1.22, 1.23               |

## What we mean by support

Our support window is roughly five months for each release branch (latest
minor release, plus 3 additional months), given that we produce a new final
release every two months.

In the following diagram, `release-1.23` is an example of a release branch.

For example, if the latest release is `v1.23.0`, you can expect a supplementary
3-month support period for the preceding release, `v1.22.x`.

Only the last patch release of each branch is supported.

```diagram
------+---------------------------------------------> main (trunk development)
       \             \
        \             \
         \             \             v1.23.0
          \             \            Apr 24, 2024                   ^
           \             \----------+---------------> release-1.23  |
            \                                                       | SUPPORTED
             \                                                      | RELEASES
              \   v1.22.0                                           | = last minor
               \  Dec 21, 2023                                      |   release +
                +-------------------+---------------> release-1.22  |   3 months
                                                                    v
```

We offer two types of support:

Technical support
:   Technical assistance is offered on a best-effort basis for supported
    releases only. You can request support from the community on the
    [CloudNativePG Slack](https://cloudnativepg.slack.com/) (in the `#general` channel),
    or using [GitHub Discussions](https://github.com/cloudnative-pg/cloudnative-pg/discussions).

Security and bug fixes
:   We backport important bug fixes — including security fixes - to all
    currently supported releases. Before backporting a patch, we ask ourselves:
    *"Does this backport improve `CloudNativePG`, bearing in mind that we really
    value stability for already-released versions?"*

If you're looking for professional support, see the
[Support page in the website](https://cloudnative-pg.io/support/).
The vendors listed there might provide service level agreements that included
extended support timeframes.
