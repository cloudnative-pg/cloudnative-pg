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

The `DatabaseRole` custom resource provides a dedicated, Kubernetes-native way
to manage PostgreSQL database roles. This is the **recommended** approach for
modern environments and GitOps workflows, as it decouples the role lifecycle
from the cluster infrastructure, and it manages more of the role than the
inline stanza can.

Beyond what [inline managed roles](#inline-managed-roles) offer, a
`DatabaseRole` can:

- **generate the password** of the role and keep it in a Secret it owns,
  shaped by [criteria](#criteria) you choose and
  [rotated](#rotation) on a schedule;
- **issue a TLS client certificate** for the role, signed by the cluster's
  client CA, enabling
  [certificate authentication](#client-certificate-authentication) instead of
  passwords;
- say **how the password is managed** through a single
  [`password.mode`](#choosing-a-mode) field, instead of the combination of
  `passwordSecret` and `disablePassword` that inline roles rely on;
- express role removal through a [reclaim policy](#role-reclaim-policy).

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
through `spec.cluster`, and any Secret it reads a password from must all live in
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
  password:
    mode: generate
```

An example manifest for a role definition can be found in the file
[`role-examples.yaml`](samples/role-examples.yaml).

### Authentication

A role authenticates to PostgreSQL with a password, with a TLS client
certificate, or with both. A `DatabaseRole` can manage either:

- [password authentication](#password-authentication), through the `password`
  stanza, which states whether the operator generates the password, reads it
  from a Secret you supply, leaves it to something else, or removes it;
- [client certificate authentication](#client-certificate-authentication),
  through the `clientCertificate` stanza, which has the operator issue and
  renew a certificate signed by the cluster's client CA.

The two are not exclusive: a role can carry a generated password *and* a
generated client certificate, with `pg_hba.conf` deciding which one a given
connection has to present.

### Password authentication

#### Choosing a mode

The `password` stanza states how the operator manages the password of the role:

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
  password:
    mode: generate
```

`password.mode` is **required** whenever the stanza is present: it has no
default, so `password: {}` is rejected. Asking for a password without saying
how it is managed is ambiguous, and neither answer is a safe guess: one
generates a credential, another removes one.

| `mode` | The operator… | Details |
|---|---|---|
| `generate` | generates the password, keeps it in a Secret it owns, and rotates it if asked | [`mode: generate`](#mode-generate) |
| `secret` | reads the password from an existing Secret you supply, and never writes to it | [`mode: secret`](#mode-secret) |
| `external` | stops managing the password, leaving it to something else | [`mode: external`](#mode-external) |
| `setNull` | sets the password to `NULL`, disabling password authentication | [`mode: setNull`](#mode-setnull) |

The remaining fields of the stanza are restricted to the modes that use them:

| Field | Allowed under |
|---|---|
| `secret` | `generate` (optional), `secret` (**required**) |
| `criteria`, `duration`, `renewBefore` | `generate` only |

The stanza is mutually exclusive with the
[deprecated `passwordSecret` and `disablePassword` fields](#deprecated-password-fields),
and it is only available on `DatabaseRole` resources, not on inline managed
roles.

Once present, the stanza cannot be removed: `mode` is what says how the
password is managed from now on, so a role that stops generating one has to
state what happens to it instead. See [Changing the mode](#changing-the-mode).

#### `mode: generate`

The operator generates the password of the role, stores it in a Secret it owns,
and applies it to PostgreSQL:

```yaml
  password:
    mode: generate
```

##### Generated password Secret

The operator creates a Secret of type `kubernetes.io/basic-auth`, named
`<databaserole-name>-password` unless `password.secret` says otherwise. The
first two keys are the ones the instance manager applies to PostgreSQL; the
third is there for whoever consumes the credential:

| Key | Contents |
|---|---|
| `username` | the name of the PostgreSQL role, `spec.name` |
| `password` | the generated password |
| `pgpass` | a ready-made [`.pgpass`](https://www.postgresql.org/docs/current/libpq-pgpass.html) line for the role |

The `pgpass` line wildcards the host and the database, since the credential
belongs to the role and says nothing about which endpoint of the cluster, or
which database, it is used against:

```
*:5432:*:dante:<the generated password>
```

It can be mounted or copied straight into a `~/.pgpass` file, so a client
authenticates without the password appearing in a connection string or in the
shell history:

```sh
kubectl get secret role-dante-password -o jsonpath='{.data.pgpass}' \
  | base64 -d > ~/.pgpass && chmod 0600 ~/.pgpass
```

The Secret does not need the `cnpg.io/reload` label: the operator owns it, and
reacts to any change to it.

A Secret with that name that the operator does not own is never overwritten,
nor deleted: the conflict is reported in `status.password.message` and no
password is generated until it is resolved.

##### Criteria

The shape of the generated password follows the `criteria` block, modeled on
the [external-secrets password generator](https://external-secrets.io/main/api/generator/password/):

| Field | Meaning | Default |
|---|---|---|
| `length` | length of the password | `24` |
| `digits` | how many characters are digits | a quarter of `length`, never more than 10 |
| `symbols` | how many characters are symbols | `0` |
| `symbolCharacters` | the symbols to draw from | the set below |
| `noUpper` | exclude uppercase characters | `false` |
| `allowRepeat` | let a character appear more than once | `false` |

The symbols the generator draws from, unless `symbolCharacters` narrows them:

```
~!@#$%^&*()_+`-={}|[]\:"<>?,./
```

Symbols are excluded by default: a password ends up in connection strings, URIs
and `.pgpass` files, where several of those characters need escaping.

`symbolCharacters` accepts ASCII punctuation only: a letter or a digit there
would collide with the rest of the password when characters cannot be repeated,
and whitespace would be trimmed away before the password reaches PostgreSQL.

Since characters are not repeated unless `allowRepeat` is set, a password
cannot contain more letters than the 52 available (26 with `noUpper`), more than
ten digits, or more symbols than `symbolCharacters` has: criteria asking for
more than that are rejected when the role is created. A password of 64
characters, for instance, needs `allowRepeat`. When criteria the API cannot
check turn out to be unsatisfiable anyway, the operator explains it in
`status.password.message` and waits for the specification to be corrected,
rather than retrying.

##### Rotation

A generated password is created once and, by default, never changes. Set
`duration` to give it a lifetime: the operator then generates a new password,
`renewBefore` ahead of its expiration, and the instance manager applies it to
PostgreSQL.

```yaml
  password:
    mode: generate
    duration: 2160h    # 90 days
    renewBefore: 168h  # 7 days
```

`renewBefore` defaults to the operator's `EXPIRING_CHECK_THRESHOLD` setting
(7 days) capped at half of `duration`, so that a short-lived password is not
due for renewal as soon as it is generated. This is the same setting that
governs certificate expiration checks, so changing it shifts password
rotation timing too. For the same reason `duration` must be at least one
minute, and an explicit `renewBefore` at most half of it.

`status.password` records when the current password was issued and when it
expires:

```yaml
status:
  password:
    secretName: role-dante-password
    issuedAt: "2026-08-16T09:12:44Z"
    expiration: "2026-11-16T09:12:44Z"
```

Only `issuedAt` is load-bearing: `expiration` is recomputed from it and the
role's current `duration`/`renewBefore` on every reconciliation, so that
shortening or lengthening either takes effect immediately instead of being
measured against a deadline computed under the previous settings.

A lifetime is also enforced by PostgreSQL, not just honored by the operator:
the role's `VALID UNTIL` follows the expiration of the generated password. See
[Password expiry, `VALID UNTIL`](#password-expiry-valid-until) for what that
means when a rotation does not happen on time.

:::warning
PostgreSQL stores a single password per role, so a rotation invalidates the
previous one as soon as it is applied: any consumer still holding it fails to
authenticate until it reads the Secret again. Enable rotation only for
consumers that pick the new password up, and expect a short window during
which the Secret already carries a password that the database has not applied
yet.
:::

Adding `duration` to a role whose password was generated earlier counts the
lifetime the password already had, from the creation of its Secret: one older
than the requested duration is rotated at once. Removing `duration` stops
rotation and clears the recorded deadline, keeping the current password.

##### Manual rotation

To rotate a generated password immediately, regardless of its renewal
deadline or of whether `duration` is set at all, annotate the `DatabaseRole`
with `cnpg.io/rotatePassword` (any value):

```sh
kubectl annotate databaserole role-dante cnpg.io/rotatePassword=true
```

The annotation is a one-shot request: the operator removes it as soon as the
rotation it asked for has happened, rather than leaving it in place as a
standing setting. A request made while password generation is off is removed
without effect, since there is nothing to rotate; a request made while
generation is only temporarily blocked (for instance by a replica cluster) is
left in place and honored once the block clears.

On a role with a `duration`, the rotation restarts its lifetime: the
expiration is recomputed from the moment the new password is issued, and with
it the `VALID UNTIL` of the role (see
[Generated passwords with a lifetime](#generated-passwords-with-a-lifetime)).

##### Replica clusters

On a [replica cluster](replica_cluster.md) the role, and therefore its
password, is owned by the primary cluster and replicated from it. The operator
does not generate a password there, and says so in `status.password.message`:
generation, and rotation, start once the cluster is promoted.

#### `mode: secret`

The operator reads the password of the role from an existing Secret that you
create and maintain, named by `password.secret`:

```yaml
  password:
    mode: secret
    secret: cluster-example-dante
```

`secret` is required in this mode. The Secret must follow the
[basic-auth format](#supplying-a-password-in-a-secret) common to both
management methods, including the `cnpg.io/reload` label for changes to be
picked up promptly.

The operator neither owns nor writes to that Secret: nothing is generated, and
nothing is rotated. The password it holds is applied to the role as it is.

This is the `password`-stanza equivalent of the deprecated `passwordSecret`
field, and the mode to use when the password is produced by something else:
an external secret manager, a CI pipeline, or a human.

#### `mode: external`

The operator stops managing the password of the role, leaving whatever is set
in PostgreSQL to whatever produced it:

```yaml
  password:
    mode: external
```

Use this when the password is managed outside CloudNativePG entirely, whether
set by hand or by a tool that talks to PostgreSQL directly, and you want the
operator to leave it alone while still managing the rest of the role.

:::important
A password the operator had **generated** is the one exception: its Secret is
deleted when generation stops, so nothing could read that credential any more,
and the operator sets it to `NULL` once before leaving the role alone. A
password it never generated is never touched.
:::

#### `mode: setNull`

The operator sets the password of the role to `NULL` in PostgreSQL, disabling
password authentication for it:

```yaml
  password:
    mode: setNull
```

This is different from simply not managing a password: `NULL` is applied
explicitly, so the role cannot authenticate with a password at all. Use it for
roles that authenticate by
[client certificate](#client-certificate-authentication) or another method, or
for a role whose password you want positively removed.

This is the `password`-stanza equivalent of the deprecated `disablePassword`
field.

#### Changing the mode

Switching mode is how a role stops doing one thing and starts doing another;
what happens to a Secret the operator generated depends on where it is going:

| Change | Result |
|---|---|
| `generate` → `external` | The generated Secret is deleted, and the password it held is set to `NULL` in PostgreSQL: nothing could read that password any more, so it is not left behind as a credential nobody knows. The operator then stops managing the password |
| `generate` → `setNull` | The generated Secret is deleted, and the role's password is set to `NULL` in PostgreSQL |
| `generate` → `secret` | The generated Secret is deleted, that one included if `password.secret` names it, and the password is read from the Secret that name refers to from now on |
| `password.secret` pointed at another name, still `generate` | The password is generated again into the new Secret, and the previous one is deleted |
| `DatabaseRole` deleted | The generated Secret, if any, is garbage-collected via owner reference, regardless of `databaseRoleReclaimPolicy` |

The operator recognizes the Secret as its own through
`status.password.secretName`, which is the name it last generated a password
into: that is how a Secret whose name has disappeared from the specification is
still cleaned up, rather than left behind holding a credential nobody
maintains.

:::warning
A Secret the operator generated is deleted as soon as it stops generating into
it, and `password.secret` naming that very Secret does not take the password
over: what the operator created, the operator deletes. Copy the password into
a Secret of your own, under a different name, before turning generation off.
The same applies to `databaseRoleReclaimPolicy: retain`, where the role
survives the deletion of the `DatabaseRole` but its generated Secret does not.
:::

### Client certificate authentication

The `DatabaseRole` resource supports opt-in generation of TLS client
certificates, signed by the cluster's client CA and stored in a Kubernetes
Secret. This enables [PostgreSQL `cert` authentication](https://www.postgresql.org/docs/current/auth-cert.html)
as an alternative to passwords: no passwords to rotate manually, and private
keys are stored as Kubernetes Secrets and never transmitted outside the cluster.

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

#### Generated certificate Secret

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

#### Certificate renewal

Client certificates inherit the operator's global certificate settings: they
are issued with a **90-day** lifetime by default and renewed automatically once
they fall within **7 days** of expiry. Both values are operator-wide and
configurable via the `CERTIFICATE_DURATION` and `EXPIRING_CHECK_THRESHOLD`
operator settings; they are not configurable per `DatabaseRole`.

Renewal is driven by the reconcile loop: the operator checks whether the
certificate is approaching expiry and re-signs it if needed. Reconciles are
scheduled at least once per hour when `clientCertificate` issuance is enabled,
so renewal happens well before expiry even without a triggering event. The
current expiration is always reflected in `status.clientCertificate.expiration`.

#### Turning certificate issuance off

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
CNPG does not manage Certificate Revocation Lists (CRLs). If a certificate must
be invalidated before it expires, rotate the cluster's client CA: on the next
reconcile the operator detects that the existing certificates are no longer
signed by the current CA and re-issues all managed client certificates.
Alternatively, delete the certificate's Secret to have the operator issue a
fresh one signed by the current CA.
:::

### Deprecated password fields

A `DatabaseRole` inherits `passwordSecret` and `disablePassword` from the
shared role configuration it has in common with
[inline managed roles](#inline-managed-roles). Both still work, and neither is
going away in the short term, but on a `DatabaseRole` they are **deprecated**
in favor of the [`password` stanza](#choosing-a-mode), which covers what they
do and more:

| Deprecated field | Equivalent | What you also gain |
|---|---|---|
| `passwordSecret: {name: foo}` | `password: {mode: secret, secret: foo}` | the same Secret, read the same way, but stated in one place with the rest of the password configuration |
| `disablePassword: true` | `password: {mode: setNull}` | no separate flag whose interaction with `passwordSecret` has to be validated |
| *(neither set)* | `password: {mode: external}` | says explicitly that something else manages the password, instead of leaving it implied |
| *(no equivalent)* | `password: {mode: generate}` | the operator generates the password, with [criteria](#criteria) and [rotation](#rotation) |

The stanza and the deprecated fields are mutually exclusive: a role uses either
the stanza or the older fields, not both. See
[Migrating from inline managed roles to a `DatabaseRole`](#migrating-from-inline-managed-roles-to-a-databaserole)
for moving an existing role across.

:::important
There is no automatic migration. A role keeps using whichever form its
manifest specifies, so existing `DatabaseRole` resources continue to work
untouched. Adopting the stanza is an explicit edit, and one worth making
deliberately, because the stanza cannot be removed once set.
:::

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
condition appears once a password Secret is in use, whether
[generated by the operator](#mode-generate), read from one you supply with
[`mode: secret`](#mode-secret), or named by the deprecated `passwordSecret`
field, and is removed when the role no longer has one.

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

---

## Inline managed roles

With the `managed` stanza in the cluster spec, CloudNativePG provides
management for roles specified in `.spec.managed.roles`.
This feature enables declarative management of existing roles, as well as the
creation of new roles if they are not already present.

Inline managed roles cover the role attributes and their passwords. Password
generation, rotation and client certificate issuance are only available with
[`DatabaseRole` resources](#the-databaserole-resource).

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

### Passwords in inline managed roles

An inline managed role takes its password from a Secret you supply, through
`passwordSecret`:

```yaml
  passwordSecret:
    name: cluster-example-dante
```

The Secret follows the [basic-auth format](#supplying-a-password-in-a-secret)
described below.

If no `passwordSecret` is specified, the instance manager will not try to
`CREATE/ALTER` the role with a password, keeping with PostgreSQL conventions.

:::important
New roles created without `passwordSecret` will have a `NULL` password inside
PostgreSQL.
:::

To set a password to `NULL` explicitly, as opposed to simply not managing one,
use `disablePassword`:

``` yaml
  disablePassword: true
```

:::note
It is an error to set both `passwordSecret` and `disablePassword` on a given
role.
:::

These two fields are the only password controls available inline. On a
`DatabaseRole` they are [deprecated](#deprecated-password-fields) in favor of
the richer [`password` stanza](#choosing-a-mode).

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

### Moving the password across

Copying the inline stanza as-is carries `passwordSecret` and
`disablePassword` with it, and that keeps working. On a `DatabaseRole` those
fields are [deprecated](#deprecated-password-fields) though, so the migration
is a good moment to state the password through the
[`password` stanza](#choosing-a-mode) instead:

| Inline | On the `DatabaseRole` |
|---|---|
| `passwordSecret: {name: foo}` | `password: {mode: secret, secret: foo}` |
| `disablePassword: true` | `password: {mode: setNull}` |
| neither set | `password: {mode: external}`, which says outright that the password is managed elsewhere |

Moving to the stanza also opens up what inline roles cannot do at all: having
the operator [generate the password](#mode-generate), shape it with
[criteria](#criteria), and [rotate](#rotation) it on a schedule. Switching an
existing role to `mode: generate` replaces the password it currently has with
a generated one, so plan it like any other credential change: the consumers
of that role have to read the new password from the generated Secret.

:::important
Do the swap in one step: the `password` stanza is mutually exclusive with
`passwordSecret` and `disablePassword`, so a manifest that carries both is
rejected. Replace the old fields with the stanza in the same edit.
:::

---

## Password topics common to both methods

The sections below apply to roles managed either way. Where behavior differs
between a `DatabaseRole` and an inline managed role, it is called out.

### Supplying a password in a Secret

Both methods can take a role's password from a Secret you create: inline
through [`passwordSecret`](#passwords-in-inline-managed-roles), and on a
`DatabaseRole` through [`mode: secret`](#mode-secret) (or the deprecated
`passwordSecret`).

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

A Secret the operator [generates](#mode-generate) has the same format but does
not need the label: the operator owns it and reacts to any change to it.

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

#### Generated passwords with a lifetime

A `DatabaseRole` that has the operator [generate its password](#mode-generate)
with a `duration` owns the expiry of the role: `VALID UNTIL` follows the
expiration of the generated password, so PostgreSQL stops accepting that
password at the same moment the operator considers it expired. `validUntil`
cannot be set on such a role, and is rejected at admission: there would
otherwise be two competing answers to when the password stops working.

This makes the lifetime a real deadline rather than a convention. The operator
rotates the password `renewBefore` ahead of it, so under normal operation the
password is replaced before `VALID UNTIL` is ever reached.

:::warning
Because `VALID UNTIL` is a hard deadline in PostgreSQL, a rotation that does
not happen costs the role its access: if generation is blocked for longer than
`renewBefore` (an unsatisfiable `criteria`, a Secret the operator does not
own, a demotion to a [replica cluster](#replica-clusters), or an operator that
is down), the password expires and the role can no longer authenticate with it.
Watch `status.password` and pick a `renewBefore` that leaves room to notice
and fix a stalled rotation.
:::

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

Pre-hashed passwords apply to a Secret you supply. A password the operator
[generates](#mode-generate) is always cleartext in its Secret, and encoded on
the way to PostgreSQL as described below.

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
