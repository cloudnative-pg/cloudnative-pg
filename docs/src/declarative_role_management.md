# Database role management

From its inception, CloudNativePG has managed the creation of specific roles
required in PostgreSQL instances:

- Some reserved users, such as the postgres superuser, streaming_replica,
  and cnpg_pooler_pgbouncer (when the PgBouncer `Pooler` is used)
- The application user, set as the low-privilege owner of the application database

This process is described in [Bootstrap](bootstrap.md).

With the `managed` stanza in the cluster spec, CloudNativePG now provides full
lifecycle management for roles specified in `.spec.managed.roles`.
This feature enables declarative management of existing roles, as well as the
creation of roles if they aren't already present in the database. Role
creation occurs after the database bootstrapping is complete.

An example manifest for a cluster with declarative role management can be found
in the file [`cluster-example-with-roles.yaml`](samples/cluster-example-with-roles.yaml).

Here's an excerpt from that file:

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
See the [API reference](cloudnative-pg.v1.md#postgresql-cnpg-io-v1-RoleConfiguration) for
the full list of attributes you can define for each role.

A few points are worth noting:

1. The `ensure` attribute isn't part of PostgreSQL. It enables declarative
   role management to create and remove roles. The two possible values are
   `present` (the default) and `absent`.
2. The `inherit` attribute is true by default, following PostgreSQL conventions.
3. The `connectionLimit` attribute defaults to -1, in line with PostgreSQL conventions.
4. Role membership with `inRoles` defaults to no memberships.

Declarative role management ensures that PostgreSQL instances align with the
spec. If a user modifies role attributes directly in the database, the
CloudNativePG operator reverts those changes during the next reconciliation
cycle.

## Password management

The declarative role management feature includes reconciling role passwords.
Passwords are managed in fundamentally different ways in the Kubernetes realm
and in PostgreSQL. As a result, there are a few things to note.

Managed role configurations can optionally specify the name of a
secret where the username and password are stored (encoded in Base64
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

This code assumes the existence of a secret called `cluster-example-dante` that
contains a username and password. The username must match the role you're
setting the password for. For example:

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

If no `passwordSecret` is specified for a role, the instance manager doesn't
try to CREATE or ALTER the role with a password. This behavior keeps with PostgreSQL
conventions, where ALTER doesn't update passwords unless directed to using
`WITH PASSWORD`.

If a role was initially created with a password, and you want to set the
password to NULL in PostgreSQL, this requires
the user of CloudNativePG to be explicit.
To distinguish "no password provided in spec" from "set the password to NULL,"
use the field `DisablePassword`.

Suppose you decide you want to have no password on the `dante` role in the
database. In this case, specify the following:

``` yaml
  managed:
    roles:
    - name: dante
      ensure: present
      [… snipped …]
      disablePassword: true
```

!!! Note
    It's an error to set both `passwordSecret` and
    `disablePassword` on a given role.
    This configuration is rejected by the validation webhook.

### Password expiry, `VALID UNTIL`

The `VALID UNTIL` role attribute in PostgreSQL controls password expiry. Roles
created without `VALID UNTIL` specified get NULL by default in PostgreSQL,
meaning that their password never expires.

PostgreSQL uses a timestamp type for `VALID UNTIL`, which includes support for
the value `'infinity'` indicating that the password never expires. See the
[PostgreSQL documentation](https://www.postgresql.org/docs/current/datatype-datetime.html)
for reference.

With declarative role management, the `validUntil` attribute for managed roles
controls password expiry. `validUntil` can only either:

- Take a Kubernetes timestamp
- Be omitted (defaulting to `null`)

In the first case, the given `validUntil` timestamp is set in the database
as the `VALID UNTIL` attribute of the role.

In the second case (omitted `validUntil`) the operator ensures the password
never expires, mirroring the behavior of PostgreSQL. Specifically:

- In the case of a new role, it omits the `VALID UNTIL` clause in the role
  creation statement.
- In the case of an existing role, it sets `VALID UNTIL` to `infinity` if `VALID
  UNTIL` wasn't set to `NULL` in the database. (This is due to PostgreSQL not
  allowing `VALID UNTIL NULL` in the `ALTER ROLE` SQL statement.)

!!! Warning
    The declarative role management feature has changed behavior since its
    initial version (1.20.0). In 1.20.0, a role without a `passwordSecret`
    led to setting the password to NULL in PostgreSQL.
    In practice, there's little difference from 1.20.0.
    New roles created without `passwordSecret` have a `NULL` password.
    The relevant change is when using the managed roles to manage roles that
    were created previously. In 1.20.0, doing this might inadvertently
    result in setting existing passwords to `NULL`.

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

## Unrealizable role configurations

In PostgreSQL, in some cases, the database can't honor the commands and
the commands are rejected. See the
[PostgreSQL documentation on error codes](https://www.postgresql.org/docs/current/errcodes-appendix.html)
for details.

Role operations can produce such fundamental errors.
Two examples:

- Asking PostgreSQL to create the role `petrarca` as a member of the role
  (group) `poets`, but `poets` doesn't exist
- Asking PostgreSQL to drop the role `dante`, but the role `dante` is the owner
  of the database `inferno`

These fundamental errors can't be fixed by the database or the CloudNativePG
operator without clarification from the human administrator. The two examples 
could be fixed by creating the role `poets` or dropping the database
`inferno`, respectively. However, they might have originated due to human error. 
In that case, the proposed fix might be the wrong thing to do.

CloudNativePG  records when such fundamental errors occur and displays
them in the cluster status. 

## Status of managed roles

The CRD status includes a section for the managed roles' status. For example:

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

The special subsection `cannotReconcile` is for operations the database (and
CloudNativePG) can't honor and that require human intervention.
This section covers roles reserved for operator use and those that aren't
under declarative management, providing a comprehensive view of the roles in
the database instances.

!!! Important
    In terms of backward compatibility, declarative role management is designed
    to ignore roles that exist in the database but aren't included in the spec.
    The lifecycle of these roles will continue to be managed in PostgreSQL,
    allowing CloudNativePG users to adopt this feature at their convenience.
