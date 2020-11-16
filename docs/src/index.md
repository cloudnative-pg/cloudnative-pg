# Cloud Native PostgreSQL

**Cloud Native PostgreSQL** is a stack designed by [EnterpriseDB](https://www.enterprisedb.com)
to manage [PostgreSQL](https://www.postgresql.org/) workloads on [Kubernetes](https://kubernetes.io),
particularly optimised for Private Cloud environments with Local Persistent Volumes (PV).

Cloud Native PostgreSQL defines a new Kubernetes resource called *Cluster* that
represents a PostgreSQL cluster made up of a single primary and an optional number
of replicas that co-exist in a chosen Kubernetes namespace.

PostgreSQL 13, 12, 11 and 10 are currently supported.

Cloud Native PostgreSQL has also been certified for
[RedHat OpenShift Container Platform (OCP)](https://www.openshift.com/products/container-platform)
4.5+ and is available directly from the [RedHat Catalog](https://catalog.redhat.com/).
OpenShift Container Platform is an open source distribution of Kubernetes which is
maintained and commercially supported by Red Hat.

## Main features

* Direct integration with Kubernetes API server for High Availability,
  without requiring an external tool
* Self-Healing capability, through:
    * failover of the primary instance, by promoting the most aligned replica
    * automated recreation of a replica
* Planned switchover of the primary instance, by promoting a selected replica
* Scale up/down capabilities
* Definition of an arbitrary number of instances (minimum 1 - one primary server)
* Definition of the *read-write* service, to connect your applications to the only primary server of the cluster
* Definition of the *read-only* service, to connect your applications to any of the instances for read workloads
* Support for Local Persistent Volumes with PVC templates
* Reuse of Persistent Volumes storage in Pods
* Rolling updates for PostgreSQL minor versions and operator upgrades
* Continuous backup to an S3 compatible object store
* Standard output logging of PostgreSQL error messages

## Requirements on Kubernetes

Cloud Native PostgreSQL requires Kubernetes 1.15 or higher, tested on AWS, Google, Azure (with multiple availability zones).

!!! Warning
    These requirements do not apply to Cloud Native PostgreSQL on RedHat Open Container Platform.

## About this guide

Follow the instructions in the ["Quickstart"](quickstart.md) to test Cloud Native PostgreSQL
on a local Kubernetes cluster using Minikube or Kind.

In case you are not familiar with some basic terminology on Kubernetes and PostgreSQL,
please consult the ["Before you start" section](before_you_start.md).

!!! Note
    Although the guide primarily addresses Kubernetes, all concepts can
    be extended to OpenShift as well.
