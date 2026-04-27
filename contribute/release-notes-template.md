<!--

PATCH RELEASE (X.Y.Z where Z > 0)
==================================
Prepend a new "## Version X.Y.Z" block to the existing
`docs/src/release_notes/v1.XX.md` file. Do NOT create a new file.

Get the commit range for the branch being released:

  LAST_TAG=v1.29.0
  BRANCH=release-1.29
  git log ${LAST_TAG}..origin/${BRANCH} --pretty="format:%h;%s"

For each PR you mention, find the supported branches that also carry it
(same PR number in their git log) to build the backport comment. The helper
script does the lookup across `main` and every `release-*` branch:

  ./hack/check-pr-in-release-branches.sh NNNNN

NEW MINOR RELEASE — RELEASE CANDIDATE (X.Y.0-rcN)
==================================================
First candidate (rc1): copy this file to `docs/src/release_notes/v1.XX.md`
and remove this comment. Use a "## Version X.Y.0-rc1" header.

Subsequent candidates (rc2, rc3, ...): prepend a "## Version X.Y.0-rcN"
section above the previous RC in the existing file. Include only changes
landed since the previous RC tag:

  git log v1.XX.0-rc<N-1>..origin/main --pretty="format:%h;%s"

Also update `docs/src/preview_version.md`: uncomment the block under
"## Current Preview Version" and fill in the RC version, announcement URL
and documentation URL.

NEW MINOR RELEASE — GA (X.Y.0)
================================
Prepend a new "## Version X.Y.0" section above the most recent RC section
in `docs/src/release_notes/v1.XX.md`. Include only changes landed after the
last RC tag:

  git log v1.XX.0-rcN..origin/main --pretty="format:%h;%s"

Also revert `docs/src/preview_version.md`: replace the uncommented preview
block with "There are currently no preview versions available."

