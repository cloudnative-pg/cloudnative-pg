# Declarative Database Management

## The Database Custom Resource Definition

Declarative database management enables users to control the lifecycle of
databases in PostgreSQL via the `Database` Custom Resource Definition (CRD).

### Example: Simple Database Declaration

Below is an example of a basic `Database` manifest:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Database
metadata:
  name: db-one
spec:
  name: one
  owner: app
  cluster:
    name: cluster-example
```

Note that the Database references a Cluster, on which the database will be
created.
The Database object is managed by the instance manager of the cluster's
primary instance.

In the CRD, the `metadata.name` field represents the name the object
will have in Kubernetes, which is guaranteed to be unique per namespace.
There is also the field `spec.name`, which is the name that will be used for
the database in Postgres.

!!! Note
    Having separate `metadata.name` and `spec.name` makes it possible to have
    two different CloudNativePG clusters in the same namespace, with Databases
    that have the same name in Postgres.

!!! Warning
    While declarative database management in CloudNativePG adheres to the
    PostgreSQL database
    [CREATE](https://www.postgresql.org/docs/current/sql-createdatabase.html)
    and [ALTER](https://www.postgresql.org/docs/current/sql-alterdatabase.html)
    commands, it does not support renaming of databases. Changing the
    `spec.name` in a Database object will be rejected at the Kubernetes level.

### Managing an existing database via a Database manifest

It is possible to declare a Database object that will handle an existing
database. In such case, the Database's fields will be applied using `ALTER`
statements, rather than `CREATE`. There are differences between these two
Postgres commands. In particular, the options accepted by `ALTER` are a subset
of those accepted by `CREATE`.

The database reconciler will transparently handle this on behalf of the user,
making it easy to honor a Database manifest no matter the previous history
of the cluster.

There is however a difference regarding the handling of "immutable" fields: on
an existing Database object, any modification of the immutable fields will
be rejected at the Kubernetes level.
On a newly declared Database manifest that references an existing database, the
immutable fields will simply be ignored, as they are not valid options in the
`ALTER DATABASE` command.

!!! Warning
    If a Database manifest references an existing database, any fields in the
    manifest that cannot be set in `ALTER DATABASE` will be ignored.
    Notably, the options around encoding and collations, as well as the template
    used, are immutable and not supported in `ALTER`.

### Database objects defined on  replica clusters

Database objects declared on a replica cluster cannot be enforced, since the
replica does not have write privileges.
Instead, a Database object defined on a replica cluster will be periodically
re-queued, and will be enforced once the cluster is promoted.

### Reserved names

PostgreSQL creates the `postgres` database, as well as `template0` and
`template1`. Those names are therefore reserved for Postgres use. You will not
be allowed to create a Database with any of `postgres`, `template0`, or
`template1` as the `spec.name`.

### Status sub-resource

Once the reconciliation of a Database has been performed,
the Database status will be updated with a field,  `applied`, set to `true`,
and an `observedGeneration` field which tracks the last applied `generation`.
If there were errors during the reconciliation of a database, the `applied`
field would show `false`, and an additional field `message` would be displayed
in the status.

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Database
metadata:
  [... snipped ...]
  generation: 1
  name: db-one
spec:
  cluster:
    name: cluster-example
  name: declarative
  owner: app
  template: template0
status:
  observedGeneration: 1
  applied: true
```

## Database Deletion and Reclaim Policies

A finalizer named `cnpg.io/deleteDatabase` is automatically added
to each Database object to control its deletion process.

By default, the `databaseReclaimPolicy` is set to `retain`, which means
that if the Database object is deleted, the underlying PostgreSQL database
is retained for manual management by an administrator.

Alternatively, if the `databaseReclaimPolicy` is set to `delete`,
the PostgreSQL database will be automatically deleted when the Database
object is removed.

### Example: Database with Delete Reclaim Policy

The following example illustrates a Database object with a `delete`
reclaim policy:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Database
metadata:
  name: db-one-with-delete-reclaim-policy
spec:
  databaseReclaimPolicy: delete
  name: two
  owner: app
  cluster:
    name: cluster-example
```

In this case, when the Database object is deleted, the corresponding PostgreSQL
database will also be removed automatically.

## Imperative deletion of a PostgreSQL database

In the previous section, the database Reclaim Policy was discussed, which serves
in case the Postgres database should be dropped once the Database object is
deleted.

For the purpose of deleting an existing Postgres database via a Database
declaration, the `ensure` field may be use. By default its value is `present`.
Setting it to `absent` will have the effect of dropping the database in the next
reconciliation cycle.

### Example deletion of a Postgres database

In the following example, `ensure: absent` has the effect of dropping the
Postgres database. Since `applied` is true, we know the database was
successfully dropped.

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Database
metadata:
  [... snipped ...]
  generation: 1
  name: db-one-deleter
spec:
  cluster:
    name: cluster-example
  name: database-to-drop
  owner: app
  ensure: absent
status:
  observedGeneration: 1
  applied: true
```

## Collision of Database objects

As mentioned above, the Database CRD has the fields `metadata.name` and
`spec.name`, which are individually settable. A situation can arise where two
Database objects refer to the same Postgres database (i.e. they have
identical `spec.name` and `spec.cluster.name`).

The database reconciler could simply apply them both. The first applied would
result in `CREATE` statements (assuming the database did not exist in Postgres),
while the second one would result in `ALTER` statements.
While this could work, it would make it much harder to reason about Database
objects. There would be uncertainty as to the order of operations.

For this reason, the database reconciler will check, given a Database object,
if there is already another Database object managing the same database.
If so, it will update its status with a message explaining this, and will not
apply any changes in Postgres:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Database
metadata:
  [... snipped ...]
  generation: 1
  name: db-duplicate
spec:
  cluster:
    name: cluster-example
  name: declarative
  owner: app
status:
  applied: false
  message: this Database clashes with the previous `db-one` managing database `declarative`
```

## Support of different Postgres versions

The DDL for databases in Postgres keeps evolving. For example, the option
[`ICU_RULES`](https://www.postgresql.org/docs/16/sql-createdatabase.html) has
been introduced with Postgres 16 and is not available in previous versions.

The database reconciler will apply all the fields declared in the `spec`, and
will transparently relay back any error messages from Postgres, leaving it to
the user to take appropriate steps.

For example, applying the following Database manifest:

```yaml
apiVersion: postgresql.cnpg.io/v1
  kind: Database
  metadata:
    name: db-icu
spec:
  name: declarative-icu
  owner: app
  encoding: UTF8
  locale_provider: icu
  icu_locale: en
  icu_rules: fr
  template: template0
  cluster:
    name: cluster-example
```

on a cluster running Postgres 14 will result in an error message in the
Database object's status:

```yaml
[...]
status:
  applied: false
  error: option "locale_provider" not recognized
```

The rationale is that this is exactly what will happen if you attempt to create
a database directly on the `psql` command line. The database reconciler aims
at transparency.

## Making direct changes in Postgres

It is possible to make changes directly in Postgres to a database that was
created or managed with a Database object.

The fields `observedGeneration` and `generation` described above will ensure
that once a Database has been reconciled to its defined `generation`, it will
not be re-applied by the instance manager. Therefore, your manual changes will
not be rolled back inadvertently.

CloudNativePG gives you the flexibility to make your databases via Database
manifests, via direct changes, or mixing matching to fit your use case.
