# Image Catalog

`ImageCatalog` and `ClusterImageCatalog` are resources that allow you to define
images that can be used to create a `Cluster`.

The only difference between them is the scope: an `ImageCatalog` is namespaced,
while a `ClusterImageCatalog` is cluster-scoped.

Both of them have the same structure, composed of a list of images, each of them
with a `major` field that indicates the major version of the image.

!!! Warning The operator will trust the user-defined major version and will not
perform any detection of the PostgreSQL version. It is up to the user to ensure
that the PostgreSQL image matches the major version declared in the catalog.

The `major` field value must be unique among the images within a catalog.
Different catalogs can expose different images under the same `major` value.

Examples:

```yaml
kind: ImageCatalog
metadata:
  name: postgresql
  namespace: default
spec:
  images:
    - major: 15
      image: ghcr.io/cloudnative-pg/postgresql:15.6
    - major: 16
      image: ghcr.io/cloudnative-pg/postgresql:16.2
```

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
      image: ghcr.io/cloudnative-pg/postgresql:16.2
```

A `Cluster` resource can reference an `ImageCatalog` or a `ClusterImageCatalog`
to specify the image to be used.

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

Clusters using these catalogs continuously monitor them. When the image in a
catalog is changed, **all the clusters** referring to that entry will
automatically be updated.