ENTRY FORMAT
============
Every entry is two lines: a prose description, then the PR link indented two
spaces. Append a backport comment listing the supported branches that
received the same PR via backport (typically excluding the branch being
released):

  - Fixed the XYZ condition that caused spurious failovers.
    ([#10427](https://github.com/cloudnative-pg/cloudnative-pg/pull/10427)) <!-- 1.29 1.28 -->

External contributions get a "Contributed by" line just before the PR link:

  - Improved ABC by doing XYZ.
    Contributed by @octocat.
    ([#10427](https://github.com/cloudnative-pg/cloudnative-pg/pull/10427)) <!-- 1.29 1.28 -->

When a single logical entry covers multiple PRs, attach a backport comment
to EACH PR line (their backport sets may differ):

  - Improved ABC by doing XYZ.
    ([#10054](https://github.com/cloudnative-pg/cloudnative-pg/pull/10054), <!-- 1.28 1.27 1.25 -->
    [#10062](https://github.com/cloudnative-pg/cloudnative-pg/pull/10062)) <!-- 1.28 1.27 1.25 -->

Headlined entries (everything in Features, plus prominent Security and
Supply Chain items) lead with a bolded short name:

  - **PostgreSQL extensions in image catalogs**: extended ImageCatalog ...
    ([#9781](https://github.com/cloudnative-pg/cloudnative-pg/pull/9781))

`cnpg` plugin items go in their own subsection inside Enhancements and/or
Fixes, with a blank line between the header bullet and its children:

  - `cnpg` plugin:

      - Enhanced ...
        ([#NNNNN](https://github.com/cloudnative-pg/cloudnative-pg/pull/NNNNN)) <!-- 1.XX 1.YY -->

Omit the backport comment if the change is only in the branch being
released. Remove any section below that has no entries.

ORDERING
========
Lead each section with the highest-impact entries (deprecations, behaviour
changes, broadly visible improvements) so a reader scanning only the first
few bullets gets the gist of the release.

"IMPORTANT CHANGES" vs "CHANGES"
=================================
- "Important changes" = users must act on upgrade (deprecations, breaking
  behaviour changes, removals).
- "Changes" = neutral updates worth surfacing but requiring no action
  (default image bumps, dependency updates that affect behaviour).
Both can coexist in the same release.

SUPPORTED VERSIONS
==================
Source the values for the "Supported versions" section from:
  - Kubernetes range: the tested matrix in the e2e workflow / cluster setup,
    kept in sync with the Kubernetes support policy.
  - PostgreSQL majors: the supported set declared in the operator. Note the
    EOL date of the oldest supported major.
  - Default PostgreSQL image: the tag set by the most recent default-image
    bump on the branch (`git log --grep "default PostgreSQL"`).

-->

---
id: v1.XX
title: Release notes for CloudNativePG 1.XX
---

# Release notes for CloudNativePG 1.XX

History of user-visible changes in the 1.XX minor release of CloudNativePG.

For a complete list of changes, please refer to the
[commits](https://github.com/cloudnative-pg/cloudnative-pg/commits/release-1.XX)
on the release branch in GitHub.

<!-- ============================================================ -->
<!-- PATCH RELEASE TEMPLATE                                       -->
<!-- ============================================================ -->

## Version 1.XX.Z

**Release date:** Mon DD, 20YY

### Important changes

- Description of a deprecation notice, behaviour change, or anything users
  must act on when upgrading.
  ([#NNNNN](https://github.com/cloudnative-pg/cloudnative-pg/pull/NNNNN)) <!-- 1.XX 1.YY -->

### Enhancements

- Improved ...
  ([#NNNNN](https://github.com/cloudnative-pg/cloudnative-pg/pull/NNNNN)) <!-- 1.XX 1.YY -->

### Security and Supply Chain

- **Headlined item**: description of the security or supply-chain change.
  ([#NNNNN](https://github.com/cloudnative-pg/cloudnative-pg/pull/NNNNN)) <!-- 1.XX 1.YY -->

### Changes

- Updated the default PostgreSQL version to ...
  ([#NNNNN](https://github.com/cloudnative-pg/cloudnative-pg/pull/NNNNN)) <!-- 1.XX 1.YY -->

### Fixes

- Fixed ...
  ([#NNNNN](https://github.com/cloudnative-pg/cloudnative-pg/pull/NNNNN)) <!-- 1.XX 1.YY -->

<!-- ============================================================ -->
<!-- MINOR RELEASE TEMPLATE (X.Y.0 or RC)                        -->
<!-- ============================================================ -->

## Version 1.XX.0

**Release date:** Mon DD, 20YY

### Important changes

- Description of a deprecation notice, behaviour change, or anything users
  must act on when upgrading.
  ([#NNNNN](https://github.com/cloudnative-pg/cloudnative-pg/pull/NNNNN)) <!-- 1.XX 1.YY -->

### Features

- **Feature name**: description of the feature and why it matters.
  ([#NNNNN](https://github.com/cloudnative-pg/cloudnative-pg/pull/NNNNN))

### Enhancements

- Improved ...
  ([#NNNNN](https://github.com/cloudnative-pg/cloudnative-pg/pull/NNNNN)) <!-- 1.XX 1.YY -->

- Improved ...
  Contributed by @octocat.
  ([#NNNNN](https://github.com/cloudnative-pg/cloudnative-pg/pull/NNNNN)) <!-- 1.XX 1.YY -->

- `cnpg` plugin:

    - Enhanced ...
      ([#NNNNN](https://github.com/cloudnative-pg/cloudnative-pg/pull/NNNNN)) <!-- 1.XX 1.YY -->

### Security and Supply Chain

- **Headlined item**: description of the security or supply-chain change
  (CVE-XXXX-XXXXX, SLSA provenance, SBOM, scanner integration, ...).
  ([#NNNNN](https://github.com/cloudnative-pg/cloudnative-pg/pull/NNNNN)) <!-- 1.XX 1.YY -->

### Changes

- Updated the default PostgreSQL version to ...
  ([#NNNNN](https://github.com/cloudnative-pg/cloudnative-pg/pull/NNNNN)) <!-- 1.XX 1.YY -->

### Fixes

- Fixed ...
  ([#NNNNN](https://github.com/cloudnative-pg/cloudnative-pg/pull/NNNNN)) <!-- 1.XX 1.YY -->

- `cnpg` plugin:

    - Fixed ...
      ([#NNNNN](https://github.com/cloudnative-pg/cloudnative-pg/pull/NNNNN)) <!-- 1.XX 1.YY -->

### Supported versions

- Kubernetes 1.35, 1.34, and 1.33
- PostgreSQL 18, 17, 16, 15, and 14
    - PostgreSQL 18.3 is the default image
    - PostgreSQL 14 support ends on November 11, 2026
