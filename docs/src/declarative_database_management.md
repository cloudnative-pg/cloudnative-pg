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
the database created in Postgres.

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

### Database objects defined on  replica clusters

Database objects declared on a replica cluster cannot be enforced, since the
replica does not have write privileges.
Instead, a database object defined on a replica cluster will be periodically
re-queued, and will be enforced once the cluster is promoted.

### Reserved names

PostgreSQL creates the `postgres` database, as well as `template0` and
`template1`. Those names are therefore reserved for Postgres use. You will not
be allowed to create a Database with any of `postgres`, `template0`, or
`template1` as the `spec.name`.

### Status sub-resource

Once the instance manager has completed the reconciliation of a Database,
the Database status will be updated with a `ready` field set to `true`, and an
`observedGeneration` field which keeps track of the last applied `generation`.
If there were errors during the reconciliation of a database, the `ready` field
would show `false`, and an additional field `error` would be displayed in the
status.

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
  ready: true
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

## Collision of Database objects

As mentioned above, the Database CRD has the fields `metadata.name` and
`spec.name`, which are individually settable. A situation can arise where two
Database objects refer to the same Postgres database (i.e. they have
identical `spec.name` and `spec.cluster.name`).

The database reconciler could simply apply them both. The first applied would
result in `CREATE` statements (assuming the database did not exist in Postgres),
while the second one would result in `ALTER` statements.
While this could work, it could lead to unexpected behavior: given two Database
objects managing the same Postgres database, it would not be clear which one
would be reflected in Postgres in the long term.

For this reason, the database reconciler will check, given a Database object,
if there is already another Database object managing the same database.
If so, it will mark it with an error explaining this, and will not apply any
changes in Postgres:

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
  ready: false
  error: this Database clashes with the previous `db-one` managing database `declarative`
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
  ready: false
  error: option "locale_provider" not recognized
```

The rationale is that this is exactly what will happen if you attempt to create
a database directly on the `psql` command line. The database reconciler aims
at transparency.

## Making direct changes in Postgres

It is possible to make changes to a database that was created or managed with a
Database object, directly on Postgres, for example by issuing commands on
`psql`.

The fields `observedGeneration` and `generation` described above will ensure
that once a Database has been reconciled to its defined `generation`, it will
not be re-applied by the instance manager. Therefore, your manual changes will
not be rolled back inadvertently.

!!! Note
    A Database manifest is applied to the fullest. A field included in a
    manifest may override a value that had been written previously on
    the database in Postgres

CloudNativePG gives you the flexibility to make alterations on your databases
via Database manifests, via direct changes, or mixing matching to fit your
use case.
