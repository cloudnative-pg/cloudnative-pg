---
id: declarative_role_management
sidebar_position: 230
title: PostgreSQL Role management
---

# PostgreSQL Role management
<!-- SPDX-License-Identifier: CC-BY-4.0 -->

:::info
From its inception, CloudNativePG has managed the creation of specific roles
required in PostgreSQL instances:

- some reserved users, such as the `postgres` superuser, `streaming_replica`
  and `cnpg_pooler_pgbouncer` (when the PgBouncer `Pooler` is used)
- The application user, set as the low-privilege owner of the application database

This process is described in the ["Bootstrap"](bootstrap.md) section.
:::

CloudNativePG provides full lifecycle management for PostgreSQL database roles.
You can define roles either:

1. as [standalone `DatabaseRole` resources](#the-databaserole-resource) (recommended), or
2. via [the `managed` stanza within the `Cluster` spec](#inline-managed-roles).

## General Role Configuration Notes

Regardless of the management method used, the role specification adheres to the
[PostgreSQL structure and naming conventions](https://www.postgresql.org/docs/current/sql-createrole.html).

:::tip
Please refer to the [API reference](cloudnative-pg.v1.md#roleconfiguration)
for the full list of attributes.
:::

A few points are worth noting:

1.  The `ensure` attribute is **not** part of PostgreSQL.
    It enables declarative role management to create and remove roles.
    The two possible values are `present` (the default) and `absent`.
    Note: `ensure: absent` is only supported for
    [inline managed roles](#inline-managed-roles). For `DatabaseRole` CRDs,
    delete the Kubernetes resource with `roleReclaimPolicy: delete` instead.
2.  The `inherit` attribute is true by default, following PostgreSQL
    conventions.
3.  The `connectionLimit` attribute defaults to -1, in line with PostgreSQL
    conventions.
4.  Role membership with `inRoles` defaults to no memberships.

-----

## The `DatabaseRole` Resource

The `DatabaseRole` Custom Resource provides a dedicated, Kubernetes-native way to
manage PostgreSQL database roles.

This is the recommended approach for modern environments and
GitOps workflows, as it decouples role lifecycle from the cluster
infrastructure.

### Example Manifest

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: DatabaseRole
metadata:
  name: role-dante
spec:
  cluster:
    name: cluster-example
  name: dante
  ensure: present
  comment: "Dante Alighieri"
  login: true
  superuser: false
  createdb: true
  roleReclaimPolicy: delete
  inRoles:
    - pg_monitor
  passwordSecret:
    name: cluster-example-dante
```

An example manifest for a role definition can be found in the file
[`role-examples.yaml`](samples/role-examples.yaml).

### Role Reclaim Policy

The `roleReclaimPolicy` field defines the "final act" of the operator when a
`DatabaseRole` Custom Resource is removed from the Kubernetes API.
This mirrors the behavior of Kubernetes Persistent Volumes.

- **`retain` (default):** The role is left in the database. This is the safest
  setting for production, ensuring that even if a manifest is accidentally
  deleted, the database user (and any objects they own) remains untouched.
- **`delete`:** The operator attempts to execute a `DROP ROLE` in PostgreSQL
  before the Kubernetes object is finalized. This is ideal for ephemeral or
  automated environments.

:::note
If a role owns objects (tables, schemas, etc.), a `delete` policy will fail to
drop the role until those objects are reassigned or dropped.
<!-- TODO: at the moment this is not working -->
The Kubernetes resource will remain in a `Terminating` state until the database
allows the `DROP ROLE` command to succeed.
:::

### Status of `DatabaseRole` resources

The `DatabaseRole` resource includes a dedicated `status` section for per-role
observability:

```yaml
status:
  applied: true
  observedGeneration: 3
  conditions:
  - lastTransitionTime: "2026-04-04T15:06:23Z"
    message: "2051"
    reason: ChangeDetected
    status: "True"
    type: PasswordSecretChange
```

If a `DatabaseRole` CRD targets a name already managed in the Cluster spec, the
`applied` field will be `false` with the message:

```
database role is already managed by the CNPG cluster
````

---

## Inline Managed Roles

With the `managed` stanza in the cluster spec, CloudNativePG provides
management for roles specified in `.spec.managed.roles`.
This feature enables declarative management of existing roles, as well as the
creation of new roles if they are not already present.

### Example Manifest

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

### Status of Inline Managed Roles

When using the inline method, the `Cluster` status includes a comprehensive
summary:

```yaml
status:
  managedRolesStatus:
    byStatus:
      reconciled:
      - dante
      reserved:
      - postgres
      - streaming_replica
    cannotReconcile:
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

---

## Precedence and Coexistence

You can use both methods simultaneously for different roles.

However, **the Cluster specification (`managed.roles`) always takes precedence.**
If a role name exists in both the Cluster spec and a `DatabaseRole` CRD, the CRD will
not be reconciled.

To migrate an inline role to a `DatabaseRole` CRD:

1.  Create the `DatabaseRole` CRD with the desired specification.
2.  Remove the entry from `.spec.managed.roles` in the `Cluster` manifest.
3.  The operator will automatically detect the change and hand over management
    to the standalone `DatabaseRole` resource.

:::important
In terms of backward compatibility, declarative role management is designed to
ignore roles that exist in the database but are not included in the spec or a
`DatabaseRole` CRD. The lifecycle of these roles will continue to be managed within
PostgreSQL, allowing CloudNativePG users to adopt this feature at their
convenience.
:::

---

## Password management

The declarative role management feature (both via CRD and Cluster spec)
includes reconciling of role passwords.
Managed role configurations may optionally specify the name of a **Secret**
where the username and password are stored:

```yaml
  passwordSecret:
    name: cluster-example-dante
```

The Secret must be of type `kubernetes.io/basic-auth`. The username (encoded in
*Base64* as is usual in Kubernetes) should match the role we are setting the
password for. For example:

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

If no `passwordSecret` is specified, the instance manager will not try to
`CREATE/ALTER` the role with a password, keeping with PostgreSQL conventions.

:::important
New roles created without `passwordSecret` will have a `NULL` password inside
PostgreSQL.
:::


### Disabling Passwords

To explicitly set a password to `NULL` in PostgreSQL (distinguished from simply
omitting a password update), use the `disablePassword` field:

``` yaml
  disablePassword: true
```

:::note
It is an error to set both `passwordSecret` and `disablePassword` on a given role.
:::

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

### Safety when transmitting cleartext passwords

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
