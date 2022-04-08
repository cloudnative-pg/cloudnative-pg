# Supported releases

<!-- Inspired by https://raw.githubusercontent.com/istio/istio.io/master/content/en/docs/releases/supported-releases/index.md -->

This page lists the status, timeline and policy for currently supported releases of CloudNativePG.

Supported releases of CloudNativePG include releases that are in the active
maintenance window and are patched for security and bug fixes.

Subsequent patch releases on a minor release do not contain backward
incompatible changes.

* [Support Policy](#support-policy)
* [Naming scheme](#naming-scheme)
* [Support status of CloudNativePG releases](#support-status-of-cloudnativepg-releases)

## Support policy

We produce new builds of CloudNativePG for each commit.

Roughly every two months, we build a minor release and run through several
additional tests as well as release qualification. We release patch versions
for issues found in minor releases.

The various types of releases represent a different product quality level and
level of assistance from the CloudNativePG community.

In this context, *support* means that the community will produce patch releases
for critical issues and offer technical assistance. Separately, 3rd parties and
partners may offer longer-term support solutions.

| Type              | Support Level                                                                                                         | Quality and Recommended Use                                                                                    |
|-------------------|-----------------------------------------------------------------------------------------------------------------------|----------------------------------------------------------------------------------------------------------------|
| Development Build | No support                                                                                                            | Dangerous, may not be fully reliable. Useful to experiment with.                                               |
| Minor Release     | Support provided until 1 month after the N+2 minor release (ex. 1.11 supported until 1 month after 1.13.0 is released)|
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
| 1.15.X          | Yes                  | April DD, 2022    | September DD, 2022       | 1.21, 1.22, 1.23              | 1.19, 1.20                |

The list of supported Kubernetes versions in the above table depends on what
the CloudNativePG maintainers think is reasonable to support and to test.

<!--
## Upcoming release

| Version         | Release Date      | End of Life              | Supported Kubernetes Versions |
|-----------------|-------------------|--------------------------|-------------------------------|
| 1.15.X          | April DD, 2022    | September DD, 2022       | 1.21, 1.22, 1.23              |
| 1.16.X          | June DD, 2022     | November DD, 2022        | 1.21, 1.22, 1.23              |
| 1.17.X          | August DD, 2022   | January DD, 2022         | 1.21, 1.22, 1.23              |

!!! Note
    Dates in the future are uncertain and might change.
-->
