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

## Image Tag Requirements

To ensure the operator makes informed decisions, it must accurately detect the
PostgreSQL major version. This detection can occur in two ways:

1. Utilizing the `major` field of the `imageCatalogRef`, if defined.
2. Auto-detecting the major version from the image tag of the `imageName` if
   not explicitly specified.

For auto-detection to work, the image tag must adhere to a specific format. It
should commence with a valid PostgreSQL major version number (e.g., 15.6 or
16), optionally followed by a dot and the patch level.

Following this, the tag can include any character combination valid and
accepted in a Docker tag, preceded by a dot, an underscore, or a minus sign.

Examples of accepted image tags:

- `12.1`
- `13.3.2.1-1`
- `13.4`
- `14`
- `15.5-10`
- `16.0`

!!! Warning
    `latest` is not considered a valid tag for the image.

!!! Note
    Image tag requirements do no apply for images defined in a catalog.
