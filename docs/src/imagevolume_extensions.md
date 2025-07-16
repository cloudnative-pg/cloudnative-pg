# ImageVolume Extensions
<!-- SPDX-License-Identifier: CC-BY-4.0 -->

CloudNativePG supports dynamic loading of PostgreSQL extensions into a
running `Cluster`, leveraging the ImageVolume feature of Kubernetes.

This feature allows mounting a PostgreSQL extension, packaged as an
OCI-compliant container image, as a read-only and immutable volume
directly inside a running pod's filesystem at a known path.

## Requirements:
* PostgreSQL 18+
* Kubernetes 1.31+ with the ImageVolume feature gate enabled.

## Key Concepts

Each image volume will be mounted at `/extensions/<EXTENSION_NAME>`.
The operator takes care of automatically managing both
`extension_control_path` and `dynamic_library_path` GUCs, pointing
them by default to:
- `/extension/<EXTENSION_NAME>/share` for `extension_control_path`
- `/extension/<EXTENSION_NAME>/lib` for `dynamic_library_path`

!!! Important
    The extension container image must be compatible with the PostgreSQL image
    deployed, meaning that it should have coherent PostgreSQL major version,
    OS distribution and CPU architecture.

## Adding a new extension

Via the `.spec.postgresql.extensions` stanza, you can define a list of
imageVolume extensions that should be mounted inside a Cluster. For example:

```yaml
spec:
  postgresql:
    extensions:
      - name: pgvector
        image:
          reference: pgvector:0.8
          pullPolicy: IfNotPresent
```

The `name` field is a mandatory unique name that will be used to mount the
imageVolume, which in this case will result to `/extensions/pgvector`.
It must consist of lowercase alphanumeric characters or '-', and must start/end
with an alphanumeric character.

!!! Info
    When a new extension is applied on a running Cluster, CloudNativePG will
    perform a [Rolling Update](rolling_update.md) to add the newly requested
    image volume to each pod.

Then, the operator will automatically configure the required GUCs to allow
PostgreSQL to load extensions from the `pgvector` volume, by appending:
- `/extension/pgvector/share` to `extension_control_path`
- `/extension/pgvector/lib` to `dynamic_library_path`

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
