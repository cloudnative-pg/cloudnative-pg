# Database Role Management

From its inception, CloudNativePG has managed the creation of specific roles
required in PostgreSQL instances:

- some reserved users, such as the `postgres` superuser, `streaming_replica`
  and `cnpg_pooler_pgbouncer` (when the PgBouncer `Pooler` is used)
- The application user, set as the low-privilege owner of the application database

This process is described in the ["Bootstrap"](bootstrap.md) section.

With the `managed` stanza in the cluster spec, CloudNativePG now provides full
lifecycle management for roles specified in `.spec.managed.roles`.

This feature enables declarative management of existing roles, as well as the
creation of new roles if they are not already present in the database. Role
creation will occur *after* the database bootstrapping is complete.

An example manifest for a cluster with declarative role management can be found
in the file [`cluster-example-with-roles.yaml`](samples/cluster-example-with-roles.yaml).

Here is an excerpt from that file:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
spec:
  managed:
    roles:
    - name: dante
      ensure: present
      comment: Dante Alighieri
      login: true
      superuser: false
```

The role specification in `spec.managed.roles` adheres to the
[PostgreSQL structure and naming conventions](https://www.postgresql.org/docs/current/sql-createrole.html).
Please refer to the [API reference](api_reference.md#RoleConfiguration) for
the full list of attributes you can define for each role.

A few points are worth noting:

1. The `ensure` attribute is **not** part of PostgreSQL. It enables declarative
   role management to create and remove roles. The two possible values are
   `present` (the default) and `absent`.
2. The `inherit` attribute is true by default, following PostgreSQL conventions.
3. The `connectionLimit` attribute defaults to -1, in line with PostgreSQL conventions.

Declarative role management ensures that PostgreSQL instances align with the
spec. If a user modifies role attributes directly in the database, the
CloudNativePG operator will revert those changes during the next reconciliation
cycle.

The CRD status includes a section for the managed roles' status, as shown below:

```yaml
  roleStatus:
    not-managed:
    - app
    pending-reconciliation:
    - dante
    reserved:
    - postgres
    - streaming_replica
```

This section covers roles reserved for operator use and those that are **not**
under declarative management, providing a comprehensive view of the roles in
the database instances.

!!! Important
    In terms of backward compatibility, declarative role management is designed
    to ignore roles that exist in the database but are not included in the spec.
    The lifecycle of these roles will continue to be managed within PostgreSQL,
    allowing CloudNativePG users to adopt this feature at their convenience.
