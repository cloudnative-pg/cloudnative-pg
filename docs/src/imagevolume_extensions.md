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

:::info
While this documentation provides the necessary technical specifications for
third parties to build their own images and catalogs, the following
instructions focus specifically on the deployment and usage of our official
extension images and catalogs.
:::

## Benefits

By decoupling the distribution of extensions from the PostgreSQL operand
images, this feature removes a significant barrier to running PostgreSQL in
containers. It eliminates the need to embed extensions at build time, allowing
you to use official minimal operand images and dynamically add only the
required extensions to your `Cluster` definitions—either directly or via an
[image catalog](image_catalog.md).

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

:::important
    When a new extension is added to a running `Cluster`, CloudNativePG will
    automatically trigger a [rolling update](rolling_update.md) to attach the new
    image volume to each pod. Before adding a new extension in production,
    ensure you have thoroughly tested it in a staging environment to prevent
    configuration issues that could leave your PostgreSQL cluster in an unhealthy
    state.
:::

:::info
    For field-level details, see the
    [API reference for `ExtensionConfiguration`](cloudnative-pg.v1.md#extensionconfiguration).
:::

### Configuration Source and Precedence

The `extensions` stanza accepts a list of entries, each requiring a `name` that
must be unique within the cluster.

:::important
The `name` must consist of lowercase alphanumeric characters, underscores (`_`)
or hyphens (`-`) and must start and end with an alphanumeric character.
:::

Each entry defines the configuration for a container image and specifies the
options PostgreSQL needs to locate and load the extension:

- [**Via Image Catalog**](#via-an-image-catalog-recommended):
  If the cluster references an `ImageCatalog` that defines extensions for the
  current PostgreSQL major version, those definitions serve as the default
  configuration. This allows the catalog to centrally manage the
  `image.reference`, while the cluster simply enables the extension by name.

- [**Direct Definition**](#directly-in-the-cluster):
  If a cluster does not use an image catalog, the `image.reference` field is
  mandatory and must explicitly point to a valid container registry path for the
  extension image. The `image` stanza follows the
  [Kubernetes `ImageVolume` API](https://kubernetes.io/docs/concepts/storage/volumes/#image).

Following the *"convention over configuration"* paradigm, CloudNativePG
provides total flexibility: any value inherited from a catalog, including the
`image.reference`, can be selectively overridden within the `Cluster`
definition to meet specific local requirements.


### Mounting and PostgreSQL Configuration

Each extension is mounted as a read-only volume at
`/extensions/<EXTENSION_NAME>` inside the pod.

By default, CloudNativePG automatically manages the relevant GUCs by setting:

- `extension_control_path` to `/extensions/<EXTENSION_NAME>/share`, allowing
  PostgreSQL to locate any extension control file within `/extensions/<EXTENSION_NAME>/share/extension`
- `dynamic_library_path` to `/extensions/<EXTENSION_NAME>/lib`

:::note
Extension names containing underscores (e.g., `pg_ivm`) are converted to use
hyphens (e.g., `pg-ivm`) for Kubernetes volume names to comply with RFC 1123
DNS label requirements. Do not use extension names that become identical after
sanitization (e.g., `pg_ivm` and `pg-ivm` both sanitize to `pg-ivm`). The
webhook validation will prevent such conflicts.
:::

These values are appended in the order in which the extensions are defined in
the `extensions` list, ensuring deterministic path resolution within
PostgreSQL. This allows PostgreSQL to discover and load the extension without
requiring manual configuration inside the pod.

While you can manually adjust these paths to match a custom image layout,
official CloudNativePG catalogs pre-configure these options to the correct
values for each extension, ensuring they work out-of-the-box.

:::important
If an extension image includes shared libraries, they must be compiled for the
same PostgreSQL major version, operating system distribution, and CPU
architecture as the operand image. Using official CloudNativePG catalogs
ensures this compatibility automatically: the catalogs are designed to match
the specific environment of the cluster, preventing runtime issues caused by
library or architecture mismatches.
:::

### Installation in a Database

You can leverage [declarative database management](declarative_database_management#managing-extensions-in-a-database)
to automate the final activation of any extension that supports the standard
PostgreSQL `CREATE EXTENSION` mechanism.

Once the extension image is mounted and the GUCs are configured, the extension
is available to the PostgreSQL instance but is not yet active within a specific
database. To install it, define a `Database` resource. The example below uses
`pgvector`, following the implementation standards of the
[official `postgres-extensions-containers` project](https://github.com/cloudnative-pg/postgres-extensions-containers/tree/main/pgvector):

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Database
metadata:
  name: cluster-example-app
spec:
  name: app
  owner: app
  cluster:
    name: cluster-example
  extensions:
    - name: vector
      version: "<VERSION>"
```

CloudNativePG automatically reconciles this resource by executing
`CREATE EXTENSION IF NOT EXISTS vector` within the target database. This
ensures your desired state is maintained and consistently applied across all
instances.

:::note
Some PostgreSQL components, often referred to as modules, do not use the
`CREATE EXTENSION` mechanism. These typically consist of shared libraries that
must be loaded via `shared_preload_libraries` at server start.
Even when using an image catalog to simplify the distribution of these
binaries, you must still manually add the library name to the
[`shared_preload_libraries` option](postgresql_conf.md#shared-preload-libraries)
in your `Cluster` definition to ensure it is loaded into memory.
:::

## Adding an Extension to a Postgres Cluster

As anticipated earlier, CloudNativePG offers two ways to define extension
images. While both achieve the same result at the Pod level, using an image
catalog is the recommended approach for maintaining a consistent,
production-ready supply chain.

### Via an Image Catalog (Recommended)

:::info
Support for extension container images in image catalogs was introduced in
CloudNativePG 1.29.
:::

When you use an [image catalog that covers extensions](image_catalog.md#image-catalog-with-image-volume-extensions)
like the [official ones](image_catalog.md#cloudnativepg-catalogs)
provided by the community, the complex configuration details, such as the
specific container image `reference` and the required filesystem paths, are
managed centrally.

Your `Cluster` definition remains clean and declarative, as it only needs to
"opt-in" to the extension by name. The operator automatically handles the
resolution of the image and the required PostgreSQL settings based on the
catalog's definitions.

To enable an extension like `pgvector` from a catalog, add it to the
`extensions` list in your cluster like in the following excerpt:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-example
spec:
  # ... <snip>
  imageCatalogRef:
    apiGroup: postgresql.cnpg.io
    kind: ClusterImageCatalog
    name: postgresql-minimal-trixie
    major: 18

  postgresql:
    extensions:
      - name: pgvector # Resolves all details, including the image reference, from the catalog
      # ... <snip>
```

This method ensures that the extension image is always compatible with the
PostgreSQL operand image defined in the same catalog entry.

### Directly in the Cluster

:::info
Defining extensions directly in the `Cluster` resource is the original method
and remains the only option for versions prior to CloudNativePG 1.29.
It is also useful if you need to use an extension not present in your current
catalog or for testing custom images.
:::

You can define an extension directly within the `Cluster` resource without a
catalog. In this case, you must explicitly provide the image `reference`.

The following example shows how to add `pgvector` by explicitly pointing to its
official container image:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-example
spec:
  # ... <snip>
  postgresql:
    extensions:
      - name: pgvector
        image:
          reference: ghcr.io/cloudnative-pg/pgvector:<TAG>
```

:::tip
Remember that configuration provided directly in the `Cluster` takes
precedence. If you reference a catalog but also define the same extension name
in the `Cluster` stanza, the settings in the `Cluster` will override those in
the catalog.
While every field in an [`ExtensionConfiguration`](cloudnative-pg.v1.md#extensionconfiguration)
can be overridden at the `Cluster` level to provide total flexibility, the
`name` field is the exception.
:::

:::warning
The `name` serves as the unique identifier; changing it will define a new
extension entry rather than overriding an existing one from a catalog.
:::

### Verifying Extension Status

Because extensions can be sourced from both image catalogs and direct cluster
definitions, CloudNativePG provides a "resolved" view of the configuration in
the `Cluster` status. This is the final, effective state the operator uses to
provision the pods.

You can inspect the `status.pgDataImageInfo.extensions` field to see the
results of any inheritance or overrides:

```yaml
# ... <snip>
status:
  # ... <snip>
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
    # ... <snip>
```

This section is particularly useful for:

- **Validating Resolution:** Confirming that an extension requested by name in
  the `Cluster` has correctly pulled its `image.reference` from the associated
  catalog.
- **Confirming Overrides:** Verifying that a cluster-level override (such as a
  specific image version) has successfully replaced the catalog's default
  value.
- **Troubleshooting:** Ensuring all desired extensions are recognized by the
  operator before the pods undergo a rolling update.

## Removing an Extension from a PostgreSQL Cluster

Removing an extension involves a two-step process to clean up both the
infrastructure and the database metadata.

You should first remove the extension from the database to avoid "library not
found" errors. If you are using declarative management, update your `Database`
resource by setting `ensure: absent` for the extension:

```yaml
spec:
  # ... <snip>
  extensions:
    - name: pgvector
      ensure: absent
    # ... <snip>
```

This triggers CloudNativePG to execute `DROP EXTENSION` within the database.

If the extension was added to `shared_preload_libraries`, you must also remove
it from your `Cluster` configuration.

Then, remove the extension entry from the `.spec.postgresql.extensions` list in
your `Cluster` resource. The operator will perform a rolling update to detach
the `ImageVolume` and update the relevant GUC paths.

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
  # ... <snip>
  postgresql:
    extensions:
      - name: my-extension
        extension_control_path:
          - my/share/path
        dynamic_library_path:
          - my/lib/path
        image:
          reference: # registry path for your extension image
      # ... <snip>
    # ... <snip>
  # ... <snip>
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
  # ... <snip>
  postgresql:
    extensions:
      - name: geospatial
        extension_control_path:
          - postgis/share
          - pgrouting/share
        dynamic_library_path:
          - postgis/lib
          - pgrouting/lib
        # ... <snip>
        image:
          reference: # registry path for your geospatial image
      # ... <snip>
    # ... <snip>
  # ... <snip>
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
  # ... <snip>
  postgresql:
    extensions:
      - name: postgis
        # ... <snip>
        ld_library_path:
          - system
        image:
          reference: # registry path for your PostGIS image
      # ... <snip>
    # ... <snip>
  # ... <snip>
```

CloudNativePG will set the `LD_LIBRARY_PATH` environment variable to include
`/extensions/postgis/system`, allowing PostgreSQL to locate and load these
system libraries at runtime.

:::important
Since `ld_library_path` must be set when the PostgreSQL process starts,
changing this value requires a **cluster restart** for the new value to take
effect.
CloudNativePG does not currently trigger this restart automatically; you will
need to manually restart the cluster (e.g., using `cnpg restart`) after
modifying `ld_library_path`.
:::

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

:::important
We encourage PostgreSQL extension developers and third-party providers to
publish OCI-compliant extension images following this layout.
For practical implementation details, we recommend reviewing the
[`postgres-extensions-containers` project](https://github.com/cloudnative-pg/postgres-extensions-containers),
which serves as the reference for building official CloudNativePG extension images.

Ideally, extension images should:

- Target a specific operating system distribution and set of CPU architectures.
- Be tied to a particular PostgreSQL major version.
- Be built using the distribution’s native packaging system (e.g., `.deb` or
  `.rpm` packages) to ensure consistency, security, and compatibility with the
  PostgreSQL operand images used in your clusters.
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
