---
id: image_catalog
sidebar_position: 70
title: Image Catalog
---

# Image Catalog
<!-- SPDX-License-Identifier: CC-BY-4.0 -->

`ImageCatalog` and `ClusterImageCatalog` are Custom Resource Definitions (CRDs)
that allow you to decouple the PostgreSQL image lifecycle from the `Cluster`
definition. By using a catalog, you can manage image updates centrally; when a
catalog entry is updated, all associated clusters automatically
[roll out the new image](rolling_update.md).

## Catalog scoping

The primary difference between the two resources is their scope:

| Resource              | Scope        | Best use case                                               |
| --------------------- | ------------ | ----------------------------------------------------------- |
| `ImageCatalog`        | Namespaced   | Application-specific versions or team-level restrictions.   |
| `ClusterImageCatalog` | Cluster-wide | Global standards across all namespaces for an organization. |

## Catalog structure

Both resources share a common schema:

- **Major versioning**: A list of images keyed by the `major` PostgreSQL version.
- **Uniqueness**: The `major` field must be unique within a single catalog.
- **Extensions**: Support for certified extension container images (available for
  PostgreSQL 18+ via `extension_control_path`).

:::warning
The operator trusts the user-defined `major` version and does **not** perform
image detection. Ensure the declared major version in the catalog matches the
actual PostgreSQL image.
:::

## Configuration examples

### Defining a catalog

You can define multiple major versions within a single catalog.

The following example defines a namespaced `ImageCatalog`:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: ImageCatalog
metadata:
  name: postgresql
  namespace: default
spec:
  images:
    - major: 15
      image: ghcr.io/cloudnative-pg/postgresql:15.14-system-trixie
    - major: 16
      image: ghcr.io/cloudnative-pg/postgresql:16.10-system-trixie
    - major: 17
      image: ghcr.io/cloudnative-pg/postgresql:17.6-system-trixie
    - major: 18
      image: ghcr.io/cloudnative-pg/postgresql:18.1-system-trixie
```

The following example defines a cluster-wide `ClusterImageCatalog`:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: ClusterImageCatalog
metadata:
  name: postgresql-global
spec:
  images:
    - major: 15
      image: ghcr.io/cloudnative-pg/postgresql:15.14-system-trixie
    - major: 16
      image: ghcr.io/cloudnative-pg/postgresql:16.10-system-trixie
    - major: 17
      image: ghcr.io/cloudnative-pg/postgresql:17.6-system-trixie
    - major: 18
      image: ghcr.io/cloudnative-pg/postgresql:18.1-system-trixie
```

### Referencing a Catalog in a Cluster

A `Cluster` resource uses the `imageCatalogRef` to select its images.

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-example
spec:
  instances: 3
  imageCatalogRef:
    apiGroup: postgresql.cnpg.io
    kind: ClusterImageCatalog # Or 'ImageCatalog'
    name: postgresql-global
    major: 18
  storage:
    size: 1Gi
```

## Image Catalog with Image Volume Extensions

[Image Volume Extensions](imagevolume_extensions.md) allow you to bundle
sidecar containers for extensions directly within the catalog entry.

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: ImageCatalog
metadata:
  name: postgresql
spec:
  images:
    - major: 18
      image: ghcr.io/cloudnative-pg/postgresql:18.1-minimal-trixie
      extensions:
        - name: foo
          image:
            reference: # registry path for your `foo` extension image
```

The `extensions` section follows the [`ExtensionConfiguration`](cloudnative-pg.v1.md#extensionconfiguration) API schema.

As a result, the [Image volume Extensions - Advanced Topics](imagevolume_extensions.md#advanced-topics) section also apply
here in case you need to finely control the configuration of an extension.

A `Cluster` that references a Catalog image for a given major version can
request any extensions associated with that image to be loaded into the `Cluster`.
For details, see [Adding a new extension defined via `Image Catalog` to a `Cluster` resource](imagevolume_extensions.md#adding-a-new-extension-defined-via-image-catalog-to-a-cluster-resource).

## CloudNativePG Catalogs

The CloudNativePG project maintains `ClusterImageCatalog` manifests for all
supported images.

These catalogs are regularly updated and published in the
[artifacts repository](https://github.com/cloudnative-pg/artifacts/tree/main/image-catalogs).

Each catalog corresponds to a specific combination of image type (e.g.
`minimal`) and Debian release (e.g. `trixie`). It lists the most up-to-date
container images for every supported PostgreSQL major version.

By installing these catalogs, cluster administrators can ensure that their
PostgreSQL clusters are automatically updated to the latest patch release
within a given PostgreSQL major version, for the selected Debian distribution
and image type.

For example, to install the latest catalog for the `minimal` PostgreSQL
container images on Debian `trixie`, run:

```shell
kubectl apply -f \
  https://raw.githubusercontent.com/cloudnative-pg/artifacts/refs/heads/main/image-catalogs/catalog-minimal-trixie.yaml
```

You can install all the available catalogs by using the `kustomization` file
present in the `image-catalogs` directory:

```shell
kubectl apply -k https://github.com/cloudnative-pg/artifacts//image-catalogs?ref=main
```

You can then view all the catalogs deployed with:

```shell
kubectl get clusterimagecatalogs.postgresql.cnpg.io
```

For example, you can create a cluster with the latest `minimal` image for PostgreSQL 18 on `trixie` with:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: angus
spec:
  instances: 3
  imageCatalogRef:
    apiGroup: postgresql.cnpg.io
    kind: ClusterImageCatalog
    name: postgresql-minimal-trixie
    major: 18
  storage:
    size: 1Gi
```
