<!--

Copy this file inside `docs/src/release_notes/v1.XX.md`, making
sure you remove this comment.

Create a spreadsheet with the list of commits since the last minor release:

Use the last known tag on `main` branch as a start (e.g. LAST_TAG=v1.28.1).

```bash
LAST_TAG=v1.28.1
git checkout main
git log ${LAST_TAG}.. --oneline --pretty="format:%h;%s" > log.csv
```
-->
---
id: v1.XX
title: Release notes for CloudNativePG 1.XX
---

# Release notes for CloudNativePG 1.XX

History of user-visible changes in the 1.XX minor release of CloudNativePG.

For a complete list of changes, please refer to the
[commits](https://github.com/cloudnative-pg/cloudnative-pg/commits/release-1.XXX
on the release branch in GitHub.

## Version 1.XX.0-rc1

**Release date:** Mon DD, 20YY

### Important changes:

- OPTIONAL
- OPTIONAL

### Features:

- **MAIN FEATURE #1**: short description
- **MAIN FEATURE #2**: short description

### Enhancements:

- Added ...
- Introduced ...
- Allowed ...
- Enhanced ...
- `cnpg` plugin updates:
    - Enhanced ...
    - Added ...

### Security:

- Added ...
- Improved ...

### Fixes:

- Enhanced ...
- Disabled ...
- Gracefully handled ...
- Fixed ...
- Addressed ...
- `cnpg` plugin:
    - ...
    - ...

### Supported versions

- Kubernetes 1.35, 1.34, and 1.33
- PostgreSQL 18, 17, 16, 15, and 14
    - PostgreSQL 18.3 is the default image
    - PostgreSQL 14 support ends on November 11, 2026
