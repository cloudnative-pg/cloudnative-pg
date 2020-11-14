# Container Image Requirements

The Cloud Native PostgreSQL operator for Kubernetes is designed to
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
- Sensible locale settings
- User and group `postgres` with `UID` and `GID` set to 26

No entry point and/or command is required in the image definition, as Cloud
Native PostgreSQL overrides it with its own instance manager.

!!! Warning
    Application Container Images will be used by Cloud Native PostgreSQL
    in a **Primary with multiple/optional Hot Standby Servers Architecture**
    only.

EnterpriseDB provides and supports public container images for Cloud Native
PostgreSQL and publishes them on [Quay.io](https://quay.io/repository/edb/postgresql).

## Image tag requirements

While the image name can be anything valid for Docker, the Cloud Native
PostgreSQL operator relies on the *image tag* to detect the Postgres major
version carried out by the image.

The image tag must start with a valid PostgreSQL major version number (e.g. 9.6
or 12) optionally followed by a dot and the patch level.

The prefix can be followed by any valid character combination that is valid and
accepted in a Docker tag, preceded by a dot, an underscore or a minus sign.

Examples of accepted image tags:

- `9.6.19-alpine`
- `12.4`
- `11_1`
- `13`
- `12.3.2.1-1`

!!! Warning
    `latest` is not considered a valid tag for the image.
