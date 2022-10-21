# Container Image Requirements

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
- PGAudit extension installed (optional - only if PGAudit is required
  in the deployed clusters)
- Appropriate locale settings

!!! Important
    Only [PostgreSQL versions supported by the PGDG](https://postgresql.org/) are allowed.

No entry point and/or command is required in the image definition, as
CloudNativePG overrides it with its instance manager.

!!! Warning
    Application Container Images will be used by CloudNativePG
    in a **Primary with multiple/optional Hot Standby Servers Architecture**
    only.

The CloudNativePG community provides and supports
[public PostgreSQL container images](https://github.com/cloudnative-pg/postgres-containers)
that work with CloudNativePG, and publishes them on
[ghcr.io](https://ghcr.io/cloudnative-pg/postgresql).

## Image tag requirements

While the image name can be anything valid for Docker, the CloudNativePG
operator relies on the *image tag* to detect the Postgres major
version carried out by the image.

The image tag must start with a valid PostgreSQL major version number (e.g.
14.5 or 15) optionally followed by a dot and the patch level.

The prefix can be followed by any valid character combination that is valid and
accepted in a Docker tag, preceded by a dot, an underscore, or a minus sign.

Examples of accepted image tags:

- `11.1`
- `12.3.2.1-1`
- `12.4`
- `13`
- `14.5-10`
- `15.0`

!!! Warning
    `latest` is not considered a valid tag for the image.
