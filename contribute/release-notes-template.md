<!--

Copy this file inside `docs/src/release_notes/v1.XX.md`, making
sure you remove this comment.

Create a spreadsheet with the list of commits since the last minor release:

Use the last known tag on `main` branch as a start (e.g. LAST_TAG=v1.25.1).

```bash
LAST_TAG=v1.25.1
git checkout main
git log ${LAST_TAG}.. --oneline --pretty="format:%h;%s" > log.csv
```
-->
# Release notes for CloudNativePG 1.XX

History of user-visible changes in the 1.XX minor release of CloudNativePG.

For a complete list of changes, please refer to the
[commits](https://github.com/cloudnative-pg/cloudnative-pg/commits/release-1.XXX
on the release branch in GitHub.

## Version 1.XX.0-rc1

**Release date:** Mon DD, 20YY

<!--
If individual PRs are mentioned, make sure to include the contributor's GitHub handle alongside it.
-->

### Important changes:

- OPTIONAL
- OPTIONAL

### Features:

- **MAIN FEATURE #1**: short description
- **MAIN FEATURE #2**: short description

### Enhancements:

- Add ...
- Introduce ...
- Allow ...
- Enhance ...
- `cnpg` plugin updates:
    - Enhance ...
    - Add ...

### Security:

- Add ...
- Improve ...

### Fixes:

- Enhance ...
- Disable ...
- Gracefully handle ...
- Wait ...
- Fix ...
- Address ...
- `cnpg` plugin:
    - ...
    - ...

### Supported versions

- Kubernetes 1.31, 1.30, and 1.29
- PostgreSQL 17, 16, 15, 14, and 13
    - PostgreSQL 17.X is the default image
    - PostgreSQL 13 support ends on November 12, 2025

### New Contributors

- @github-user-handle made their first contribution in #123
