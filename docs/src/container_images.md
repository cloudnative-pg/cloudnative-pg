# Container image requirements

The CloudNativePG operator for Kubernetes is designed to
work with any compatible container image of PostgreSQL that complies
with the following requirements:

- PostgreSQL executables that must be in the path:
    - `initdb`
    - `postgres`
    - `pg_ctl`
    - `pg_controldata`
    - `pg_basebackup`
- Barman Cloud executables that must be in the path:
    - `barman-cloud-backup`
    - `barman-cloud-backup-delete`
    - `barman-cloud-backup-list`
    - `barman-cloud-check-wal-archive`
    - `barman-cloud-restore`
    - `barman-cloud-wal-archive`
    - `barman-cloud-wal-restore`
- If PGAudit is required in the deployed clusters, PGAudit extension installed 
- Appropriate locale settings

!!! Important
    Only [PostgreSQL versions supported by the PostgreSQL Global Development Group (PGDG)](https://postgresql.org/) are allowed.

No entry point or command is required in the image definition, as
CloudNativePG overrides it with its instance manager.

!!! Warning
    Application container images are used by CloudNativePG
    only in a "primary with multiple/optional hot standby servers" architecture.

The CloudNativePG community provides and supports
[public PostgreSQL container images](https://github.com/cloudnative-pg/postgres-containers)
that work with CloudNativePG and publishes them on
[`ghcr.io`](https://ghcr.io/cloudnative-pg/postgresql).

## Image tag requirements

While the image name can be anything valid for Docker, the CloudNativePG
operator relies on the *image tag* to detect the Postgres major
version contained in the image.

The image tag must start with a valid PostgreSQL major version number (for example, 
14.5 or 15) optionally followed by a dot and the patch level.

The version number can be followed by a dot, an underscore, or a minus sign and any character combination that's valid and accepted in a Docker tag.

Examples of accepted image tags:

- `11.1`
- `12.3.2.1-1`
- `12.4`
- `13`
- `14.5-10`
- `15.0`

!!! Warning
    `latest` isn't a valid tag for the image.
