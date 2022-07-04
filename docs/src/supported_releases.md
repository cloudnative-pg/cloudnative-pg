# Supported releases

<!-- Inspired by https://raw.githubusercontent.com/istio/istio.io/master/content/en/docs/releases/supported-releases/index.md wokeignore:rule=master -->
<!-- Inspired by https://raw.githubusercontent.com/cert-manager/website/master/content/docs/installation/supported-releases.md -->

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

| Version         | Currently Supported  | Release Date      | End of Life              | Supported Kubernetes Versions | Tested, but not supported |
|-----------------|----------------------|-------------------|--------------------------|-------------------------------|---------------------------|
| main            | No, development only |                   |                          |                               |                           |
| 1.15.x          | Yes                  | April 21, 2022    | October 15, 2022         | 1.21, 1.22, 1.23, 1.24        | 1.19, 1.20                |
| 1.16.x          | Yes                  | July 7, 2022      | ~ December 10, 2022      | 1.22, 1.23, 1.24              | 1.19, 1.20, 1.21          |

The list of supported Kubernetes versions in the above table depends on what
the CloudNativePG maintainers think is reasonable to support and to test.

At the moment, the CloudNativePG community does not support any additional
Kubernetes distribution, like Red Hat OpenShift (this might change in the
future, but it will be addressed by an official policy written by the
CloudNativePG maintainers).

## Upcoming release

| Version         | Release Date       | End of Life              | Supported Kubernetes Versions |
|-----------------|--------------------|--------------------------|-------------------------------|
| 1.17.0          | September 15, 2022 | ~ February 10, 2023      | 1.22, 1.23, 1.24              |
| 1.18.0          | November 10, 2022  | 1 month after 1.20       |                               |

!!! Note
    Dates in the future are uncertain and might change. This applies to Kubernetes versions too.

## What we mean by support

Our support window is roughly five months for each release branch (latest two
minor release, plus an additional month), given that we produce a new final
release every two months.

In the below diagram, `release-1.16` is an example of a release branch.

For example, imagining that the latest release is `v1.16.0`, you can expect
support for both `v1.16.0` and `v1.15.0`.

Only the last patch release of each branch is actually supported.

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
    releases only. You can request support from the community on
    [CloudNativePG Slack](https://cloudnativepg.slack.com/) (in the `#general` channel),
    or using [GitHub Discussions][https://github.com/cloudnative-pg/cloudnative-pg/discussions].

Security and bug fixes
:   We back-port important bug fixes — including security fixes - to all
    currently supported releases. Before back-porting a patch, we ask ourselves:
    *"Does this back-port improve `CloudNativePG`, bearing in mind that we really
    value stability for already-released versions?"*


## Professional support

If you are looking for professional support, please refer to the
["Support" page in the website](https://cloudnative-pg.io/support/).
Such vendors might provide service level agreements that include
extended support timeframes.

