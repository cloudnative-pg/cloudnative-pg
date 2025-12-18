---
id: container_images
sidebar_position: 460
title: Container Image Requirements
---

# Container Image Requirements
<!-- SPDX-License-Identifier: CC-BY-4.0 -->

The CloudNativePG operator for Kubernetes is designed to work with any
compatible PostgreSQL container image that meets the following requirements:

- PostgreSQL executables must be available in the system path:
  - `initdb`
  - `postgres`
  - `pg_ctl`
  - `pg_controldata`
  - `pg_basebackup`
- Proper locale settings configured

Optional Components:

- [PGAudit](https://www.pgaudit.org/) extension (only required if audit logging
  is needed)
- `du` (used for `kubectl cnpg status`)

:::info[Important]
    Only [PostgreSQL versions officially supported by PGDG](https://postgresql.org/) are allowed.
:::

:::info
    Barman Cloud executables are no longer required in CloudNativePG. The
    recommended approach is to use the dedicated [Barman Cloud Plugin](https://github.com/cloudnative-pg/plugin-barman-cloud).
:::

No entry point or command is required in the image definition. CloudNativePG
automatically overrides it with its instance manager.

:::warning
    CloudNativePG only supports **Primary with multiple/optional Hot Standby
    Servers architecture** for PostgreSQL application container images.
:::

The CloudNativePG community provides and maintains
[public PostgreSQL container images](https://github.com/cloudnative-pg/postgres-containers)
that are fully compatible with CloudNativePG. These images are published on
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

:::warning
    `latest` is not considered a valid tag for the image.
:::

:::note
    Image tag requirements do not apply for images defined in a catalog.
:::