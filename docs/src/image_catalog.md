# Image Catalog

`ImageCatalog` and `ClusterImageCatalog` are resources that allow you to define images that can be used to create
a `Cluster` resource.

The only difference between them is the scope: an `ImageCatalog` is namespaced, while a `ClusterImageCatalog` is
cluster-scoped.

Both of them have the same structure, composed of a list of images, each of them with a `major` field that indicates the
major version of the image.

!!! Warning The operator will trust the user-defined major version and will not perform any detection of the PostgreSQL
version. It is up to the user to ensure that the image is compatible with the major version declared in the catalog.

The `major` field value must be unique among the images within a catalog. Different catalogs can expose different images
under the same `major` value.

```yaml
kind: ImageCatalog
metadata:
  name: postgresql
spec:
  images:
    - image: ghcr.io/cloudnative-pg/postgresql:16.0
      major: 16
    - image: ghcr.io/cloudnative-pg/postgresql:15.1
      major: 15
```

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: ClusterImageCatalog
metadata:
  name: postgresql
spec:
  images:
    - image: ghcr.io/cloudnative-pg/postgresql:16.1
      major: 16
    - image: ghcr.io/cloudnative-pg/postgresql:15.2
      major: 15
```

A `Cluster` resource can reference an `ImageCatalog` or a `ClusterImageCatalog` to specify the image to use for the
instances of the cluster.

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
    catalogName: postgresql
    major: 16
  storage:
    size: 1Gi
```

Clusters using an `ImageCatalog` or a `ClusterImageCatalog` keep a watch on it, and if the image in the referred catalog
is updated, the operator will automatically update the image of the instances of the cluster to the latest available
image in the catalog for the same major version.

# Image Catalog

The **Image Catalog** feature in our system involves two resources: `ImageCatalog` and `ClusterImageCatalog`. These
resources enable you to define images for use in creating a `Cluster`.

An `ImageCatalog` is a resource that defines images within a specific namespace. It contains a list of images, each
identified by a `major` field indicating the major version of the image.

On the other hand, a `ClusterImageCatalog` is similar to an `ImageCatalog` but is cluster-scoped. This means it is not
bound to a specific namespace.

Both `ImageCatalog` and `ClusterImageCatalog` share the same structure:

```yaml
kind: ImageCatalog
metadata:
  name: postgresql
spec:
  images:
  - image: ghcr.io/cloudnative-pg/postgresql:16.0
    major: 16
  - image: ghcr.io/cloudnative-pg/postgresql:15.1
    major: 15
```

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: ClusterImageCatalog
metadata:
  name: postgresql
spec:
  images:
  - image: ghcr.io/cloudnative-pg/postgresql:16.1
    major: 16
  - image: ghcr.io/cloudnative-pg/postgresql:15.2
    major: 15
```

!!! Warning
    The operator trusts the user-defined major version and does not perform any detection of the PostgreSQL
    version. Users must ensure that the declared major version is compatible with the image.

The `major` field value must be unique within a catalog, but different catalogs can expose images under the same `major`
value.

## Usage in Cluster Resource

A `Cluster` resource can reference either an `ImageCatalog` or a `ClusterImageCatalog` to specify the image for the
cluster instances.

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-example
spec:
  instances: 3
  imageCatalogRef:
    kind: ImageCatalog
    catalogName: postgresql
    major: 16
  storage:
    size: 1Gi
```

Clusters using these catalogs continuously monitor them. If the image in the referred catalog is updated, the operator
automatically updates the image of the cluster instances to the latest available image for the same major version.
