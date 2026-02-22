---
id: image_catalog
sidebar_position: 70
title: Image Catalog
---

# Image Catalog
<!-- SPDX-License-Identifier: CC-BY-4.0 -->

`ImageCatalog` and `ClusterImageCatalog` are essential resources that empower
you to define images for creating a `Cluster`.

The key distinction lies in their scope: an `ImageCatalog` is namespaced, while
a `ClusterImageCatalog` is cluster-scoped.

Both share a common structure, comprising a list of images, each equipped with
a `major` field indicating the major version of the image.

:::warning
    The operator places trust in the user-defined major version and refrains
    from conducting any PostgreSQL version detection. It is the user's
    responsibility to ensure alignment between the declared major version in
    the catalog and the PostgreSQL image.
:::

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
      image: ghcr.io/cloudnative-pg/postgresql:15.14-system-trixie
    - major: 16
      image: ghcr.io/cloudnative-pg/postgresql:16.10-system-trixie
    - major: 17
      image: ghcr.io/cloudnative-pg/postgresql:17.6-system-trixie
    - major: 18
      image: ghcr.io/cloudnative-pg/postgresql:18.2-system-trixie
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
      image: ghcr.io/cloudnative-pg/postgresql:15.14-system-trixie
    - major: 16
      image: ghcr.io/cloudnative-pg/postgresql:16.10-system-trixie
    - major: 17
      image: ghcr.io/cloudnative-pg/postgresql:17.6-system-trixie
    - major: 18
      image: ghcr.io/cloudnative-pg/postgresql:18.2-system-trixie
```

A `Cluster` resource has the flexibility to reference either an `ImageCatalog`
(like in the following example) or a `ClusterImageCatalog` to precisely specify
the desired image.

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-example
spec:
  instances: 3
  imageCatalogRef:
    apiGroup: postgresql.cnpg.io
    # Change the following to `ClusterImageCatalog` if needed
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
