# Container Image Requirements

The CloudNativePG operator for Kubernetes is designed to
work with any compatible container image of PostgreSQL that complies
with the following requirements:

- PostgreSQL 10+ executables that must be in the path:
    - `initdb`
    - `postgres`
    - `pg_ctl`
    - `pg_controldata`
    - `pg_basebackup`
- Barman Cloud executables that must be in the path:
    - `barman-cloud-wal-archive`
    - `barman-cloud-wal-restore`
    - `barman-cloud-backup`
    - `barman-cloud-restore`
    - `barman-cloud-backup-list`
    - `barman-cloud-check-wal-archive`
- PGAudit extension installed (optional - only if PGAudit is required
  in the deployed clusters)
- Sensible locale settings

No entry point and/or command is required in the image definition, as Cloud
Native PostgreSQL overrides it with its instance manager.

!!! Warning
    Application Container Images will be used by CloudNativePG
    in a **Primary with multiple/optional Hot Standby Servers Architecture**
    only.

EDB provides and supports public container images for Cloud Native
PostgreSQL and publishes them on
[Quay.io](https://quay.io/repository/enterprisedb/postgresql).

## Image tag requirements

While the image name can be anything valid for Docker, the Cloud Native
PostgreSQL operator relies on the *image tag* to detect the Postgres major
version carried out by the image.

The image tag must start with a valid PostgreSQL major version number (e.g. 9.6
or 12) optionally followed by a dot and the patch level.

The prefix can be followed by any valid character combination that is valid and
accepted in a Docker tag, preceded by a dot, an underscore, or a minus sign.

Examples of accepted image tags:

- `9.6.19-alpine`
- `12.4`
- `11_1`
- `13`
- `12.3.2.1-1`

!!! Warning
    `latest` is not considered a valid tag for the image.
