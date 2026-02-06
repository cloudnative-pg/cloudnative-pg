---
id: imagevolume_extensions
sidebar_position: 470
title: Image Volume Extensions
---

# Image Volume Extensions
<!-- SPDX-License-Identifier: CC-BY-4.0 -->

CloudNativePG supports the **dynamic loading of PostgreSQL extensions** into a
`Cluster` at Pod startup using the [Kubernetes `ImageVolume` feature](https://kubernetes.io/docs/tasks/configure-pod-container/image-volumes/)
alongside the `extension_control_path` GUC introduced in PostgreSQL 18, a
feature to which the CloudNativePG project contributed.

This feature allows you to mount a [PostgreSQL extension](https://www.postgresql.org/docs/current/extend-extensions.html),
packaged as an OCI-compliant container image, as a read-only and immutable
volume at a designated filesystem path within a running pod.

For extensions requiring database-level installation via the `CREATE EXTENSION`
command, you can use the [`Database` resource’s declarative extension management](declarative_database_management.md#managing-extensions-in-a-database)
to ensure consistent, automated setup across all your PostgreSQL databases.

## Official Extension Images and Catalogs

The CloudNativePG Community maintains a suite of extension container
images, including
[pgvector](https://github.com/cloudnative-pg/postgres-extensions-containers/tree/main/pgvector)
and
[PostGIS](https://github.com/cloudnative-pg/postgres-extensions-containers/tree/main/postgis),
as part of the [`postgres-extensions-containers` project](https://github.com/cloudnative-pg/postgres-extensions-containers)).
These images are built on top of the
[official PostgreSQL `minimal` images](https://github.com/cloudnative-pg/postgres-containers?tab=readme-ov-file#minimal-images).

While this documentation provides the necessary technical specifications for
third parties to build their own images and catalogs, the following
instructions focus specifically on the deployment and usage of our official
extension images and catalogs.

## Benefits

By decoupling the distribution of extensions from the PostgreSQL operand
images, this feature removes a significant barrier to running PostgreSQL in
containers. It eliminates the need to embed extensions at build time, allowing
you to use official minimal operand images and dynamically add only the
required extensions to your `Cluster` definitions—either directly or via an
image catalog.

This approach significantly reduces the attack surface of your database
clusters by ensuring that the core database container contains only the
essential binaries required for operation.
By excluding unnecessary extensions, libraries, and build-time dependencies,
you minimize potential entry points for exploits and simplify vulnerability
management.
This architecture enhances supply chain security and reduces operational
overhead by maintaining an immutable, minimal base image for your data
workloads.

:::important
Extension images must be built according to the [documented specifications](#image-specifications).
:::

## Requirements

To use image volume extensions with CloudNativePG, you need:

- **PostgreSQL 18 or later**: Required for `extension_control_path` support.
- **Kubernetes 1.35 or later**: The `ImageVolume` feature is enabled by
  default. Users on Kubernetes 1.33 and 1.34 must manually enable the
  `ImageVolume` feature gate.
- **Container runtime with `ImageVolume` support**:
    - `containerd` v2.1.0 or later, or
    - `CRI-O` v1.31 or later.
- **CloudNativePG-compatible extension container images**, ensuring:
    - Matching PostgreSQL major version of the `Cluster` resource.
    - Compatible operating system distribution of the `Cluster` resource.
    - Matching CPU architecture of the `Cluster` resource.

## How it works

An extension image can be added to a new or existing `Cluster` resource using
the `.spec.postgresql.extensions` stanza.

:::info
    For field-level details, see the
    [API reference for `ExtensionConfiguration`](cloudnative-pg.v1.md#extensionconfiguration).
:::

The `extensions` stanza accepts a list of extensions to be added to the
PostgreSQL cluster. Each entry provides the configuration for a container image
to be loaded as a read-only volume, as well as the options that allow PostgreSQL
to locate and load the extension. Each image volume is mounted at
`/extensions/<EXTENSION_NAME>` inside the pod.

By default, CloudNativePG automatically manages the relevant GUCs, setting:

- `extension_control_path` to `/extensions/<EXTENSION_NAME>/share`, allowing
  PostgreSQL to locate any extension control file within `/extensions/<EXTENSION_NAME>/share/extension`
- `dynamic_library_path` to `/extensions/<EXTENSION_NAME>/lib`

These values are appended in the order in which the extensions are defined in
the `extensions` list, ensuring deterministic path resolution within
PostgreSQL. This allows PostgreSQL to discover and load the extension without
requiring manual configuration inside the pod.

:::info
    Depending on how your extension container images are built and their layout,
    you may need to adjust the default `extension_control_path` and
    `dynamic_library_path` values to match the image structure.
:::

:::info[Important]
    If the extension image includes shared libraries, they must be compiled
    with the same PostgreSQL major version, operating system distribution, and CPU
    architecture as the PostgreSQL container image used by your cluster, to ensure
    compatibility and prevent runtime issues.
:::

## How to add a new extension

Adding an extension to a database in CloudNativePG involves a few steps:

1. Define the extension image in the `Cluster` resource so that PostgreSQL can
   discover and load it.
2. Add the library to [`shared_preload_libraries`](postgresql_conf.md#shared-preload-libraries)
   if the extension requires it.
3. Declare the extension in the `Database` resource where you want it
   installed, if the extension supports `CREATE EXTENSION`.

:::warning
    Avoid making changes to extension images and PostgreSQL configuration
    settings (such as `shared_preload_libraries`) simultaneously.
    First, allow the pod to roll out with the new extension image, then update
    the PostgreSQL configuration.
    This limitation will be addressed in a future release of CloudNativePG.
:::

For illustration purposes, this guide uses a simple, fictitious extension named
`foo` that supports `CREATE EXTENSION`.

### Adding a new extension to a `Cluster` resource

You can add an `ImageVolume`-based extension to a `Cluster` using the
`.spec.postgresql.extensions` stanza. For example:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: foo-18
spec:
  # ...
  postgresql:
    extensions:
      - name: foo
        image:
          reference: # registry path for your extension image
  # ...
```

The `name` field is **mandatory** and **must be unique within the cluster**, as
it determines the mount path (`/extensions/foo` in this example). It must
consist of *lowercase alphanumeric characters, underscores (`_`) or hyphens (`-`)* and must start
and end with an alphanumeric character.

:::note
Extension names containing underscores (e.g., `pg_ivm`) are converted to use
hyphens (e.g., `pg-ivm`) for Kubernetes volume names to comply with RFC 1123
DNS label requirements. Do not use extension names that become identical after
sanitization (e.g., `pg_ivm` and `pg-ivm` both sanitize to `pg-ivm`). The
webhook validation will prevent such conflicts.
:::

The `image` stanza follows the [Kubernetes `ImageVolume` API](https://kubernetes.io/docs/tasks/configure-pod-container/image-volumes/).
The `reference` must point to a valid container registry path for the extension
image.

:::info[Important]
    When a new extension is added to a running `Cluster`, CloudNativePG will
    automatically trigger a [rolling update](rolling_update.md) to attach the new
    image volume to each pod. Before adding a new extension in production,
    ensure you have thoroughly tested it in a staging environment to prevent
    configuration issues that could leave your PostgreSQL cluster in an unhealthy
    state.
:::

Once mounted, CloudNativePG will automatically configure PostgreSQL by appending:

- `/extensions/foo/share` to `extension_control_path`
- `/extensions/foo/lib` to `dynamic_library_path`

This ensures that the PostgreSQL container is ready to serve the `foo`
extension when requested by a database, as described in the next section. The
`CREATE EXTENSION foo` command, triggered automatically during the
[reconciliation of the `Database` resource](declarative_database_management.md#managing-extensions-in-a-database),
will work without additional configuration, as PostgreSQL will locate:

- the extension control file at `/extensions/foo/share/extension/foo.control`
- the shared library at `/extensions/foo/lib/foo.so`

:::note
If the extension requires one or more shared libraries to be pre-loaded at
server start, you can add them via the [`shared_preload_libraries` option](postgresql_conf.md#shared-preload-libraries).
:::

### Adding a new extension defined via `Image Catalog` to a `Cluster` resource

`ImageVolume` extension can also be defined in the `.spec.images[].extensions` stanza
of an `ImageCatalog` or `ClusterImageCatalog`.
To add extensions to a catalog, please refer to [Image Catalog with Image Volume Extensions](image_catalog.md#image-catalog-with-image-volume-extensions).

Clusters that reference a catalog image for which extensions are defined can request any of
those extensions to be loaded into the `Cluster`.

**Example: enabling catalog-defined extensions:**
Given an `ImageCatalog` that defines the extensions `foo` and `bar`
for the PostgreSQL major version `18`:

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
        - name: bar
          image:
            reference: # registry path for your `bar` extension image
```

You can enable one or more of these extensions inside a `Cluster` by referencing the extension's `name`
under `.spec.postgresql.extensions`.
For example, to enable only the `foo` extension:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-example
spec:
  imageCatalogRef:
    apiGroup: postgresql.cnpg.io
    kind: ImageCatalog
    name: postgresql
    major: 18

  postgresql:
    extensions:
      - name: foo
```

**Example: combining catalog-defined and cluster-defined extensions:**
You can define additional extensions directly at the `Cluster` level alongside extensions coming
from the referenced catalog. For example:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-example
spec:
  imageCatalogRef:
    apiGroup: postgresql.cnpg.io
    kind: ImageCatalog
    name: postgresql
    major: 18

  postgresql:
    extensions:
      - name: foo
      - name: baz
        image:
          reference: # registry path for your `baz` extension image
```

In this case:
* `foo` is loaded from the catalog definition
* `baz` is defined entirely at the Cluster level

**Example: overriding catalog-defined extensions:**

Any configuration field of an extension defined in the catalog can be overridden directly
in the `Cluster` object.

When both the catalog and the `Cluster` define an extension with the same `name`, the
`Cluster` configuration takes precedence.
This allows the catalog to act as a base layer
of configuration that can be selectively overridden by individual clusters.

For example, you can request the `bar` extension from the catalog while overriding its
configuration to set a custom `extension_control_path`:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-example
spec:
  imageCatalogRef:
    apiGroup: postgresql.cnpg.io
    kind: ImageCatalog
    name: postgresql
    major: 18

  postgresql:
    extensions:
      - name: foo
      - name: baz
        image:
          reference: # registry path for your `baz` extension image
      - name: bar
        extension_control_path:
          - my/bar/custom/path
```

In this example, the bar extension is loaded using the `image.reference` defined in the catalog, while
its `extension_control_path` is overridden at the Cluster level with a custom value.

Any field of an [`ExtensionConfiguration`](cloudnative-pg.v1.md#extensionconfiguration) can be
overridden at `Cluster` level, with the exception of `name`.
Changing the `name` results in defining a separate extension rather than overriding an
existing one.

### Adding a new extension to a `Database` resource

Once the extension is available in the PostgreSQL instance, you can leverage
declarative databases to [manage the lifecycle of your extensions](declarative_database_management.md#managing-extensions-in-a-database)
within the target database.

Continuing with the `foo` example, you can request the installation of the
`foo` extension in the `app` database of the `foo-18` cluster using the
following resource definition:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Database
metadata:
  name: foo-app
spec:
  name: app
  owner: app
  cluster:
    name: foo-18
  extensions:
    - name: foo
      version: 1.0
```

CloudNativePG will automatically reconcile this resource, executing the
`CREATE EXTENSION foo` command inside the `app` database if it is not
already installed, ensuring your desired state is maintained without manual
intervention.

## Advanced Topics

In some cases, the default expected structure may be insufficient for your
extension image, particularly when:

- The extension requires additional system libraries.
- Multiple extensions are bundled in the same image.
- The image uses a custom directory structure.

Following the *"convention over configuration"* paradigm, CloudNativePG allows
you to finely control the configuration of each extension image through the
following fields:

- `extension_control_path`: A list of relative paths within the container image
  to be appended to PostgreSQL’s `extension_control_path`, allowing it to
  locate extension control files.
- `dynamic_library_path`: A list of relative paths within the container image
  to be appended to PostgreSQL’s `dynamic_library_path`, enabling it to locate
  shared library files for extensions.
- `ld_library_path`: A list of relative paths within the container image to be
  appended to the `LD_LIBRARY_PATH` environment variable of the instance
  manager process, allowing PostgreSQL to locate required system libraries at
  runtime.

This flexibility enables you to support complex or non-standard extension
images while maintaining clarity and predictability.

### Setting Custom Paths

If your extension image does not use the default `lib` and `share` directories
for its libraries and control files, you can override the defaults by
explicitly setting `extension_control_path` and `dynamic_library_path`.

For example:

```yaml
spec:
  postgresql:
    extensions:
      - name: my-extension
        extension_control_path:
          - my/share/path
        dynamic_library_path:
          - my/lib/path
        image:
          reference: # registry path for your extension image
```

CloudNativePG will configure PostgreSQL with:

- `/extensions/my-extension/my/share/path` appended to `extension_control_path`
- `/extensions/my-extension/my/lib/path` appended to `dynamic_library_path`

This allows PostgreSQL to discover your extension’s control files and shared
libraries correctly, even with a non-standard layout.

### Multi-extension Images

You may need to include multiple extensions within the same container image,
adopting a structure where each extension’s files reside in their own
subdirectory.

For example, to package PostGIS and pgRouting together in a single image, each
in its own subdirectory:

```yaml
# ...
spec:
  # ...
  postgresql:
    extensions:
      - name: geospatial
        extension_control_path:
          - postgis/share
          - pgrouting/share
        dynamic_library_path:
          - postgis/lib
          - pgrouting/lib
        # ...
        image:
          reference: # registry path for your geospatial image
      # ...
    # ...
  # ...
```

### Including System Libraries

Some extensions, such as PostGIS, require system libraries that may not be
present in the base PostgreSQL image. To support these requirements, you can
package the necessary libraries within your extension container image and make
them available to PostgreSQL using the `ld_library_path` field.

For example, if your extension image includes a `system` directory with the
required libraries:

```yaml
# ...
spec:
  # ...
  postgresql:
    extensions:
      - name: postgis
        # ...
        ld_library_path:
          - system
        image:
          reference: # registry path for your PostGIS image
      # ...
    # ...
  # ...
```

CloudNativePG will set the `LD_LIBRARY_PATH` environment variable to include
`/extensions/postgis/system`, allowing PostgreSQL to locate and load these
system libraries at runtime.

:::info[Important]
    Since `ld_library_path` must be set when the PostgreSQL process starts,
    changing this value requires a **cluster restart** for the new value to take effect.
    CloudNativePG does not currently trigger this restart automatically; you will need to
    manually restart the cluster (e.g., using `cnpg restart`) after modifying `ld_library_path`.
:::

## Inspecting the Cluster's Extensions Status

The `Cluster` status includes a dedicated section for `ImageVolume` Extensions:

```yaml
status:

  <- snipped ->
  pgDataImageInfo:
    image: # registry path for your PostgreSQL image
    majorVersion: 18
    extensions:
    - name: foo
      image:
        reference: # registry path for your `foo` extension image
    - name: bar
      image:
        reference: # registry path for your `bar` extension image
```

This section is particularly useful when extensions are defined through an image
catalog, when catalog-defined and Cluster-defined extensions are combined, or
when catalog-defined extensions are overridden at the Cluster level.

The `pgDataImageInfo.extensions` field shows the fully resolved configuration of
all `ImageVolume` Extensions. This is the effective configuration that the operator
uses to provision and configure extensions for the Cluster's instances.

## Image Specifications

A standard extension container image for CloudNativePG includes two
required directories at its root:

- `/share/`: contains an `extension` subdirectory with the extension control
  file (e.g. `<EXTENSION>.control`) and the corresponding SQL files.
- `/lib/`: contains the extension’s shared library (e.g. `<EXTENSION>.so`) as
  well as any other required libraries.

Following this structure ensures that the extension will be automatically
discoverable and usable by PostgreSQL within CloudNativePG without requiring
manual configuration.

:::info[Important]
    We encourage PostgreSQL extension developers to publish OCI-compliant extension
    images following this layout as part of their artifact distribution, making
    their extensions easily consumable within Kubernetes environments.
    Ideally, extension images should target a specific operating system
    distribution and architecture, be tied to a particular PostgreSQL version, and
    be built using the distribution’s native packaging system (for example, using
    Debian or RPM packages). This approach ensures consistency, security, and
    compatibility with the PostgreSQL images used in your clusters.
:::

## Caveats

Currently, adding, removing, or updating an extension image triggers a
restart of the PostgreSQL pods. This behavior is inherited from how
[image volumes](https://kubernetes.io/docs/tasks/configure-pod-container/image-volumes/)
work in Kubernetes.

Before performing an extension update, ensure you have:

- Thoroughly tested the update process in a staging environment.
- Verified that the extension image contains the required upgrade path between
  the currently installed version and the target version.
- Updated the `version` field for the extension in the relevant `Database`
  resource definition to align with the new version in the image.

These steps help prevent downtime or data inconsistencies in your PostgreSQL
clusters during extension updates.
