# PostGIS

[PostGIS](https://postgis.net/) is a very popular open source extension
for PostgreSQL that introduces support for storing GIS (Geographic Information
Systems) objects in the database and be queried via SQL.

!!! Important
    This section assumes you are familiar with PostGIS and provides some basic
    information about how to create a new PostgreSQL cluster with a PostGIS database
    in Kubernetes via CloudNativePG.

The CloudNativePG Community maintains container images that are built on top
of the official [PostGIS images hosted on DockerHub](https://hub.docker.com/r/postgis/postgis).
For more information please visit:

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
- install it in the `template1` database so as to make it available for all the
  databases you end up creating in the cluster, in case you adopt the monolith
  architecture where the instance is shared by multiple databases

!!! Info
    For more information on the microservice vs monolith architecture in the database
    please refer to the ["How many databases should be hosted in a single PostgreSQL instance?" FAQ](faq.md)
    or the ["Database import" section](database_import.md).

## Create a new PostgreSQL cluster with PostGIS

Let's suppose you want to create a new PostgreSQL 14 cluster with PostGIS 3.2.

The first step is to ensure you use the right PostGIS container image for the
operand, and properly set the `.spec.imageName` option in the `Cluster`
resource.

The [`postgis-example.yaml` manifest](samples/postgis-example.yaml) below
provides some guidance on how the creation of a PostGIS cluster can be done.

!!! Warning
    Please consider that, although convention over configuration applies in
    CloudNativePG, you should spend time configuring and tuning your system for
    production. Also the `imageName` in the example below deliberately points
    to the latest available image for PostgreSQL 14 - you should use a specific
    image name or, preferably, the SHA256 digest for true immutability.

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: postgis-example
spec:
  instances: 3
  imageName: ghcr.io/cloudnative-pg/postgis:14
  bootstrap:
    initdb:
      postInitTemplateSQL:
        - CREATE EXTENSION postgis;
        - CREATE EXTENSION postgis_topology;
        - CREATE EXTENSION fuzzystrmatch;
        - CREATE EXTENSION postgis_tiger_geocoder;

  storage:
    size: 1Gi
```

The example relies on the `postInitTemplateSQL` option which executes a list of
queries against the `template1` database, before the actual creation of the
application database (called `app`). This means that, once you have applied the
manifest and the cluster is up, you will have the above extensions installed in
both the template database and the application database, ready for use.

!!! Info
    Take some time and look at the available options in `.spec.bootstrap.initdb`
    from the [API reference](api_reference.md#BootstrapInitDB), such as
    `postInitApplicationSQL`.

You can easily verify the available version of PostGIS that is in the
container, by connecting to the `app` database (you might obtain different
values from the ones in this document):

```console
$ kubectl exec -ti postgis-example-1 -- psql app
Defaulted container "postgres" out of: postgres, bootstrap-controller (init)
psql (15.3 (Debian 15.3-1.pgdg110+1))
Type "help" for help.

app=# SELECT * FROM pg_available_extensions WHERE name ~ '^postgis' ORDER BY 1;
           name           | default_version | installed_version |                          comment
--------------------------+-----------------+-------------------+------------------------------------------------------------
 postgis                  | 3.2.2           | 3.2.2             | PostGIS geometry and geography spatial types and functions
 postgis-3                | 3.2.2           |                   | PostGIS geometry and geography spatial types and functions
 postgis_raster           | 3.2.2           |                   | PostGIS raster types and functions
 postgis_raster-3         | 3.2.2           |                   | PostGIS raster types and functions
 postgis_sfcgal           | 3.2.2           |                   | PostGIS SFCGAL functions
 postgis_sfcgal-3         | 3.2.2           |                   | PostGIS SFCGAL functions
 postgis_tiger_geocoder   | 3.2.2           | 3.2.2             | PostGIS tiger geocoder and reverse geocoder
 postgis_tiger_geocoder-3 | 3.2.2           |                   | PostGIS tiger geocoder and reverse geocoder
 postgis_topology         | 3.2.2           | 3.2.2             | PostGIS topology spatial types and functions
 postgis_topology-3       | 3.2.2           |                   | PostGIS topology spatial types and functions
(10 rows)
```

The next step is to verify that the extensions listed in the
`postInitTemplateSQL` section have been correctly installed in the `app`
database.

```console
app=# \dx
                                        List of installed extensions
          Name          | Version |   Schema   |                        Description
------------------------+---------+------------+------------------------------------------------------------
 fuzzystrmatch          | 1.1     | public     | determine similarities and distance between strings
 plpgsql                | 1.0     | pg_catalog | PL/pgSQL procedural language
 postgis                | 3.2.2   | public     | PostGIS geometry and geography spatial types and functions
 postgis_tiger_geocoder | 3.2.2   | tiger      | PostGIS tiger geocoder and reverse geocoder
 postgis_topology       | 3.2.2   | topology   | PostGIS topology spatial types and functions
(5 rows)
```

Finally:

```console
app=# SELECT postgis_full_version();
                                                                            postgis_full_version
----------------------------------------------------------------------------------------------------------------------------------------------------------------------------
 POSTGIS="3.2.2 628da50" [EXTENSION] PGSQL="140" GEOS="3.9.0-CAPI-1.16.2" PROJ="7.2.1" LIBXML="2.9.10" LIBJSON="0.15" LIBPROTOBUF="1.3.3" WAGYU="0.5.0 (Internal)" TOPOLOGY
(1 row)
```
