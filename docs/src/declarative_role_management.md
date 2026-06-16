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

## Coexistence and precedence

The two methods are not mutually exclusive: you can manage different roles with
each one at the same time, which is what makes a gradual migration from the
inline stanza to `DatabaseRole` resources possible. They only need a rule for
the case where the same role name is defined in both places.

In that case, **the Cluster specification (`managed.roles`) always takes
precedence**: the `DatabaseRole` is not reconciled and reports the conflict in
its status (see [Status of `DatabaseRole` resources](#status-of-databaserole-resources)).

:::important
Declarative role management ignores roles that exist in the database but are
not included in either the Cluster spec or a `DatabaseRole`. The lifecycle of
those roles continues to be managed within PostgreSQL, allowing you to adopt
this feature at your convenience.
:::

-----

## General role configuration notes

Regardless of the management method used, the role specification adheres to the
[PostgreSQL structure and naming conventions](https://www.postgresql.org/docs/current/sql-createrole.html).

:::tip
Please refer to the [API reference](cloudnative-pg.v1.md#roleconfiguration)
for the full list of attributes.
:::

A few points are worth noting:

1.  The `ensure` attribute is **not** part of PostgreSQL. It enables
    declarative role management to create (`present`, the default) or remove
    (`absent`) a role, and is available **only** in the inline
    [`managed.roles`](#inline-managed-roles) stanza. A `DatabaseRole` does not
    support `ensure`; it expresses role removal through its
    [reclaim policy](#role-reclaim-policy) instead.
2.  The `inherit` attribute is true by default, following PostgreSQL
    conventions.
3.  The `connectionLimit` attribute defaults to -1, in line with PostgreSQL
    conventions.
4.  Role membership with `inRoles` defaults to no memberships.

-----

## The `DatabaseRole` resource

The `DatabaseRole` custom resource provides a dedicated, Kubernetes-native way to
manage PostgreSQL database roles.

This is the recommended approach for modern environments and
GitOps workflows, as it decouples role lifecycle from the cluster
infrastructure.

:::note
A `DatabaseRole` is applied when its specification or its password Secret
changes. Changes made directly in the database, such as a manual
`ALTER ROLE`, are not detected or reverted until the next time the resource
is applied. Inline managed roles, by contrast, are periodically compared
with the database catalog and brought back to their specification.
:::

See [Security](security.md#rbac-on-custom-resources) for the RBAC
implications of granting access to `DatabaseRole` resources.

A `DatabaseRole` is namespace-scoped: the resource, the `Cluster` it references
through `spec.cluster`, and the `passwordSecret` it consumes must all live in
the same namespace.

### Example manifest

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: DatabaseRole
metadata:
  name: role-dante
spec:
  cluster:
    name: cluster-example
  name: dante
  comment: "Dante Alighieri"
  login: true
  superuser: false
  createdb: true
  databaseRoleReclaimPolicy: delete
  inRoles:
    - pg_monitor
  passwordSecret:
    name: cluster-example-dante
```

An example manifest for a role definition can be found in the file
[`role-examples.yaml`](samples/role-examples.yaml).

### Role reclaim policy

The `databaseRoleReclaimPolicy` field defines the "final act" of the operator when a
`DatabaseRole` Custom Resource is removed from the Kubernetes API.
This mirrors the behavior of Kubernetes Persistent Volumes.

- **`retain` (default):** The role is left in the database. This is the safest
  setting for production, ensuring that even if a manifest is accidentally
  deleted, the database user (and any objects they own) remains untouched.
- **`delete`:** The operator attempts to execute a `DROP ROLE` in PostgreSQL
  before the Kubernetes object is finalized. This is ideal for ephemeral or
  automated environments.

:::note
If a role owns objects (tables, schemas, etc.), `DROP ROLE` fails and the
`DatabaseRole` stays in `Terminating`, retried periodically until those objects
are reassigned or dropped. The operator never drops owned objects on your
behalf: reassign or drop them in PostgreSQL, or switch to `retain`, to let the
deletion complete.
:::

### Removing a role

How you remove a role depends on how it was created:

- **Created through a `DatabaseRole`:** delete the resource. Whether the role is
  also dropped from PostgreSQL is governed by its
  [reclaim policy](#role-reclaim-policy).
- **Pre-existing, or managed elsewhere:** a `DatabaseRole` is not the tool to drop
  it. Declare it `absent` through the inline [`managed.roles`](#inline-managed-roles)
  stanza, or run `DROP ROLE` directly.

:::warning
Creating a `DatabaseRole` for a role that already exists **adopts** it: the
operator alters the existing role so that **every** attribute matches the
manifest, including the attributes you omit, which are forced back to their
defaults. In particular, memberships not listed in `inRoles` are revoked, an
omitted `connectionLimit` is reset to `-1` (unlimited), and an omitted
`validUntil` becomes `infinity` if the role had an expiration date. Review
the current attributes and memberships of a role before adopting it, and do
not point a `DatabaseRole` at a role you only want to drop, since it will be
modified before it can be removed.
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

The `PasswordSecretChange` condition is maintained by the operator as an
internal signal for the instance manager: its message carries the
`resourceVersion` of the password Secret the operator last observed, and a
change in that value triggers the re-application of the password. The
condition appears once a password Secret is in use and is removed when
`passwordSecret` is removed from the specification.

If a `DatabaseRole` targets a name already managed in the Cluster spec
(see [Coexistence and precedence](#coexistence-and-precedence)), the `applied`
field will be `false` with the message:

```
database role is already managed by the CNPG cluster
```

On a [replica cluster](replica_cluster.md) the role is owned by the primary
cluster, not reconciled locally. In that case the instance manager reports the
role as *unknown* rather than failed: the `applied` field is left unset (`nil`)
with an explanatory message. The role is reconciled normally once the cluster
is promoted to primary.

### Client Certificate Generation

The `DatabaseRole` resource supports opt-in generation of TLS client
certificates, signed by the cluster's client CA and stored in a Kubernetes
Secret. This enables [PostgreSQL `cert` authentication](https://www.postgresql.org/docs/current/auth-cert.html)
as an alternative to passwords: no passwords to rotate manually, and private
keys never leave the cluster.

To enable it, add a `clientCertificate` block to the spec:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: DatabaseRole
metadata:
  name: role-dante
spec:
  cluster:
    name: cluster-example
  name: dante
  login: true
  clientCertificate:
    enabled: true
  databaseRoleReclaimPolicy: retain
```

`clientCertificate.enabled` defaults to `true` when the block is present, so
`clientCertificate: {}` is equivalent to enabling it. Set `enabled: false` to
turn issuance off while keeping the block in place.

:::important
`login: true` is required when `clientCertificate` issuance is enabled. The
operator enforces this via validation and will reject the resource otherwise.
:::

#### Generated Secret

The operator creates a Secret named `<databaserole-name>-client-cert` in the
same namespace. It contains two keys:

| Key | Contents |
|---|---|
| `tls.crt` | PEM-encoded client certificate, signed by the cluster's client CA |
| `tls.key` | PEM-encoded private key |

The expiration time of the certificate is visible in
`status.clientCertificate.expiration`:

```yaml
status:
  clientCertificate:
    expiration: "2026-07-01T12:00:00Z"
```

#### Configuring `pg_hba.conf`

The operator generates the certificate but does **not** modify `pg_hba.conf`
automatically. You must add a `hostssl` rule with the `cert` method to the
cluster for the role to be able to authenticate:

```yaml
spec:
  postgresql:
    pg_hba:
      - hostssl all dante all cert
```

A working connection string using the generated Secret would look like:

```
psql "host=<cluster>-rw.<namespace>.svc port=5432 dbname=<db> user=dante \
  sslcert=/path/to/tls.crt sslkey=/path/to/tls.key \
  sslrootcert=/path/to/ca.crt sslmode=verify-full"
```

#### Renewal

Certificates are renewed automatically on every reconcile cycle. The operator
checks whether the certificate is approaching expiry and re-signs it if needed.
Reconciles are scheduled at least once per hour when `clientCertificate`
issuance is enabled. The current expiration is always reflected in
`status.clientCertificate.expiration`.

#### Deletion and opt-out

| Scenario | Result |
|---|---|
| `clientCertificate.enabled` set to `false`, or the `clientCertificate` block removed | The cert Secret is deleted; `status.clientCertificate` is cleared |
| `DatabaseRole` deleted | The cert Secret is garbage-collected via owner reference, regardless of `databaseRoleReclaimPolicy` |

:::note
`databaseRoleReclaimPolicy: retain` retains the PostgreSQL role, not the generated
Secret. The Secret is only meaningful while the operator is managing the role,
so it is always cleaned up on deletion.
:::

#### Bring-your-own-CA limitation

If the cluster's client CA Secret does not contain a private key (i.e. you
supplied your own CA via `spec.certificates.clientCASecret`), the operator
cannot sign new certificates. It will record the reason in
`status.clientCertificate.message` and stop retrying:

```yaml
status:
  clientCertificate:
    message: 'client CA secret "my-ca" has no private key; bring-your-own-CA
      clusters require manual certificate management'
```

In this case, you must issue and renew client certificates manually.

:::note
CNPG does not manage Certificate Revocation Lists (CRLs). If a certificate
must be invalidated before it expires, rotate the cluster's client CA. This
will trigger re-issuance of all managed client certificates on the next
reconcile.
:::

---

## Inline managed roles

With the `managed` stanza in the cluster spec, CloudNativePG provides
management for roles specified in `.spec.managed.roles`.
This feature enables declarative management of existing roles, as well as the
creation of new roles if they are not already present.

### Example manifest

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

### Status of inline managed roles

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

## Migrating from inline managed roles to a `DatabaseRole`

You can move a role from the inline `managed.roles` stanza to a standalone
`DatabaseRole` without disruption:

1.  Create the `DatabaseRole` with the desired specification. Both methods
    share the same [`RoleConfiguration`](cloudnative-pg.v1.md#roleconfiguration)
    structure, so the stanza can be copied across as-is.
2.  Remove the matching entry from `.spec.managed.roles` in the `Cluster`
    manifest.
3.  The operator detects the change and hands management over to the
    `DatabaseRole`.

Because the Cluster spec takes precedence while both exist (see
[Coexistence and precedence](#coexistence-and-precedence)), the handover
happens only once the inline entry is gone, so there is no window in which the
role is left unmanaged.

When converting a role that the inline stanza removed with `ensure: absent`,
note that a `DatabaseRole` does not support `ensure: absent`. Express removal
through the [reclaim policy](#role-reclaim-policy) instead: delete the resource
with `databaseRoleReclaimPolicy: delete` to drop the role, or keep the default
`retain` to leave it in place. See [Removing a role](#removing-a-role) for the
full behavior.

---

## Password management

The declarative role management feature (both with a `DatabaseRole` and the
inline `managed.roles` stanza) includes reconciling of role passwords.
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

:::important
Label the Secret with `cnpg.io/reload: "true"`, as shown above. Password
changes in labeled Secrets are applied immediately, while changes in
unlabeled Secrets are only applied at a subsequent reconciliation, for
example when the operator refreshes its internal cache.
:::

If no `passwordSecret` is specified, the instance manager will not try to
`CREATE/ALTER` the role with a password, keeping with PostgreSQL conventions.

:::important
New roles created without `passwordSecret` will have a `NULL` password inside
PostgreSQL.
:::


### Disabling passwords

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

### Pre-hashed passwords

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

:::warning
    The example above uses `stringData:`, where Kubernetes encodes the value
    for you, which is the safest path for pre-hashed passwords. If you must
    use `data:`, encode the bytes exactly with `printf '%s' "$hash" | base64`
    (or `echo -n "$hash" | base64`). A trailing newline from a naive
    `echo "$hash" | base64` makes the value miss the SCRAM/MD5 shadow-format
    check, so the operator falls back to treating it as cleartext and
    re-hashes it, and login stops working.
:::

### Safety when transmitting cleartext passwords

Role passwords are safely managed in Kubernetes using Secrets, but the
SQL path between the operator and PostgreSQL is also a concern. As noted
in the [PostgreSQL documentation](https://www.postgresql.org/docs/current/sql-createrole.html):

> The password will be transmitted to the server in cleartext, and it might
> also be logged in the client's command history or the server log

CloudNativePG protects this path in two complementary ways:

1. Before emitting `CREATE`/`ALTER ROLE ... PASSWORD '...'`, the operator
   SCRAM-SHA-256 encodes any cleartext password operator-side (client-side
   from PostgreSQL's point of view). This is the standard PostgreSQL
   practice for keeping cleartext out of server logs and extensions like
   `pg_stat_statements` or `pgaudit`, and is the same encoding that
   `psql \password` and libpq's `PQencryptPasswordConn` perform. The
   literal PostgreSQL receives is the SCRAM-SHA-256 verifier stored in
   `pg_authid.rolpassword`. Passwords already provided in MD5 or
   SCRAM-SHA-256 shadow form are forwarded unchanged.
2. The same `CREATE`/`ALTER ROLE` statements are executed inside a
   transaction that temporarily suppresses both statement logging
   (`log_statement`) and error statement logging
   (`log_min_error_statement`), preventing leakage to the PostgreSQL log
   in both success and failure scenarios.

The Status section of the cluster does not print the query statement for any
managed role operation.

#### Opting out of operator-side encoding

If you need PostgreSQL (not the operator) to decide how the password is
hashed (for example, on a cluster running `password_encryption = md5`),
set the annotation `cnpg.io/passwordPassthrough: "enabled"` on the
basic-auth Secret. The operator will then forward the password value
verbatim.

:::warning
    The `cnpg.io/passwordPassthrough` annotation must be set on the
    **basic-auth Secret** itself, not on the `Cluster` resource. Placing it
    on the `Cluster` has no effect, and the operator will continue to apply
    SCRAM-SHA-256 encoding to the password before sending it to PostgreSQL.
:::

The opt-in is per-Secret and applies to every basic-auth Secret the
operator consumes (managed-role secrets, but also the superuser and
application-user secrets), so a single cluster can mix passthrough
secrets and operator-encoded secrets freely. The statement-logging
suppression layer described above still applies in both modes.

:::warning
    With `cnpg.io/passwordPassthrough: "enabled"`, the operator forwards
    the Secret's `password` value verbatim. If that value is cleartext (the
    common case on a `password_encryption = md5` cluster), extensions such
    as `pg_stat_statements` or `pgaudit` will observe it. This is the
    expected trade-off for letting PostgreSQL choose the hash format.
:::

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

CloudNativePG will record when such fundamental errors occur, and will display
them in the cluster Status, as described in
[Status of inline managed roles](#status-of-inline-managed-roles).
