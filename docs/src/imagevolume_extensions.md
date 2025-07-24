# Image Volume Extensions
<!-- SPDX-License-Identifier: CC-BY-4.0 -->

CloudNativePG supports the **dynamic loading of PostgreSQL extensions** into a
`Cluster` at Pod startup using the [Kubernetes `ImageVolume` feature](https://kubernetes.io/docs/tasks/configure-pod-container/image-volumes/)
and the `extension_control_path` GUC introduced in PostgreSQL 18, to which this
project contributed.

This feature allows you to mount a [PostgreSQL extension](https://www.postgresql.org/docs/current/extend-extensions.html),
packaged as an OCI-compliant container image, as a read-only and immutable
volume inside a running pod at a known filesystem path.

You can make the extension available either globally, using the
[`shared_preload_libraries` option](postgresql_conf.md#shared-preload-libraries),
or at the database level through the `CREATE EXTENSION` command. For the
latter, you can use the [`Database` resource’s declarative extension management](declarative_database_management.md/#managing-extensions-in-a-database)
to ensure consistent, automated extension setup within your PostgreSQL
databases.

## Benefits

Image volume extensions decouple the distribution of PostgreSQL operand
container images from the distribution of extensions. This eliminates the
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

Extension images are defined in the `.spec.postgresql.extensions` stanza of a
`Cluster` resource, which accepts an ordered list of extensions to be added to
the PostgreSQL cluster.

!!! Info
    For field-level details, see the
    [API reference for `ExtensionConfiguration`](cloudnative-pg.v1.md#postgresql-cnpg-io-v1-ExtensionConfiguration).

Each image volume is mounted at `/extensions/<EXTENSION_NAME>`.

By default, CloudNativePG automatically manages the relevant GUCs, setting:

- `extension_control_path` to `/extensions/<EXTENSION_NAME>/share`, allowing
  PostgreSQL to locate any extension control file within `/extensions/<EXTENSION_NAME>/share/extension`
- `dynamic_library_path` to `/extensions/<EXTENSION_NAME>/lib`

These values are appended in the order in which the extensions are defined in
the `extensions` list, ensuring deterministic path resolution within
PostgreSQL. This allows PostgreSQL to discover and load the extension without
requiring manual configuration inside the pod.

!!! Info
    Depending on how your extension container images are built and their layout,
    you may need to adjust the default `extension_control_path` and
    `dynamic_library_path` values to match the image structure.

!!! Important
    If the extension image includes shared libraries, they must be compiled
    with the same PostgreSQL major version, operating system distribution, and CPU
    architecture as the PostgreSQL container image used by your cluster, to ensure
    compatibility and prevent runtime issues.

## How to add a new extension

Adding an extension to a database in CloudNativePG involves a few steps:

1. Define the extension image in the `Cluster` resource so that PostgreSQL can
   discover and load it.
2. Add the library to [`shared_preload_libraries`](postgresql_conf.md#shared-preload-libraries)
   if the extension requires it.
3. Declare the extension in the `Database` resource where you want it
   installed, if the extension supports `CREATE EXTENSION`.

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

- `/extensions/foo/share` to `extension_control_path`
- `/extensions/foo/lib` to `dynamic_library_path`

This ensures that the PostgreSQL container is ready to serve the `foo`
extension when requested by a database, as described in the next section. The
`CREATE EXTENSION foo` command, triggered automatically during the
[reconciliation of the `Database` resource](declarative_database_management.md/#managing-extensions-in-a-database),
will work without additional configuration, as PostgreSQL will locate:

- the extension control file at `/extensions/foo/share/extension/foo.control`
- the shared library at `/extensions/foo/lib/foo.so`

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
          - syslib
        image:
          reference: # registry path for your PostGIS image
      # ...
    # ...
  # ...
```

CloudNativePG will set the `LD_LIBRARY_PATH` environment variable to include
`/extensions/postgis/system`, allowing PostgreSQL to locate and load these
system libraries at runtime.

!!! Important
    Since `ld_library_path` must be set when the PostgreSQL process starts,
    changing this value requires a **cluster restart** for the new value to take effect.
    CloudNativePG does not currently trigger this restart automatically; you will need to
    manually restart the cluster (e.g., using `cnpg restart`) after modifying `ld_library_path`.

## Image Specifications

A standard extension container image for CloudNativePG includes two
required directories at its root:

- `share`: contains the extension control file (e.g., `<EXTENSION>.control`)
  and any SQL files.
- `lib`: contains the extension's shared library (e.g., `<EXTENSION>.so`) and
  any additional required libraries.

Following this structure ensures that the extension will be automatically
discoverable and usable by PostgreSQL within CloudNativePG without requiring
manual configuration.

!!! Important
    We encourage PostgreSQL extension developers to publish OCI-compliant extension
    images following this layout as part of their artifact distribution, making
    their extensions easily consumable within Kubernetes environments.
    Ideally, extension images should target a specific operating system
    distribution and architecture, be tied to a particular PostgreSQL version, and
    be built using the distribution’s native packaging system (for example, using
    Debian or RPM packages). This approach ensures consistency, security, and
    compatibility with the PostgreSQL images used in your clusters.

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
