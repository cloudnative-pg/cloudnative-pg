# Database Role Management

CloudNativePG has from inception managed role creation for a few specific roles
needed in PostgreSQL instances:

- the superuser `postgres` of course, as well as the `streaming_replica` role
- the application user, set as the low-privilege owner of the application
  database

This is described in the ["Bootstrap" section](bootstrap.md).

With the `managed` stanza in the cluster spec, CloudNativePG extends management
from creation to the full lifecycle management for roles described in
`.spec.managed.roles`.

This feature allows to manage existing roles in a declarative way, and to create
them too, if they are not yet present in the database.
The creation of those roles will happen *after* the database bootstrapping is
complete.

There is an example manifest for a cluster with declarative role management
in the file
[`cluster-example-with-roles.yaml`](samples/cluster-example-with-roles.yaml)

An excerpt from that file:

``` yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
spec:
  managed:
    roles:
    - name: edb_admin
      ensure: present
      comment: my database-side comment
      login: true
      superuser: false
```

The role specification in `spec.managed.roles` follows the
[PostgreSQL structure
and naming conventions](https://www.postgresql.org/docs/current/sql-createrole.html).
A few points are worth noting:

1. the `ensure` attribute is **not** a part of PostgreSQL. It allows declarative
  role management to extend not only to role creation, but to role destruction.
  The two possible values are `present` (the default,) and `absent`.
2. the `inherit` attribute is true by default, as per PostgreSQL conventions.
3. the `connectionLimit` attribute is by default -1, as per PostgreSQL
  conventions.

Declarative role management will ensure that PostgreSQL instances are in
line with the spec. This means that if a user were to log onto the
database and change role attributes there, the CloudNativePG operator would
roll back those changes in the next reconciliation cycle.

There is a section in the CRD status for the status of the managed roles. E.g.

``` yaml
  roleStatus:
    not-managed:
    - app
    pending-reconciliation:
    - edb_admin
    reserved:
    - postgres
    - streaming_replica
```

It includes the roles reserved for operator use and the roles that are **not**
managed by CloudNativePG, in order to get a complete view of the roles in
the database instances.

This segues into the topic of backward compatibility: declarative role
management is defined to ignore roles that exist in the database but are not in the spec. The lifecycle of those roles will continue to be handled within
PostgreSQL. This allows users of CloudNativePG to opt into the feature at
their convenience.
