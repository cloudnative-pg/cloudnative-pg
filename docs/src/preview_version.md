---
id: preview_version
sidebar_position: 570
title: Preview Versions
---

# Preview Versions
<!-- SPDX-License-Identifier: CC-BY-4.0 -->

CloudNativePG candidate releases are pre-release versions made available for
testing before the community issues a new generally available (GA) release.
These versions are feature-frozen, meaning no new features are added, and are
intended for public testing prior to the final release.

:::warning[Important]
    CloudNativePG release candidates are **not intended for use in production** systems.
    They should only be deployed in staging or dedicated testing environments.
:::

## Purpose of Release Candidates

Release candidates are provided to the community for extensive testing before
the official release. While a release candidate aims to be identical to the
initial release of a new minor version of CloudNativePG, additional changes may
be implemented before the GA release.

### Mitigating Critical Issues

The primary purpose of an RC is to catch serious defects and regressions that our
automated testing and internal manual checks might miss. **Specifically, community testing
is invaluable for validating real-world scenarios, such as:**

- **Upgrade Paths:** Testing the process of upgrading an existing CloudNativePG
  cluster from the previous stable minor version to the RC version. This often
  uncovers critical compatibility or migration issues, like those that affect PVC
  ownership, which can only be reliably tested with diverse existing user setups.
- **Unique Configurations:** Validating compatibility with specific Kubernetes
  versions, storage providers, networking setups, or custom PostgreSQL
  extensions not covered by our standard test matrix.
- **Real-world Workloads:** Running the RC with your actual application
  workload and traffic patterns to expose performance regressions or race
  conditions.

## Community Involvement: How to Help

The stability of each CloudNativePG minor release significantly depends on the
community's efforts to test the upcoming version with their workloads and
tools. Identifying bugs and regressions through user testing is crucial in
determining when we can finalize the release.

### Your Call to Action

If a Release Candidate is available, we encourage you to perform the following tests in a
non-production environment:

- **New Deployment Test:** Deploy a new CloudNativePG cluster using the RC version.
- **Upgrade Test (Crucial):** **Upgrade an existing cluster** (from the
  preceding stable minor version) to the RC version, and verify all key
  operational aspects (scaling, failover, backup/restore) function correctly.
- **Basic Operations:** Verify key features like rolling update,
  backup/restore, instance failover, and connection pooling function as
  expected.
- **Report Issues:** **Immediately report any issues or unexpected behavior**
  by opening a GitHub issue and clearly marking it with a **`Release
  Candidate`** tag or label, along with the RC version number
  (e.g., `1.28.0-rc1`).

## Usage Advisory

The CloudNativePG Community strongly advises against using preview versions of
CloudNativePG in production environments or active development projects. Although
CloudNativePG undergoes extensive automated and manual testing, beta releases
may contain serious bugs. Features in preview versions may change in ways that
are not backwards compatible and could be removed entirely.

**By testing the Release Candidate, you are helping to prevent a potentially
critical bug from affecting the entire community upon the GA release.**

## Current Preview Version

There are currently no preview versions available.

<!--
The current preview version is **1.29.0-rc1**.

For more information on the current preview version and how to test, please view the links below:

- [Announcement](https://cloudnative-pg.io/releases/cloudnative-pg-1-29.0-rc1-released/)
- [Documentation](https://cloudnative-pg.io/documentation/preview/)
-->
