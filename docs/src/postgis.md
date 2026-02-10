---
id: postgis
sidebar_position: 440
title: PostGIS
---

# PostGIS
<!-- SPDX-License-Identifier: CC-BY-4.0 -->

[PostGIS](https://postgis.net/) is a very popular open source extension
for PostgreSQL that introduces support for storing GIS (Geographic Information
Systems) objects in the database and be queried via SQL.

:::info[Important]
    This section assumes you are familiar with PostGIS and provides some basic
    information about how to create a new PostgreSQL cluster with a PostGIS database
    in Kubernetes via CloudNativePG.
:::

## Supported Installation Methods

Depending on your environment (PostgreSQL version, Kubernetes version, and
CloudNativePG version), you have two primary ways to provision PostGIS:

1. [**Image Volume Extensions**](#method-1-image-volume-extensions): Mounts
   extension binaries directly from OCI images as read-only volumes (recommended
   for PG 18+ and Kubernetes 1.35+).

2. [**PostGIS Operand Images**](#method-2-postgis-operand-images): Uses a
   pre-built PostgreSQL image that already contains PostGIS libraries.

Once PostGIS is provisioned on the system using one of the methods below, you
must still enable and manage it inside the database (see the
["Enabling the Extension"](#enabling-the-extension) section below).

## Method 1: Image Volume Extensions

Starting with **PostgreSQL 18** and **Kubernetes 1.35** (1.33 and 1.34 with the
`ImageVolume` feature gate enabled), you can leverage
[image volume extensions](imagevolume_extensions.md).

This is the modern way to manage extensions, as it allows you to use an
official minimal PostgreSQL image and "plug in" PostGIS at runtime, by mounting
the content of the extension's OCI image directly into the Postgres container
as a read-only volume. This decouples the extension lifecycle from the base
PostgreSQL image, keeping your operand images slim and secure.

The CloudNativePG Community distributes standalone extension container images
for PostGIS via the
[`postgres-extensions-containers`](https://github.com/cloudnative-pg/postgres-extensions-containers/tree/main/postgis)
repository.

<!--
### For CloudNativePG 1.29+

Starting from CloudNativePG 1.29 you can take advantage of
[image catalogs with image volume extensions](image_catalog.md)
to automate the management of these versions.

### For other supported CNPG versions
-->

The following illustrative example shows how to add PostGIS 3.6.1 to a
PostgreSQL 18 cluster, by loading the extension directly in the `Cluster`
resource:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-postgis
spec:
  imageName: ghcr.io/cloudnative-pg/postgresql:18.1-minimal-trixie
  instances: 1

  storage:
    size: 1Gi

  postgresql:
    extensions:
    - name: postgis
      image:
        reference: ghcr.io/cloudnative-pg/postgis-extension:3.6.1-18-trixie
      ld_library_path:
      - system
```

:::note
Refer to the [`postgres-extensions-containers`](https://github.com/cloudnative-pg/postgres-extensions-containers/tree/main/postgis)
project for the latest versions and instructions for this extension.
:::

## Method 2: PostGIS Operand Images

For users who prefer to keep using the dedicated `postgis-containers` registry,
or for those on older versions of PostgreSQL or Kubernetes that do not support
image volume extensions, the community continues to maintain images with
PostGIS pre-installed (built on top of the [PostgreSQL Container images](https://github.com/cloudnative-pg/postgres-containers)).

For more information, please visit:

- The [`postgis-containers` project in GitHub](https://github.com/cloudnative-pg/postgis-containers)
- The [`postgis-containers` Container Registry in GitHub](https://github.com/cloudnative-pg/postgis-containers/pkgs/container/postgis)

To use this method, point the `.spec.imageName` to the desired PostGIS-enabled
image:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-postgis
spec:
  instances: 1
  imageName: ghcr.io/cloudnative-pg/postgis:18.1-3.6.1-system-trixie
  storage:
    size: 1Gi
  postgresql:
    parameters:
      log_statement: ddl
```

---

## Enabling the Extension

Once the binaries are provisioned (via an image volume extension or directly in
the `Cluster` resource), you must enable the extension in the database.

Using the `Database` resource allows for declarative management of the
extension's lifecycle, including version upgrades.

:::info
For more details, see the
["Managing Extensions in a Database" section](declarative_database_management.md#managing-extensions-in-a-database).
:::

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Database
metadata:
  name: cluster-postgis-app
spec:
  name: app
  owner: app
  cluster:
    name: cluster-postgis
  extensions:
  - name: postgis
    version: '3.6.1'
  - name: postgis_raster
  - name: postgis_sfcgal
  - name: fuzzystrmatch
  - name: address_standardizer
  - name: address_standardizer_data_us
  - name: postgis_tiger_geocoder
  - name: postgis_topology
```

:::tip
Specifying the `version` in the extensions stanza is highly recommended.
CloudNativePG will compare this version with the one currently installed in the
database; if they differ, it will automatically execute the necessary
`ALTER EXTENSION ... UPDATE TO ...` command.
:::

## Verification

You can easily verify the available version of PostGIS that is in the
container, by connecting to the `app` database and querying the PostgreSQL
catalog. For example:

```bash
kubectl cnpg psql cluster-postgis -- app -c \
  "SELECT * FROM pg_available_extensions WHERE name ~ '^postgis' ORDER BY 1"
```

Returning something similar to this (you might obtain different values from the
ones in this document):

```console
           name           | default_version | installed_version |                          comment
--------------------------+-----------------+-------------------+------------------------------------------------------------
 postgis                  | 3.6.1           | 3.6.1             | PostGIS geometry and geography spatial types and functions
 postgis-3                | 3.6.1           |                   | PostGIS geometry and geography spatial types and functions
 postgis_raster           | 3.6.1           | 3.6.1             | PostGIS raster types and functions
 postgis_raster-3         | 3.6.1           |                   | PostGIS raster types and functions
 postgis_sfcgal           | 3.6.1           | 3.6.1             | PostGIS SFCGAL functions
 postgis_sfcgal-3         | 3.6.1           |                   | PostGIS SFCGAL functions
 postgis_tiger_geocoder   | 3.6.1           | 3.6.1             | PostGIS tiger geocoder and reverse geocoder
 postgis_tiger_geocoder-3 | 3.6.1           |                   | PostGIS tiger geocoder and reverse geocoder
 postgis_topology         | 3.6.1           | 3.6.1             | PostGIS topology spatial types and functions
 postgis_topology-3       | 3.6.1           |                   | PostGIS topology spatial types and functions
(10 rows)
```

The next step is to verify that the extensions listed in the `Database`
resource have been correctly installed in the `app` database:

```bash
kubectl cnpg psql cluster-postgis -- app -c '\dx'
```

The command returns something like this:

```console
                                                                                List of installed extensions
             Name             | Version | Default version |   Schema   |                                                     Description

------------------------------+---------+-----------------+------------+---------------------------------------------------------------------------------
------------------------------------
 address_standardizer         | 3.6.1   | 3.6.1           | public     | Used to parse an address into constituent elements. Generally used to support ge
ocoding address normalization step.
 address_standardizer_data_us | 3.6.1   | 3.6.1           | public     | Address Standardizer US dataset example
 fuzzystrmatch                | 1.2     | 1.2             | public     | determine similarities and distance between strings
 plpgsql                      | 1.0     | 1.0             | pg_catalog | PL/pgSQL procedural language
 postgis                      | 3.6.1   | 3.6.1           | public     | PostGIS geometry and geography spatial types and functions
 postgis_raster               | 3.6.1   | 3.6.1           | public     | PostGIS raster types and functions
 postgis_sfcgal               | 3.6.1   | 3.6.1           | public     | PostGIS SFCGAL functions
 postgis_tiger_geocoder       | 3.6.1   | 3.6.1           | tiger      | PostGIS tiger geocoder and reverse geocoder
 postgis_topology             | 3.6.1   | 3.6.1           | topology   | PostGIS topology spatial types and functions
(9 rows)
```

Finally:

```bash
kubectl cnpg psql cluster-postgis -- app -c 'SELECT postgis_full_version()'
```

Returning:

```console
                             postgis_full_version

---------------------------------------------------------------------------------------------------------------------------------------------------------
---------------------------------------------------------------------------------------------------------------------------------------------------------
------------------------------------------------------------------------------
 POSTGIS="3.6.1 f533623" [EXTENSION] PGSQL="180" GEOS="3.13.1-CAPI-1.19.2" SFCGAL="SFCGAL 2.0.0, CGAL 6.0, BOOST 1.83.0" PROJ="9.6.0 NETWORK_ENABLED=OFF
URL_ENDPOINT= USER_WRITABLE_DIRECTORY=/tmp/proj" (compiled against PROJ 9.6.0) GDAL="GDAL 3.10.3, released 2025/04/01 GDAL_DATA not found" LIBXML="2.9.14
" LIBJSON="0.18" LIBPROTOBUF="1.5.1" WAGYU="0.5.0 (Internal)" TOPOLOGY RASTER
(1 row)
```
