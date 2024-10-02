# Image Catalog

`ImageCatalog` and `ClusterImageCatalog` are essential resources that empower
you to define images for creating a `Cluster`.

The key distinction lies in their scope: an `ImageCatalog` is namespaced, while
a `ClusterImageCatalog` is cluster-scoped.

Both share a common structure, comprising a list of images, each equipped with
a `major` field indicating the major version of the image.

!!! Warning
    The operator places trust in the user-defined major version and refrains
    from conducting any PostgreSQL version detection. It is the user's
    responsibility to ensure alignment between the declared major version in
    the catalog and the PostgreSQL image.

The `major` field's value must remain unique within a catalog, preventing
duplication across images. Distinct catalogs, however, may
expose different images under the same `major` value.

**Example of a Namespaced `ImageCatalog`:**

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: ImageCatalog
metadata:
  name: postgresql
  namespace: default
spec:
  images:
    - major: 15
      image: ghcr.io/cloudnative-pg/postgresql:15.6
    - major: 16
      image: ghcr.io/cloudnative-pg/postgresql:17.0
```

**Example of a Cluster-Wide Catalog using `ClusterImageCatalog` Resource:**

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: ClusterImageCatalog
metadata:
  name: postgresql
spec:
  images:
    - major: 15
      image: ghcr.io/cloudnative-pg/postgresql:15.6
    - major: 16
      image: ghcr.io/cloudnative-pg/postgresql:17.0
```

A `Cluster` resource has the flexibility to reference either an `ImageCatalog`
or a `ClusterImageCatalog` to precisely specify the desired image.

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-example
spec:
  instances: 3
  imageCatalogRef:
    apiGroup: postgresql.cnpg.io
    kind: ImageCatalog
    name: postgresql
    major: 16
  storage:
    size: 1Gi
```

Clusters utilizing these catalogs maintain continuous monitoring.
Any alterations to the images within a catalog trigger automatic updates for
**all associated clusters** referencing that specific entry.

## CloudNativePG Catalogs

The CloudNativePG project maintains `ClusterImageCatalogs` for the images it
provides. These catalogs are regularly updated with the latest images for each
major version. By applying the `ClusterImageCatalog.yaml` file from the
CloudNativePG project's GitHub repositories, cluster administrators can ensure
that their clusters are automatically updated to the latest version within the
specified major release.

### PostgreSQL Container Images

You can install the
[latest version of the cluster catalog for the PostgreSQL Container Images](https://raw.githubusercontent.com/cloudnative-pg/postgres-containers/main/Debian/ClusterImageCatalog.yaml)
([cloudnative-pg/postgres-containers](https://github.com/cloudnative-pg/postgres-containers) repository)
with:

```shell
kubectl apply \
  -f https://raw.githubusercontent.com/cloudnative-pg/postgres-containers/main/Debian/ClusterImageCatalog.yaml
```

### PostGIS Container Images

You can install the
[latest version of the cluster catalog for the PostGIS Container Images](https://raw.githubusercontent.com/cloudnative-pg/postgis-containers/main/PostGIS/ClusterImageCatalog.yaml)
([cloudnative-pg/postgis-containers](https://github.com/cloudnative-pg/postgis-containers) repository)
with:

```shell
kubectl apply \
  -f https://raw.githubusercontent.com/cloudnative-pg/postgis-containers/main/PostGIS/ClusterImageCatalog.yaml
```
