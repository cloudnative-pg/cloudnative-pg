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
      inRoles:
        - pg_monitor
        - pg_signal_backend
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
4. Role membership with `inRoles` defaults to no memberships.

Declarative role management ensures that PostgreSQL instances align with the
spec. If a user modifies role attributes directly in the database, the
CloudNativePG operator will revert those changes during the next reconciliation
cycle.

## Password management

The declarative role management feature includes reconciling of role passwords.
Passwords are managed in fundamentally different ways in the Kubernetes world
and in PostgreSQL, and as a result there are a few things to note.

Managed role configurations may optionally specify the name of a
**Secret** where the username and password are stored (encoded in Base64
as is usual in Kubernetes). For example:

``` yaml
  managed:
    roles:
    - name: dante
      ensure: present
      [… snipped …]
      passwordSecret:
        name: cluster-example-dante
```

This would assume the existence of a Secret called `cluster-example-dante`,
containing a username and password. The username should match the role we
are setting the password for. For example, :

``` yaml
apiVersion: v1
data:
  username: ZGFudGU=
  password: ZGFudGU=
kind: Secret
metadata:
  name: cluster-example-dante
  labels:
    cnpg.io/reload: "true"
type: kubernetes.io/basic-auth
```

If there is no `passwordSecret` specified for a role, the instance manager will
not try to CREATE / ALTER the role with a password. This keeps with PostgreSQL
conventions, where ALTER will not update passwords unless directed to with
`WITH PASSWORD`.

If a role was initially created with a password, and we would like to set the
password to NULL in PostgreSQL, this necessitates being explicit on the part of
the user of CloudNativePG.
To distinguish "no password provided in spec" from "set the password to NULL",
the field `DisablePassword` should be used.

Imagine we decided we would like to have no password on the `dante` role in the
database. In such case we would specify the following:

``` yaml
  managed:
    roles:
    - name: dante
      ensure: present
      [… snipped …]
      disablePassword: true
```

NOTE: it is considered an error to set both `passwordSecret` and
`disablePassword` on a given role.
This configuration will be rejected by the validation webhook.

!!! Warning
    The declarative role management feature has changed behavior since its
    initial version (1.20.0). In 1.20.0, a role without a `passwordSecret` would
    lead to setting the password to NULL in PostgreSQL.
    In practice there is little difference from 1.20.0.
    New roles created without `passwordSecret` will have a NULL password.
    The relevant change is when using the managed roles to manage roles that
    had been previously created. In 1.20.0, doing this might inadvertently
    result in setting existing passwords to NULL.

## Unrealizable role configurations

In PostgreSQL, in some cases, commands cannot be honored by the database and
will be rejected. Please refer to the
[PostgreSQL documentation on error codes](https://www.postgresql.org/docs/current/errcodes-appendix.html)
for details.

Role operations can produce such fundamental errors.
Two examples:

- We ask PostgreSQL to create the role `petrarca` as a member of the role
  (group) `poets`, but `poets` does not exist.
- We ask PostgreSQL to drop the role `dante`, but the role `dante` is the owner
  of the database `inferno`.

These fundamental errors cannot be fixed by the database, nor the CloudNativePG
operator, without clarification from the human administrator. The two examples
above could be fixed by creating the role `poets` or dropping the database
`inferno` respectively, but they might have originated due to human error, and
in such case, the "fix" proposed might be the wrong thing to do.

CloudNativePG  will record when such fundamental errors occur, and will display
them in the cluster Status. Which segues into…

## Status of managed roles

The CRD status includes a section for the managed roles' status, as shown below:

```yaml
status:
  […snipped…]
  managedRolesStatus:
    byStatus:
      not-managed:
      - app
      pending-reconciliation:
      - dante
      - petrarca
      reconciled:
      - ariosto
      reserved:
      - postgres
      - streaming_replica
    cannotReconcile:
      dante:
      - 'could not perform DELETE on role dante: owner of database inferno'
      petrarca:
      - 'could not perform UPDATE_MEMBERSHIPS on role petrarca: role "poets" does not exist'
```

Note the special sub-section `cannotReconcile` for operations the database (and
CloudNativePG) cannot honor, and which require human intervention.

This section covers roles reserved for operator use and those that are **not**
under declarative management, providing a comprehensive view of the roles in
the database instances.

!!! Important
    In terms of backward compatibility, declarative role management is designed
    to ignore roles that exist in the database but are not included in the spec.
    The lifecycle of these roles will continue to be managed within PostgreSQL,
    allowing CloudNativePG users to adopt this feature at their convenience.
