---
id: declarative_role_management
sidebar_position: 230
title: PostgreSQL Role management
---

# PostgreSQL Role management
<!-- SPDX-License-Identifier: CC-BY-4.0 -->

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

The role specification in `.spec.managed.roles` adheres to the
[PostgreSQL structure and naming conventions](https://www.postgresql.org/docs/current/sql-createrole.html).
Please refer to the [API reference](cloudnative-pg.v1.md#roleconfiguration) for
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

### Password expiry, `VALID UNTIL`

The `VALID UNTIL` role attribute in PostgreSQL controls password expiry. Roles
created without `VALID UNTIL` specified get NULL by default in PostgreSQL,
meaning that their password will never expire.

PostgreSQL uses a timestamp type for `VALID UNTIL`, which includes support for
the value `'infinity'` indicating that the password never expires. Please see the
[PostgreSQL documentation](https://www.postgresql.org/docs/current/datatype-datetime.html)
for reference.

With declarative role management, the `validUntil` attribute for managed roles
controls password expiry. `validUntil` can only take:

- a Kubernetes timestamp, or
- be omitted (defaulting to `null`)

In the first case, the given `validUntil` timestamp will be set in the database
as the `VALID UNTIL` attribute of the role.

In the second case (omitted `validUntil`) the operator will ensure password
never expires, mirroring the behavior of PostgreSQL. Specifically:

- in case of new role, it will omit the `VALID UNTIL` clause in the role
  creation statement
- in case of existing role, it will set `VALID UNTIL` to `infinity` if `VALID
  UNTIL` was not set to `NULL` in the database (this is due to PostgreSQL not
  allowing `VALID UNTIL NULL` in the `ALTER ROLE` SQL statement)

:::warning
    New roles created without `passwordSecret` will have a `NULL` password
    inside PostgreSQL.
:::

### Password hashed

You can also provide pre-encrypted passwords by specifying the password
in MD5/SCRAM-SHA-256 hash format:

``` yaml
kind: Secret
type: kubernetes.io/basic-auth
metadata:
  name: cluster-example-cavalcanti
  labels:
    cnpg.io/reload: "true"
apiVersion: v1
stringData:
  username: cavalcanti
  password: SCRAM-SHA-256$<iteration count>:<salt>$<StoredKey>:<ServerKey>
```

### Safety when using non-encrypted passwords

While role passwords are safely managed in Kubernetes using Secrets,
there is still a risk on the PostgreSQL side. If creating/altering a role with
password, PostgreSQL may print the password as part of the query statement
in some `postgres` logs, as mentioned in the [PostgreSQL documentation](https://www.postgresql.org/docs/current/sql-createrole.html):

> The password will be transmitted to the server in cleartext, and it might
> also be logged in the client's command history or the server log

CloudNativePG adds a safety layer by temporarily suppressing both statement
logging (`log_statement`) and error statement logging
(`log_min_error_statement`) for any CREATE or ALTER operation on a role with
password, thus preventing leakage in both success and failure scenarios.
The Status section of the cluster does not print the query statement for any
managed role operation.

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

The Cluster status includes a section for the managed roles' status, as shown
below:

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

The [kubectl plugin](kubectl-plugin.md) also shows the status of managed roles
in its `status` sub-command:

``` txt
Managed roles status
Status                  Roles
------                  -----
pending-reconciliation  petrarca
reconciled              app,dante
reserved                postgres,streaming_replica

Irreconcilable roles
Role      Errors
----      ------
petrarca  could not perform UPDATE_MEMBERSHIPS on role petrarca: role "poets" does not exist
```

:::info[Important]
    In terms of backward compatibility, declarative role management is designed
    to ignore roles that exist in the database but are not included in the spec.
    The lifecycle of these roles will continue to be managed within PostgreSQL,
    allowing CloudNativePG users to adopt this feature at their convenience.
:::