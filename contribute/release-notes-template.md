<!--

PATCH RELEASE (X.Y.Z where Z > 0)
==================================
Prepend a new "## Version X.Y.Z" block to the existing
`docs/src/release_notes/v1.XX.md` file. Do NOT create a new file.

Get the commit range for each supported branch, e.g.:

  LAST_TAG=v1.29.0
  BRANCH=release-1.29
  git log ${LAST_TAG}..origin/${BRANCH} --pretty="format:%h;%s"

Repeat for each supported branch and cross-reference PR numbers to determine
which entries carry backport comments.

NEW MINOR RELEASE — RELEASE CANDIDATE (X.Y.0-rcN)
==================================================
Copy this file to `docs/src/release_notes/v1.XX.md` and remove this comment.
Use a "## Version X.Y.0-rc1" header (full structure: Features, Enhancements,
Security, Fixes, Supported versions).

Also update `docs/src/preview_version.md`: uncomment the block under
"## Current Preview Version" and fill in the RC version, announcement URL
and documentation URL.

NEW MINOR RELEASE — GA (X.Y.0)
================================
Prepend a new "## Version X.Y.0" section above the RC section in the existing
`docs/src/release_notes/v1.XX.md` file. Include only changes landed after the
last RC tag (i.e. `git log v1.XX.0-rcN..origin/main --pretty="format:%h;%s"`).

Also revert `docs/src/preview_version.md`: replace the uncommented preview block
with "There are currently no preview versions available."

ENTRY FORMAT
============
Every entry is two lines: a prose description followed by the PR link indented
two spaces. Append a backport comment listing supported branches that contain
the same commit (identified by matching PR number in each branch's git log):

  - Fixed the XYZ condition that caused spurious failovers.
    ([#10427](https://github.com/cloudnative-pg/cloudnative-pg/pull/10427)) <!-- 1.29 1.28 -->

Multiple PRs for a single logical entry are comma-separated on the same line:

  - Improved ABC by doing XYZ.
    ([#10104](https://github.com/cloudnative-pg/cloudnative-pg/pull/10104),
    [#10298](https://github.com/cloudnative-pg/cloudnative-pg/pull/10298)) <!-- 1.29 1.28 -->

Omit the backport comment if the change is only in the branch being released.
Remove any section below that has no entries.

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
- `cnpg` plugin:
    - Enhanced ...
      ([#NNNNN](https://github.com/cloudnative-pg/cloudnative-pg/pull/NNNNN)) <!-- 1.XX 1.YY -->

### Security

- Addressed CVE-XXXX-XXXXX by ...
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
