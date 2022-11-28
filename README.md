# Welcome to the CloudNativePG project!

**CloudNativePG** is an open source operator designed to manage
[PostgreSQL](https://www.postgresql.org/) workloads on any supported Kubernetes
cluster running in private, public, hybrid, or multi-cloud environments.

CloudNativePG was originally built and sponsored by [EDB](https://www.enterprisedb.com).

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

CloudNativePG also manages additional resources to help the `Cluster` resource
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

- [Slack Channel](https://join.slack.com/t/cloudnativepg/shared_invite/zt-17culux7k-P_UsVOOR9teUYi4dGhDSBQ)
- [Github Discussions](https://github.com/cloudnative-pg/cloudnative-pg/discussions)
- [Twitter](https://twitter.com/CloudNativePg)

## Resources

- [Roadmap](https://github.com/orgs/cloudnative-pg/projects/1)
- [Website](https://cloudnative-pg.io)
- [FAQ](docs/src/faq.md)
- [Blog](https://cloudnative-pg.io/blog/)
- ["Why Run Postgres in Kubernetes?"](https://containerjournal.com/kubecon-cnc-eu-2022/why-run-postgres-in-kubernetes/) (May 2022)
- ["Shift-Left Security: The Path To PostgreSQL On Kubernetes"](https://www.tfir.io/shift-left-security-the-path-to-postgresql-on-kubernetes/) (April 2021)
- ["Local Persistent Volumes and PostgreSQL usage in Kubernetes"](https://www.2ndquadrant.com/en/blog/local-persistent-volumes-and-postgresql-usage-in-kubernetes/) (June 2020)

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
