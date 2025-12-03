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

The CloudNativePG Community maintains container images that are built on top
of the maintained [PostgreSQL Container images](https://github.com/cloudnative-pg/postgres-containers).
For more information, please visit:

- The [`postgis-containers` project in GitHub](https://github.com/cloudnative-pg/postgis-containers)
- The [`postgis-containers` Container Registry in GitHub](https://github.com/cloudnative-pg/postgis-containers/pkgs/container/postgis)

## Basic concepts about a PostGIS cluster

Conceptually, a PostGIS-based PostgreSQL cluster (or simply a PostGIS cluster)
is like any other PostgreSQL cluster. The only differences are:

- the presence in the system of PostGIS and related libraries
- the presence in the database(s) of the PostGIS extension

Since CloudNativePG is based on Immutable Application Containers, the only way
to provision PostGIS is to add it to the container image that you use for the
operand. The ["Container Image Requirements" section](container_images.md) provides
detailed instructions on how this is achieved. More simply, you can just use
the PostGIS container images from the Community, as in the examples below.

The second step is to install the extension in the PostgreSQL database. You can
do this in two ways:

- install it in the application database, which is the main and supposedly only
  database you host in the cluster according to the microservice architecture, or
- install it in the `template1` database to make it available for all the
  databases you end up creating in the cluster, in case you adopt the monolith
  architecture where the instance is shared by multiple databases

:::info
    For more information on the microservice vs monolith architecture in the database,
    please refer to the ["How many databases should be hosted in a single PostgreSQL instance?" FAQ](faq.md)
    or the ["Database import" section](database_import.md).
:::

## Create a new PostgreSQL cluster with PostGIS

Let's suppose you want to create a new PostgreSQL 18 cluster with PostGIS 3.6.

The first step is to ensure you use the right PostGIS container image for the
operand, and properly set the `.spec.imageName` option in the `Cluster`
resource.

The [`postgis-example.yaml` manifest](samples/postgis-example.yaml) below
provides some guidance on how the creation of a PostGIS cluster can be done.

:::warning
    Please consider that, although convention over configuration applies in
    CloudNativePG, you should spend time configuring and tuning your system for
    production. Also, the `imageName` in the example below deliberately points
    to the latest available image for PostgreSQL 18 - you should use a specific
    image name or, preferably, the SHA256 digest for true immutability.
    Alternatively, use the provided [image catalogs](https://github.com/cloudnative-pg/postgis-containers?tab=readme-ov-file#image-catalogs).
:::

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: postgis-example
spec:
  instances: 1
  imageName: ghcr.io/cloudnative-pg/postgis:18-3.6-system-trixie
  storage:
    size: 1Gi
  postgresql:
    parameters:
      log_statement: ddl
---
apiVersion: postgresql.cnpg.io/v1
kind: Database
metadata:
  name: postgis-example-app
spec:
  name: app
  owner: app
  cluster:
    name: postgis-example
  extensions:
  - name: postgis
  - name: postgis_topology
  - name: fuzzystrmatch
  - name: postgis_tiger_geocoder
```

The example leverages the `Database` resource's declarative extension
management to add the specified extensions to the `app` database.

:::info
    For more details, see the
    ["Managing Extensions in a Database" section](declarative_database_management.md#managing-extensions-in-a-database).
:::

You can easily verify the available version of PostGIS that is in the
container, by connecting to the `app` database (you might obtain different
values from the ones in this document):

```console
$ kubectl cnpg psql postgis-example -- app
psql (18.1 (Debian 18.1-1.pgdg13+3))
Type "help" for help.

app=# SELECT * FROM pg_available_extensions WHERE name ~ '^postgis' ORDER BY 1;
           name           | default_version | installed_version |                          comment
--------------------------+-----------------+-------------------+------------------------------------------------------------
 postgis                  | 3.6.0           | 3.6.0             | PostGIS geometry and geography spatial types and functions
 postgis-3                | 3.6.0           |                   | PostGIS geometry and geography spatial types and functions
 postgis_raster           | 3.6.0           |                   | PostGIS raster types and functions
 postgis_raster-3         | 3.6.0           |                   | PostGIS raster types and functions
 postgis_sfcgal           | 3.6.0           |                   | PostGIS SFCGAL functions
 postgis_sfcgal-3         | 3.6.0           |                   | PostGIS SFCGAL functions
 postgis_tiger_geocoder   | 3.6.0           | 3.6.0             | PostGIS tiger geocoder and reverse geocoder
 postgis_tiger_geocoder-3 | 3.6.0           |                   | PostGIS tiger geocoder and reverse geocoder
 postgis_topology         | 3.6.0           | 3.6.0             | PostGIS topology spatial types and functions
 postgis_topology-3       | 3.6.0           |                   | PostGIS topology spatial types and functions
(10 rows)
```

The next step is to verify that the extensions listed in the `Database`
resource have been correctly installed in the `app` database.

```console
app=# \dx
                                                 List of installed extensions
          Name          | Version | Default version |   Schema   |                        Description
------------------------+---------+-----------------+------------+------------------------------------------------------------
 fuzzystrmatch          | 1.2     | 1.2             | public     | determine similarities and distance between strings
 plpgsql                | 1.0     | 1.0             | pg_catalog | PL/pgSQL procedural language
 postgis                | 3.6.0   | 3.6.0           | public     | PostGIS geometry and geography spatial types and functions
 postgis_tiger_geocoder | 3.6.0   | 3.6.0           | tiger      | PostGIS tiger geocoder and reverse geocoder
 postgis_topology       | 3.6.0   | 3.6.0           | topology   | PostGIS topology spatial types and functions
```

Finally:

```console
app=# SELECT postgis_full_version();
                                                                            postgis_full_version
----------------------------------------------------------------------------------------------------------------------------------------------------------------------------
 POSTGIS="3.6.0 4c1967d" [EXTENSION] PGSQL="180" GEOS="3.13.1-CAPI-1.19.2" PROJ="9.6.0 NETWORK_ENABLED=OFF URL_ENDPOINT=https://cdn.proj.org USER_WRITABLE_DIRECTORY=/tmp/proj DATABASE_PATH=/usr/share/proj/proj.
db" (compiled against PROJ 9.6.0) LIBXML="2.9.14" LIBJSON="0.18" LIBPROTOBUF="1.5.1" WAGYU="0.5.0 (Internal)" TOPOLOGY
(1 row)
```
