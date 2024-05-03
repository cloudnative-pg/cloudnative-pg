# Production Readiness Guide

Running a production grade PostgreSQL system requires effort and preparation. If
you're coming to CloudNativePG with PostgreSQL experience, or with other
relational database experience, you should already know much of what you need
to have a production-ready CNPG system, with the additional benefits brought by
the Kubernetes ecosystem.

Whether a novice or an expert, please review this document to make
sure you're production ready with CloudNativePG. The following sections should
be considered a minimum.

## Set up a regular backup regime

You should not have a database in production without taking regular backups.
CloudNativePG can help with scheduled backups, using object stores or volume
snapshots.

See [CNPG backup documentation](https://cloudnative-pg.io/documentation/current/backup/).

## Regularly test restore from backup

Having backups is essential. It is equally essential to test those backups.
CloudNativePG makes it very easy to spin up a cluster from a backup. You should
regularly check backups are recent and in good condition.

See [CNPG recovery documentation](https://cloudnative-pg.io/documentation/current/recovery/).

## Set up monitoring

It goes without saying, a production database should be monitored, not only for
uptime, load etc., but for space. In addition, it is vitally important to ensure
that WAL files don't pile up and potentially fill your `PGDATA` volume, or your
dedicated WAL storage volume. The [CloudNativePG Quickstart guide](https://cloudnative-pg.io/documentation/current/quickstart/) includes
instructions on setting up a Grafana / Prometheus monitoring system. Prometheus
also allows you to define Alerts.

See [CNPG quickstart guide](https://cloudnative-pg.io/documentation/current/quickstart/).

Monitoring a production environment is important, and especially so with a
database.

## Know your Kubernetes environment

CloudNativePG provides a lot of functionality on top of PostgreSQL by leveraging
the Kubernetes ecosystem. There are many vendors and environments to choose
from. For your production environment you should know what to expect. For
example:

* Does your storage class support volume resizing? See
  [volume expansion documents](https://cloudnative-pg.io/documentation/current/storage/#volume-expansion).
* Do you have overly restrictive Network Policies? See
  [troubleshooting document](https://cloudnative-pg.io/documentation/current/troubleshooting/#networking).

## Have a Kubernetes installation in good working order

We see issues where an investigation reveals an out-of-memory
Kubernetes installation, processors at capacity, or a network with large
latency.
Kubernetes could be thought of as the Operating System for CloudNativePG
and as such it must be functioning properly for CloudNativePG to work reliably. 

## Know how basic CNPG operations work

It goes without saying that before going to production, you should know how
CloudNativePG works in practice. For example, you should test performing a
manual switchover, you should try killing your test database's primary pod and
see how soon it recovers, you should try resizing your database PVC's.

It is also important to know how to use the debugging tools, which is the topic
of the following section:

## Know how to perform some debugging steps

You should know the basics of inspecting your CloudNativePG clusters and the
operator. The Troubleshooting documentation includes some basic steps. It would
be recommendable that you install the suggested tools in this section:

See [troubleshooting tools](https://cloudnative-pg.io/documentation/current/troubleshooting/#before-you-start).

These tools will be of great help when you ask for assistance, and in addition,
they will help you become better acquainted with your system, so it is
worthwhile to learn to use them regularly.

## Know your database

You should also be familiar with your workload for the database. There are many
useful parameters to optimize your PostgreSQL database. That is a large topic
that deserves careful study. The
[PostgreSQL documentation and books](https://www.postgresql.org/docs/) are very
helpful.  You should also be aware of how to map
your desired PostgreSQL configuration into CNPG. Refer to the
[CloudNativePG documentation](https://cloudnative-pg.io/documentation/current/).
