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

While you can build custom catalogs, CloudNativePG provides
[official catalogs](#cloudnativepg-catalogs) as `ClusterImageCatalog`
resources, covering all official Community PostgreSQL container images.

## Catalog scoping

The primary difference between the two resources is their scope:

| Resource              | Scope        | Best use case                                               |
|-----------------------|--------------|-------------------------------------------------------------|
| `ImageCatalog`        | Namespaced   | Application-specific versions or team-level restrictions.   |
| `ClusterImageCatalog` | Cluster-wide | Global standards across all namespaces for an organization. |

## Catalog structure

Both resources share a common schema:

- **Major versioning**: A list of images keyed by the `major` PostgreSQL version.
- **Uniqueness**: The `major` field must be unique within a single catalog.
- **Extensions**: Support for certified extension container images (available for
  PostgreSQL 18+ via `extension_control_path`).

:::warning
While the operator trusts the user-defined `major` version without performing
image detection, the official CloudNativePG catalogs are pre-validated by the
community to ensure that every extension and operand image entry correctly
matches the declared major version. If you are creating a custom catalog, you
must ensure the declared major version matches the actual PostgreSQL images to
maintain compatibility.
:::

## Extra images

In addition to PostgreSQL images, a catalog can store images for other
components via the `extraImages` field. Each entry is identified by a string
**key** — a lowercase alphanumeric identifier (hyphens allowed, 1–63
characters) that must be unique within the catalog.

Currently, the supported consumer of extra images is the `Pooler` resource,
which can use an extra image entry to manage the pgbouncer container image
centrally (see
[Pooler image catalog reference](connection_pooling.md#pooler-image-catalog-reference)).

The following example defines a namespaced `ImageCatalog` with a pgbouncer
extra image:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: ImageCatalog
metadata:
  name: my-catalog
  namespace: default
spec:
  images:
    - major: 17
      image: ghcr.io/cloudnative-pg/postgresql:17.6-system-trixie
  extraImages:
    - key: pgbouncer
      image: ghcr.io/cloudnative-pg/pgbouncer:1.25.1
```

The equivalent cluster-wide `ClusterImageCatalog`:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: ClusterImageCatalog
metadata:
  name: my-global-catalog
spec:
  images:
    - major: 17
      image: ghcr.io/cloudnative-pg/postgresql:17.6-system-trixie
  extraImages:
    - key: pgbouncer
      image: ghcr.io/cloudnative-pg/pgbouncer:1.25.1
```

:::info
A catalog may contain up to 32 extra image entries. Keys follow the same
format as Kubernetes label values: lowercase alphanumeric characters or
hyphens, starting and ending with an alphanumeric character, up to 63
characters long.
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
      image: ghcr.io/cloudnative-pg/postgresql:18.3-system-trixie
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
      image: ghcr.io/cloudnative-pg/postgresql:18.3-system-trixie
```

### Referencing a Catalog in a Cluster

A `Cluster` resource uses the `imageCatalogRef` to select its images:

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
containers for extensions directly within the catalog entry:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: ImageCatalog
metadata:
  name: postgresql
spec:
  images:
    - major: 18
      image: ghcr.io/cloudnative-pg/postgresql:18.3-minimal-trixie
      extensions:
        - name: foo
          image:
            reference: # registry path for your `foo` extension image
```

The `extensions` section follows the [`ExtensionConfiguration`](cloudnative-pg.v1.md#extensionconfiguration)
API schema and structure.
Clusters referencing an image catalog can load any of its associated extensions
by name.

:::info
Refer to the [documentation of image volume extensions](imagevolume_extensions.md)
for details on the internal image structure, configuration options, and
instructions on how to select or override catalog extensions within a cluster.
:::

## CloudNativePG Catalogs


The CloudNativePG project maintains `ClusterImageCatalog` manifests for all
supported images.

These catalogs are regularly updated and published in two distinct locations
within the [artifacts repository](https://github.com/cloudnative-pg/artifacts/tree/main):

- **[`image-catalogs`](https://github.com/cloudnative-pg/artifacts/tree/main/image-catalogs):**
  core catalog definitions for base image types.

- **[`image-catalogs-extensions`](https://github.com/cloudnative-pg/artifacts/tree/main/image-catalogs-extensions):**
  identical to the above catalogs, with the key difference that the `minimal`
  image type includes extension definitions.

Each catalog corresponds to a specific combination of image type and Debian
release (e.g., `trixie`). It lists the most up-to-date container images for
every supported PostgreSQL major version.

:::important
To ensure maximum security and immutability, all images within official
CloudNativePG catalogs are identified by their **SHA256 digests** rather than
just tags.
:::

### Version Compatibility

While core catalogs work with older versions of the operator, **catalogs
containing an `extensions` section are only compatible with CloudNativePG 1.29
or later**. Using a catalog with extension definitions on an older operator
will result in those definitions being rejected.

### Installation and Usage

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

### Example: Using a Catalog in a Cluster

To create a cluster that always tracks the latest `minimal` image for
PostgreSQL 18 on `trixie`, define your `Cluster` as follows:

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
