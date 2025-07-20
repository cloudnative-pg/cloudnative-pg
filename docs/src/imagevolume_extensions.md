# Image Volume Extensions
<!-- SPDX-License-Identifier: CC-BY-4.0 -->

CloudNativePG supports the **dynamic loading of PostgreSQL extensions** into a
running `Cluster` using the [Kubernetes `ImageVolume` feature](https://kubernetes.io/docs/tasks/configure-pod-container/image-volumes/)
and the `extension_control_path` GUC introduced in PostgreSQL 18, with
contributions from this project.

This feature allows you to mount a [PostgreSQL extension](https://www.postgresql.org/docs/current/extend-extensions.html),
packaged as an OCI-compliant container image, as a read-only and immutable
volume inside a running pod at a known filesystem path.

## Benefits

ImageVolume extensions **decouple the distribution of PostgreSQL operand
container images from the distribution of extensions**. This eliminates the
need to define and embed extensions at build time within your PostgreSQL
images—a major adoption blocker for CloudNativePG, including from a security
and supply chain perspective.

As a result, you can:

- Use the [official PostgreSQL `minimal` operand images](https://github.com/cloudnative-pg/postgres-containers?tab=readme-ov-file#minimal-images)
  provided by CloudNativePG.
- Dynamically add the extensions you need to your `Cluster` definitions,
  without rebuilding or maintaining custom PostgreSQL images.
- Reduce your operational surface by using immutable, minimal, and secure base
  images while adding only the extensions required for each workload.

Extension images must be built according to these
[specifications](#image-specifications).
Once the images are available in the `Cluster`, you can manage the extensions
within your databases using the [`Database` resource’s declarative extension management](declarative_database_management.md/#managing-extensions-in-a-database)
feature.

## Requirements

To use image volume extensions with CloudNativePG, you need:

- **PostgreSQL 18 or later**, with support for `extension_control_path`.
- **Kubernetes 1.33**, with the `ImageVolume` feature gate enabled.
- **CloudNativePG-compatible extension container images**, ensuring:
    - Matching PostgreSQL major version of the `Cluster` resource.
    - Compatible operating system distribution of the `Cluster` resource.
    - Matching CPU architecture of the `Cluster` resource.

## How It Works

Each image volume is mounted at `/extensions/<EXTENSION_NAME>`.

By default, CloudNativePG automatically manages the relevant GUCs, setting:

- `extension_control_path` to `/extensions/<EXTENSION_NAME>/share`, allowing
  PostgreSQL to locate any extension control file within `/extensions/<EXTENSION_NAME>/share/extension`
- `dynamic_library_path` to `/extensions/<EXTENSION_NAME>/lib`

This allows PostgreSQL to discover and load the extension without requiring
manual configuration inside the pod.

!!! Info
    Depending on how your extension container images are built and their layout,
    you may need to adjust the default `extension_control_path` and
    `dynamic_library_path` values to match the image structure.

!!! Important
    The extension container image must match the PostgreSQL container used by
    your cluster in PostgreSQL major version, Operating system distribution, and
    CPU architecture to ensure compatibility and prevent runtime issues.

## Adding a New Extension

You can add an `ImageVolume`-based extension to a `Cluster` using the
`.spec.postgresql.extensions` stanza. For example:

```yaml
spec:
  postgresql:
    extensions:
      - name: pgvector
        image:
          reference: <registry-path-for-pgvector>
          pullPolicy: IfNotPresent
```

The `name` field is **mandatory** and **must be unique within the cluster**, as
it determines the mount path (`/extensions/pgvector` in this example). It must
consist of *lowercase alphanumeric characters or hyphens (`-`)* and must start
and end with an alphanumeric character.

The `image` stanza follows the [Kubernetes `ImageVolume` API](https://kubernetes.io/docs/tasks/configure-pod-container/image-volumes/).
The `reference` must point to a valid container registry path for the extension
image.

!!! important
    When a new extension is added to a running `Cluster`, CloudNativePG will
    automatically trigger a [Rolling Update](rolling_update.md) to attach the new
    image volume to each pod. Before adding a new extension in production,
    ensure you have thoroughly tested it in a staging environment to prevent
    configuration issues that could leave your PostgreSQL cluster in an unhealthy
    state.

Once mounted, CloudNativePG will automatically configure PostgreSQL by appending:

- `/extensions/pgvector/share` to `extension_control_path`
- `/extensions/pgvector/lib` to `dynamic_library_path`



The `CREATE EXTENSION pgvector` command, triggered automatically during the
[reconciliation of the `Database` resource](declarative_database_management.md/#managing-extensions-in-a-database),
will work without additional configuration, as PostgreSQL will locate:

- the extension control file at `/extensions/pgvector/share/extension/vector.control`
- the shared library at `/extensions/pgvector/lib/vector.so`

## Manage extensions via configuration

You can take advantage of Declarative Databases to [manage the lifecycle of
your extensions](declarative_database_management.md#managing-extensions-in-a-database)
in a target database.

TODO

### Setting custom paths

If your extension container image doesn't adhere to the default `lib` and `share`
directories to host the extension's libraries and control files, you can override the
default behavior and set your own custom paths via the `extension_control_path` and
`dynamic_library_path` fields. For example:

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
          # ...
```

This way, the following paths will be configured in PostgreSQL:
- `/extensions/my-extension/my/share/path` for `extension_control_path`
- `/extension/my-extension/my/lib/path` for `dynamic_library_path`

### Multi-extension image

You may have the necessity to include multiple extensions inside the same container image,
for example by adopting a structure where the files of each extension reside in its subdirectory.
The following example demonstrates how to add a GeoSpatial container image that contains both
PostGIS and pgRouting:

```yaml
spec:
  postgresql:
    extensions:
      - name: geospatial
        extension_control_path:
          - postgis/share
          - pgrouting/share
        dynamic_library_path:
          - postgis/lib
          - pgrouting/lib
        image:
          # ...
```

### System Libraries

Some extensions, like PostGIS, require system libraries that may not be included in the default PostgreSQL image.
To support this, these libraries can be packaged within the extension’s container image and made available to
PostgreSQL via the `ld_library_path` field.

In the example below, the `system` directory contains all necessary system libraries for running PostGIS:

```yaml
spec:
  postgresql:
    extensions:
      - name: postgis
        ld_library_path:
          - system
        image:
          # ...
```

This sets the LD_LIBRARY_PATH environment variable for the PostgreSQL process, which will contain
the path `/extensions/postgis/system`, allowing it to locate and load the required libraries.

!!! Important
    Given that `ld_library_path` needs to be set at the start of the PostgreSQL process,
    changing this value requires a restart of the Cluster for the new value(s) to be picked up.
    Currently, this is not being done automatically and users have to issue a
    `cnpg restart` after changing this value on a running Cluster.

## Image Specifications

TODO
