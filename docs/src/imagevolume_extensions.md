# Image Volume Extensions
<!-- SPDX-License-Identifier: CC-BY-4.0 -->

CloudNativePG supports the **dynamic loading of PostgreSQL extensions** into a
running `Cluster` using the [Kubernetes `ImageVolume` feature](https://kubernetes.io/docs/tasks/configure-pod-container/image-volumes/)
and the `extension_control_path` GUC introduced in PostgreSQL 18, to which this
project contributed.

This feature allows you to mount a [PostgreSQL extension](https://www.postgresql.org/docs/current/extend-extensions.html),
packaged as an OCI-compliant container image, as a read-only and immutable
volume inside a running pod at a known filesystem path. You can then make the
extension available to PostgreSQL for the `CREATE EXTENSION` command using the
[`Database` resource’s declarative extension management](declarative_database_management.md/#managing-extensions-in-a-database).

## Benefits

ImageVolume extensions **decouple the distribution of PostgreSQL operand
container images from the distribution of extensions**. This eliminates the
need to define and embed extensions at build time within your PostgreSQL
images—a major adoption blocker for PostgreSQL as a containerized workload,
including from a security and supply chain perspective.

As a result, you can:

- Use the [official PostgreSQL `minimal` operand images](https://github.com/cloudnative-pg/postgres-containers?tab=readme-ov-file#minimal-images)
  provided by CloudNativePG.
- Dynamically add the extensions you need to your `Cluster` definitions,
  without rebuilding or maintaining custom PostgreSQL images.
- Reduce your operational surface by using immutable, minimal, and secure base
  images while adding only the extensions required for each workload.

Extension images must be built according to the
[documented specifications](#image-specifications).

## Requirements

To use image volume extensions with CloudNativePG, you need:

- **PostgreSQL 18 or later**, with support for `extension_control_path`.
- **Kubernetes 1.33**, with the `ImageVolume` feature gate enabled.
- **CloudNativePG-compatible extension container images**, ensuring:
    - Matching PostgreSQL major version of the `Cluster` resource.
    - Compatible operating system distribution of the `Cluster` resource.
    - Matching CPU architecture of the `Cluster` resource.

## How it works

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

## How to add a new extension

Adding an extension to a database in CloudNativePG involves two steps:

1. Attach the extension image to the `Cluster` resource so that PostgreSQL can
   discover and load it.
2. Declare the extension in the `Database` resource where you want it
   installed.

For illustration, we will use a fictitious extension named `bozzone`.

### Adding a new extension to a `Cluster` resource

You can add an `ImageVolume`-based extension to a `Cluster` using the
`.spec.postgresql.extensions` stanza. For example:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: bozzone-18
spec:
  # ...
  postgresql:
    extensions:
      - name: bozzone
        image:
          reference: <registry-path-for-bozzone>
          pullPolicy: IfNotPresent
  # ...
```

The `name` field is **mandatory** and **must be unique within the cluster**, as
it determines the mount path (`/extensions/bozzone` in this example). It must
consist of *lowercase alphanumeric characters or hyphens (`-`)* and must start
and end with an alphanumeric character.

The `image` stanza follows the [Kubernetes `ImageVolume` API](https://kubernetes.io/docs/tasks/configure-pod-container/image-volumes/).
The `reference` must point to a valid container registry path for the extension
image.

!!! Important
    When a new extension is added to a running `Cluster`, CloudNativePG will
    automatically trigger a [rolling update](rolling_update.md) to attach the new
    image volume to each pod. Before adding a new extension in production,
    ensure you have thoroughly tested it in a staging environment to prevent
    configuration issues that could leave your PostgreSQL cluster in an unhealthy
    state.

Once mounted, CloudNativePG will automatically configure PostgreSQL by appending:

- `/extensions/bozzone/share` to `extension_control_path`
- `/extensions/bozzone/lib` to `dynamic_library_path`

This ensures that the PostgreSQL container is ready to serve the `bozzone`
extension when requested by a database, as described in the next section. The
`CREATE EXTENSION bozzone` command, triggered automatically during the
[reconciliation of the `Database` resource](declarative_database_management.md/#managing-extensions-in-a-database),
will work without additional configuration, as PostgreSQL will locate:

- the extension control file at `/extensions/bozzone/share/extension/vector.control`
- the shared library at `/extensions/bozzone/lib/vector.so`

### Adding a new extension to a `Database` resource

Once the extension is available in the PostgreSQL instance, you can leverage
declarative databases to [manage the lifecycle of your extensions](declarative_database_management.md#managing-extensions-in-a-database)
within the target database.

Continuing with the `bozzone` example, you can request the installation of the
`bozzone` extension in the `app` database of the `bozzone-18` cluster using the
following resource definition:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Database
metadata:
  name: bozzone-app
spec:
  name: app
  owner: app
  cluster:
    name: bozzone-18
  extensions:
    - name: bozzone
```

CloudNativePG will automatically reconcile this resource, executing the
`CREATE EXTENSION bozzone` command inside the `app` database if it is not
already installed, ensuring your desired state is maintained without manual
intervention.

## Advanced topics

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

### System libraries

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

This sets the `LD_LIBRARY_PATH` environment variable for the PostgreSQL process, which will contain
the path `/extensions/postgis/system`, allowing it to locate and load the required libraries.

!!! Important
    Given that `ld_library_path` needs to be set at the start of the PostgreSQL process,
    changing this value requires a restart of the Cluster for the new value(s) to be picked up.
    Currently, this is not being done automatically and users have to issue a
    `cnpg restart` after changing this value on a running Cluster.

## Image Specifications

TODO

## Caveats

- Rolling Updates
- Extension upgrades
