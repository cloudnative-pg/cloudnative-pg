# Supported releases

<!-- Inspired by https://github.com/istio/istio.io/blob/933b896c/content/en/docs/releases/supported-releases/index.md -->
<!-- Inspired by https://github.com/cert-manager/website/blob/009c5e41/content/docs/installation/supported-releases.md -->

*This page lists the status, timeline and policy for currently supported
releases of CloudNativePG*.

We support the latest two minor releases, and we aim to create a new minor
release every two months. Each release is supported until the declared
"End of Life" date in the [table below](#support-status-of-cloudnativepg-releases),
which considers an additional month of assistance to allow for upgrade
planning.

Supported releases of CloudNativePG include releases that are in the active
maintenance window and are patched for security and bug fixes.

Subsequent patch releases on a minor release do not contain backward
incompatible changes.

* [Support Policy](#support-policy)
* [Naming scheme](#naming-scheme)
* [Support status of CloudNativePG releases](#support-status-of-cloudnativepg-releases)
* [What we mean by support](#what-we-mean-by-support)

## Support policy

We produce new builds of CloudNativePG for each commit.

Roughly every two months, we build a minor release and run through several
additional tests as well as release qualification. We release patch versions
for issues found in minor releases.

The various types of releases represent a different product quality level and
level of assistance from the CloudNativePG community.
For details on the support provided by the community, please refer to the
["What we mean by support" section](#what-we-mean-by-support) below.

| Type              | Support Level                                                                                                         | Quality and Recommended Use                                                                                    |
|-------------------|-----------------------------------------------------------------------------------------------------------------------|----------------------------------------------------------------------------------------------------------------|
| Development Build | No support                                                                                                            | Dangerous, may not be fully reliable. Useful to experiment with.                                               |
| Minor Release     | Support provided until 1 month after the N+2 minor release (ex. 1.15 supported until 1 month after 1.17.0 is released)|
| Patch             | Same as the corresponding Minor release                                                                               | Users are encouraged to adopt patch releases as soon as they are available for a given release.                |
| Security Patch    | Same as a Patch, however, it will not contain any additional code other than the security fix from the previous patch | Given the nature of security fixes, users are **strongly** encouraged to adopt security patches after release. |

You can find available releases on the [releases page](https://github.com/cloudnative-pg/cloudnative-pg/releases).

You can find high-level more information for each minor and patch release in the [release notes](release_notes.md).

## Naming scheme

Our naming scheme is based on [Semantic Versioning 2.0.0](https://semver.org/)
as follows:

```
<major>.<minor>.<patch>
```

where `<minor>` is increased for each release, and `<patch>` counts the number of patches for the
current `<minor>` release. A patch is usually a small change relative to the `<minor>` release.

Git tags for versions are prepended with `v`.

## Support status of CloudNativePG releases

| Version         | Currently Supported  | Release Date      | End of Life              | Supported Kubernetes Versions | Tested, but not supported | Supported Postgres Versions |
|-----------------|----------------------|-------------------|--------------------------|-------------------------------|---------------------------|-----------------------------|
| 1.20.x          | Yes                  | April 27, 2023    | ~ September 28, 2023     | 1.24, 1.25, 1.26, 1.27        | 1.22, 1.23                | 11 - 15                     |
| 1.19.x          | Yes                  | February 14, 2023 | ~ July 15, 2023          | 1.23, 1.24, 1.25, 1.26        | 1.22, 1.27                | 11 - 15                     |
| 1.18.x          | Yes                  | November 10, 2022 | May 27, 2023             | 1.23, 1.24, 1.25, 1.26        | 1.22, 1.27                | 11 - 15                     |
| main            | No, development only |                   |                          |                               |                           | 11 - 15                     |

The list of supported Kubernetes versions in the above table depends on what
the CloudNativePG maintainers think is reasonable to support and to test.

At the moment, the CloudNativePG community doesn't support nor test any
additional Kubernetes distribution, like Red Hat OpenShift. This might change
in the future and, in that case, that would be reflected in an official policy
written by the CloudNativePG maintainers.

### Supported PostgreSQL versions

The list of supported Postgres versions in the above table generally depends on
what PostgreSQL versions were supported by the Community at the time the minor
version of CloudNativePG was released.

Please refer to the PostgreSQL [Versioning Policy](https://www.postgresql.org/support/versioning/)
page for more information about supported versions.

!!! Info
    Starting by November 10, 2022, Postgres 10 reached its final release and
    is no longer supported.

**We also recommend that you regularly update your PostgreSQL operand images and
use the latest minor release for the major version you have in use**, as not upgrading
is riskier than upgrading. As a result, when opening an issue with an older minor
version of PostgreSQL, we might not be able to help you.

## Upcoming releases

| Version         | Release Date     | End of Life               | Supported Kubernetes Versions |
|-----------------|------------------|---------------------------|-------------------------------|
| 1.21.0          | June 27, 2023    | -                         | -                             |
| 1.22.0          | Aug 29, 2023     | -                         | -                             |

!!! Note
    Feature freeze happens one week before the release

!!! Important
    Dates in the future are uncertain and might change. This applies to Kubernetes versions too.
    Updates and changes on the release schedule will be communicated in the
    ["Release updates"](https://github.com/cloudnative-pg/cloudnative-pg/discussions/categories/release-updates)
    discussion in the main GitHub repository.

## Old releases

| Version         | Release Date      | End of Life              | Compatible Kubernetes Versions |
|-----------------|-------------------|--------------------------|--------------------------------|
| 1.15.x          | April 21, 2022    | October 6, 2022          | 1.21, 1.22, 1.23               |
| 1.16.x          | July 7, 2022      | December 21, 2022        | 1.22, 1.23, 1.24               |
| 1.17.x          | September 6, 2022 | March 20, 2023           | 1.22, 1.23, 1.24               |

## What we mean by support

Our support window is roughly five months for each release branch (latest two
minor releases, plus an additional month), given that we produce a new final
release every two months.

In the below diagram, `release-1.16` is an example of a release branch.

For example, imagining that the latest release is `v1.16.0`, you can expect
support for both `v1.16.0` and `v1.15.0`.

Only the last patch release of each branch is supported.

```diagram
------+---------------------------------------------> main (trunk development)
       \             \
        \             \
         \             \             v1.16.0
          \             \            Jul 7, 2022                    ^
           \             \----------+---------------> release-1.16  |
            \                                                       | SUPPORTED
             \                                                      | RELEASES
              \   v1.15.0                                           | = the two
               \  Apr 21, 2022                                      |   last
                +-------------------+---------------> release-1.15  |   releases
                                                                    v
```

We offer two types of support:

Technical support
:   Technical assistance is offered on a best-effort basis for supported
    releases only. You can request support from the community on the
    [CloudNativePG Slack](https://cloudnativepg.slack.com/) (in the `#general` channel),
    or using [GitHub Discussions](https://github.com/cloudnative-pg/cloudnative-pg/discussions).

Security and bug fixes
:   We back-port important bug fixes — including security fixes - to all
    currently supported releases. Before back-porting a patch, we ask ourselves:
    *"Does this back-port improve `CloudNativePG`, bearing in mind that we really
    value stability for already-released versions?"*

If you are looking for professional support, please refer to the
["Support" page in the website](https://cloudnative-pg.io/support/).
The vendors listed there might provide service level agreements that included
extended support timeframes.
