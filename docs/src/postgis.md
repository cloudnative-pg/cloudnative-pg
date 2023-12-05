# PostGIS

[PostGIS](https://postgis.net/) is a very popular open-source extension
for PostgreSQL. It introduces support for storing GIS (Geographic Information
Systems) objects in the database that can be queried using SQL.

!!! Important
    This content assumes you're familiar with PostGIS. It provides some basic
    information about how to create a PostgreSQL cluster with a PostGIS database
    in Kubernetes by way of CloudNativePG.

The CloudNativePG community maintains container images that are built on top
of the official [PostGIS images hosted on DockerHub](https://hub.docker.com/r/postgis/postgis).
For more information see:

- The [`postgis-containers` project in GitHub](https://github.com/cloudnative-pg/postgis-containers)
- The [`postgis-containers` Container Registry in GitHub](https://github.com/cloudnative-pg/postgis-containers/pkgs/container/postgis)

## Basic concepts about a PostGIS cluster

Conceptually, a PostGIS-based PostgreSQL cluster (or simply a PostGIS cluster)
is like any other PostgreSQL cluster. The differences are:

- The presence of PostGIS and related libraries in the system
- The presence of the PostGIS extension in the databases

Since CloudNativePG is based on immutable application containers, the only way
to provision PostGIS is to add it to the container image that you use for the
operand. [Container image requirements](container_images.md) provides
detailed instructions on how to achieve this. More simply, you can use
the PostGIS container images from the community, as in the examples that follow.

The second step is to install the extension in the PostgreSQL database. You can
do this in either of two ways:

- Install it in the application database, which is the main and supposedly only
  database you host in the cluster according to the microservice architecture. 
- Install it in the `template1` database to make it available for all the
  databases you end up creating in the cluster. This method is useful if you adopt the monolith
  architecture where the instance is shared by multiple databases.

!!! Info
    For more information on the microservice versus monolith architecture in the database
    see the [How many databases should be hosted in a single PostgreSQL instance?](faq.md) FAQ
    or [Database import](database_import.md).

## Create a new PostgreSQL cluster with PostGIS

Suppose you want to create a PostgreSQL 14 cluster with PostGIS 3.2.

First, make sure you use the right PostGIS container image for the
operand, and properly set the `.spec.imageName` option in the `Cluster`
resource.

The [`postgis-example.yaml` manifest](samples/postgis-example.yaml)
provides some guidance on creating a PostGIS cluster.

!!! Warning
    Although convention over configuration applies in
    CloudNativePG, we recommend spending time configuring and tuning your system for
    production. Also, the `imageName` in the example that follows deliberately points
    to the latest available image for PostgreSQL 14. For true immutability, use a specific
    image name or, preferably, the SHA256 digest.

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

The example relies on the `postInitTemplateSQL` option. Before the actual creation of the
application database (called `app`), the option executes a list of
queries against the `template1` database. This means that, after you apply the
manifest and the cluster is up, the extensions shown in the example are installed in
both the template database and the application database, ready for use.

!!! Info
    Take some time and look at the available options in `.spec.bootstrap.initdb`
    from the [API reference](cloudnative-pg.v1.md#postgresql-cnpg-io-v1-BootstrapInitDB), such as
    `postInitApplicationSQL`.

You can easily verify the available version of PostGIS that's in the
container by connecting to the `app` database. (You might get different
values from the ones shown in the examples.)

```console
$ kubectl exec -ti postgis-example-1 -- psql app
Defaulted container "postgres" out of: postgres, bootstrap-controller (init)
psql (16.1 (Debian 16.1-1.pgdg110+1))
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
`postInitTemplateSQL` section were correctly installed in the `app`
database.

```console
app=# \dx
                                        List of installed extensions
          Name          | Version |   Schema   |                        Description
------------------------+---------+------------+------------------------------------------------------------
 fuzzystrmatch          | 1.1     | public     | Determine similarities and distance between strings
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
