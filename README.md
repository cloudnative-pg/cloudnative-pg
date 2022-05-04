# Welcome to the CloudNativePG project!

**CloudNativePG** is an open source operator designed to manage
[PostgreSQL](https://www.postgresql.org/) workloads on any supported Kubernetes
cluster running in private, public, hybrid, or multi-cloud environments.

CloudNativePG was originally built and sponsored by [EDB](https://www.enterprisedb.com).

## Table of content

- [Code of conduct](CODE_OF_CONDUCT.md)
- [Governance policies](GOVERNANCE.md)
- [Contributing](CONTRIBUTING.md)
- [License](LICENSE)

## Getting Started

The best way to get started is with the ["Quickstart"](docs/src/quickstart.md)
section in the documentation.

## Scope

The goal of CloudNativePG is to increase the adoption of PostgreSQL, one of the
most loved DBMS in traditional VM and bare metal environments, inside
Kubernetes, thus making the database an integral part of the development
process and CI/CD automated pipelines.

### In scope

CloudNativePG has been designed by Postgres experts with Kubernetes
administrators in mind. Put simply, it leverages Kubernetes by extending its
controller and by defining, in a programmatic way, all the actions that a good
DBA would normally do when managing a highly available PostgreSQL database
cluster.

Since the inception, our philosophy has been to adopt a Kubernetes native
approach to PostgreSQL cluster management, making incremental decisions that
would answer the fundamental question: "What would a Kubernetes user expect
from a Postgres operator?".

The most important decision we made is to have the status of a PostgreSQL
cluster directly available in the `Cluster` resource, so to inspect it through
the Kubernetes API. We've fully embraced the operator pattern and eventual
consistency, two of the core principles upon which Kubernetes is built for
managing complex applications.

As a result, the operator is responsible for managing the status of the
`Cluster` resource, keeping it up to date with the information that each
PostgreSQL instance manager regularly reports back through the API server. Such
changes might trigger, for example, actions like:

* a PostgreSQL failover where, after an unexpected failure of a cluster's
  primary instance, the operator itself elects the new primary, updates the
  status, and directly coordinates the operation through the reconciliation
  loop, by relying on the instance managers

* scaling up or down the number of read-only replicas, based on a positive or
  negative variation in the number of desired instances in the cluster, so that
  the operator creates or removes the required resources to run PostgreSQL,
  such as persistent volumes, persistent volume claims, pods, secrets, config
  maps, and then coordinates cloning and streaming replication tasks

* updates of the endpoints of the PostgreSQL services that applications rely on
  to interact with the database, as Kubernetes represents the single source of
  truth and authority

* updates of container images in a rolling fashion, following a change in the
  image name, by first updating the pods where replicas are running, and then
  the primary, issuing a switchover first

The latter example is based on another pillar of CloudNativePG:
immutable application containers - as explained in the
[blog article "Why EDB Chose Immutable Application Containers"](https://www.enterprisedb.com/blog/why-edb-chose-immutable-application-containers).

The above list can be extended. However, the gist is that CloudNativePG
exclusively relies on the Kubernetes API server and the instance manager to
coordinate the complex operations that need to take place in a business
continuity PostgreSQL cluster, without requiring any assistance from an
intermediate management tool responsible for high availability and failover
management like similar open source operators.

CloudNativePG also manages additional resources the help the `Cluster` resource
manage PostgreSQL - currently `ScheduledBackup`, `Pooler`, and `Backup`.

Fully embracing Kubernetes also means that, in case of failure of the whole
Kubernetes cluster, the operator won’t do anything, postponing any decision to
when the cluster is back up again. In the meantime, Postgres instances should
continue running according to the last known state of the cluster.

### Out of scope

CloudNativePG is exclusively focused on the PostgreSQL database management
system maintained by the PostgreSQL Global Development Group (PGDG). We are not
currently considering adding to CloudNativePG extensions or capabilities that
are included in forks of the PostgreSQL database management system, unless in
the form of extensible or pluggable frameworks.

CloudNativePG doesn't intend to pursue database independence (e.g. control a
MariaDB cluster).

## Communications

- [Slack Channel](https://cloudnativepg.slack.com)
- [Github Discussions](https://github.com/cloudnative-pg/cloudnative-pg/discussions)
- [Twitter](https://twitter.com/CloudNativePg)

## Resources

- [Roadmap](https://github.com/orgs/cloudnative-pg/projects/1)
- [Website](https://cloudnative-pg.io)
- [FAQ](docs/src/faq.md)

## Maintainers

The current maintainers of the CloudNativePG project are:

- Gabriele Bartolini (EDB)
- Francesco Canovai (EDB)
- Leonardo Cecchi (EDB)
- Jonathan Gonzalez (EDB)
- Marco Nenciarini (EDB)
- Philippe Scorsolini (controlplane.io)

They are listed in the [CODEOWNERS](CODEOWNERS) file.

## Trademarks

*[Postgres, PostgreSQL and the Slonik Logo](https://www.postgresql.org/about/policies/trademarks/)
are trademarks or registered trademarks of the PostgreSQL Community Association
of Canada, and used with their permission.*
