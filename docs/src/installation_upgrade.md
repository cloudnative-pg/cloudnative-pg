# Installation and upgrades

## Installation on Kubernetes

### Directly using the operator manifest

The operator can be installed like any other resource in Kubernetes,
through a YAML manifest applied via `kubectl`.

You can install the [latest operator manifest](https://raw.githubusercontent.com/cloudnative-pg/cloudnative-pg/main/releases/cnpg-0.30.yaml)
for this minor release as follows:

```sh
kubectl apply --server-side -f \
  https://raw.githubusercontent.com/cloudnative-pg/cloudnative-pg/main/releases/cnpg-0.30.yaml
```

You can verify that with:

```sh
kubectl get deployment -n cnpg-system cnpg-controller-manager
```

### Using the `cnpg` plugin for `kubectl`

You can use the `cnpg` plugin to override the default configuration options
that are in the static manifests. 

For example, to generate the default latest manifest but change the watch
namespaces to only be a specific namespace, you could run:

```shell
kubectl cnpg install generate \
  --watch-namespace "specific-namespace" \
  > cnpg_for_specific_namespace.yaml
```

Please refer to ["`cnpg` plugin"](./kubectl-plugin.md#generation-of-installation-manifests) documentation
for a more comprehensive example. 

!!! Warning
    If you are deploying CloudNativePG on GKE and get an error (`... failed to
    call webhook...`), be aware that by default traffic between worker nodes
    and control plane is blocked by the firewall except for a few specific
    ports, as explained in the official
    [docs](https://cloud.google.com/kubernetes-engine/docs/how-to/private-clusters#add_firewall_rules)
    and by this
    [issue](https://github.com/cloudnative-pg/cloudnative-pg/issues/1360).
    You'll need to either change the `targetPort` in the webhook service, to be
    one of the allowed ones, or open the webhooks' port (`9443`) on the
    firewall.

### Testing the latest development snapshot

If you want to test or evaluate the latest development snapshot of
CloudNativePG before the next official patch release, you can download the
manifests from the
[`cloudnative-pg/artifacts`](https://github.com/cloudnative-pg/artifacts)
which provides easy access to the current trunk (main) as well as to each
supported release.

For example, you can install the latest snapshot of the operator with:

```sh
curl -sSfL \
  https://raw.githubusercontent.com/cloudnative-pg/artifacts/main/manifests/operator-manifest.yaml | \
  kubectl apply --server-side -f -
```

If you are instead looking for the latest snapshot of the operator for this
specific minor release, you can just run:

```sh
curl -sSfL \
  https://raw.githubusercontent.com/cloudnative-pg/artifacts/release-0.30/manifests/operator-manifest.yaml | \
  kubectl apply --server-side -f -
```

!!! Important
    Snapshots are not supported by the CloudNativePG Community, and are not
    intended for use in production.

### Using the Helm Chart

The operator can be installed using the provided [Helm chart](https://github.com/cloudnative-pg/charts).

### Using OLM

CloudNativePG can also be installed via the [Operator Lifecycle Manager (OLM)](https://olm.operatorframework.io/docs/)
directly from [OperatorHub.io](https://operatorhub.io/operator/cloudnative-pg).

For deployments on Red Hat OpenShift, EDB offers and fully supports a certified
version of CloudNativePG, available through the
[Red Hat OpenShift Container Platform](https://catalog.redhat.com/software/container-stacks/detail/653fd4035eece8598f66d97b).

## Details about the deployment

In Kubernetes, the operator is by default installed in the `cnpg-system`
namespace as a Kubernetes `Deployment`. The name of this deployment
depends on the installation method.
When installed through the manifest or the `cnpg` plugin, it is called
`cnpg-controller-manager` by default. When installed via Helm, the default name
is `cnpg-cloudnative-pg`.

!!! Note
    With Helm you can customize the name of the deployment via the
    `fullnameOverride` field in the [*"values.yaml"* file](https://helm.sh/docs/chart_template_guide/values_files/).

You can get more information using the `describe` command in `kubectl`:

```sh
$ kubectl get deployments -n cnpg-system
NAME                READY   UP-TO-DATE   AVAILABLE   AGE
<deployment-name>   1/1     1            1           18m
```

```sh
kubectl describe deploy \
  -n cnpg-system \
  <deployment-name>
```

As with any Deployment, it sits on top of a ReplicaSet and supports rolling
upgrades. The default configuration of the CloudNativePG operator
comes with a Deployment of a single replica, which is suitable for most
installations. In case the node where the pod is running is not reachable
anymore, the pod will be rescheduled on another node.

If you require high availability at the operator level, it is possible to
specify multiple replicas in the Deployment configuration - given that the
operator supports leader election. Also, you can take advantage of taints and
tolerations to make sure that the operator does not run on the same nodes where
the actual PostgreSQL clusters are running (this might even include the control
plane for self-managed Kubernetes installations).

!!! Seealso "Operator configuration"
    You can change the default behavior of the operator by overriding
    some default options. For more information, please refer to the
    ["Operator configuration"](operator_conf.md) section.

## Upgrades

!!! Important
    Please carefully read the [release notes](release_notes.md)
    before performing an upgrade as some versions might require
    extra steps.

Upgrading CloudNativePG operator is a two-step process:

1. upgrade the controller and the related Kubernetes resources
2. upgrade the instance manager running in every PostgreSQL pod

Unless differently stated in the release notes, the first step is normally done
by applying the manifest of the newer version for plain Kubernetes
installations, or using the native package manager of the used distribution
(please follow the instructions in the above sections).


The second step is automatically triggered after updating the controller. By
default, this initiates a rolling update of every deployed PostgreSQL cluster,
upgrading one instance at a time to use the new instance manager. The rolling
update concludes with a switchover, which is governed by the
`primaryUpdateStrategy` option. The default value, `unsupervised`, completes
the switchover automatically. If set to `supervised`, the user must manually
promote the new primary instance using the `cnpg` plugin for `kubectl`.

!!! Seealso "Rolling updates"
    This process is discussed in-depth on the [Rolling Updates](rolling_update.md) page.

!!! Important
    In case `primaryUpdateStrategy` is set to the default value of `unsupervised`,
    an upgrade of the operator will trigger a switchover on your PostgreSQL cluster,
    causing a (normally negligible) downtime. If your PostgreSQL Cluster has only one
    instance, the instance will be automatically restarted as `supervised` value is
    not supported for `primaryUpdateStrategy`. In either case, your applications will
    have to reconnect to PostgreSQL.

The default rolling update behavior can be replaced with in-place updates of
the instance manager. This approach does not require a restart of the
PostgreSQL instance, thereby avoiding a switchover within the cluster. This
feature, which is disabled by default, is described in detail below.

### Spread Upgrades

By default, all PostgreSQL clusters are rolled out simultaneously, which may
lead to a spike in resource usage, especially when managing multiple clusters.
CloudNativePG provides two configuration options at the [operator level](operator_conf.md)
that allow you to introduce delays between cluster roll-outs or even between
instances within the same cluster, helping to distribute resource usage over
time:

- `CLUSTERS_ROLLOUT_DELAY`: Defines the number of seconds to wait between
  roll-outs of different PostgreSQL clusters (default: `0`).
- `INSTANCES_ROLLOUT_DELAY`: Defines the number of seconds to wait between
  roll-outs of individual instances within the same PostgreSQL cluster (default:
  `0`).

### In-place updates of the instance manager

By default, CloudNativePG issues a rolling update of the cluster
every time the operator is updated. The new instance manager shipped with the
operator is added to each PostgreSQL pod via an init container.

However, this behavior can be changed via configuration to enable in-place
updates of the instance manager, which is the PID 1 process that keeps the
container alive.

Internally, each instance manager in CloudNativePG supports the injection of a
new executable that replaces the existing one after successfully completing an
integrity verification phase and gracefully terminating all internal processes.
Upon restarting with the new binary, the instance manager seamlessly adopts the
already running *postmaster*.

As a result, the PostgreSQL process is unaffected by the update, refraining
from the need to perform a switchover. The other side of the coin, is that
the Pod is changed after the start, breaking the pure concept of immutability.

You can enable this feature by setting the `ENABLE_INSTANCE_MANAGER_INPLACE_UPDATES`
environment variable to `'true'` in the
[operator configuration](operator_conf.md#available-options).

The in-place upgrade process will not change the init container image inside the
Pods. Therefore, the Pod definition will not reflect the current version of the
operator.

### Compatibility among versions

CloudNativePG follows semantic versioning. Every release of the
operator within the same API version is compatible with the previous one.
The current API version is v1, corresponding to versions 1.x.y of the operator.

In addition to new features, new versions of the operator contain bug fixes and
stability enhancements. Because of this, **we strongly encourage users to upgrade
to the latest version of the operator**, as each version is released in order to
maintain the most secure and stable Postgres environment.

CloudNativePG currently releases new versions of the operator at
least monthly. If you are unable to apply updates as each version becomes
available, we recommend upgrading through each version in sequential order to
come current periodically and not skipping versions.

The [release notes](release_notes.md) page contains a detailed list of the
changes introduced in every released version of CloudNativePG,
and it must be read before upgrading to a newer version of the software.

Most versions are directly upgradable and in that case, applying the newer
manifest for plain Kubernetes installations or using the native package
manager of the chosen distribution is enough.

When versions are not directly upgradable, the old version needs to be
removed before installing the new one. This won't affect user data but
only the operator itself.

### Upgrading to 1.25.0 or 1.24.2

!!! Important
    We encourage all existing users of CloudNativePG to upgrade to version
    1.25.0 or at least to the latest stable version of the minor release you are
    currently using (namely 1.24.x).

!!! Warning
    Every time you are upgrading to a higher minor release, make sure you
    go through the release notes and upgrade instructions of all the
    intermediate minor releases. For example, if you want to move
    from 1.23.x to 1.25, make sure you go through the release notes
    and upgrade instructions for 1.24 and 1.25.

No changes to existing 1.24 cluster configurations are required when upgrading
to 1.25.

### Upgrading to 1.24 from a previous minor version

#### From Replica Clusters to Distributed Topology

One of the key enhancements in CloudNativePG 1.24.0 is the upgrade of the
replica cluster feature.

The former replica cluster feature, now referred to as the "Standalone Replica
Cluster," is no longer recommended for Disaster Recovery (DR) and High
Availability (HA) scenarios that span multiple Kubernetes clusters. Standalone
replica clusters are best suited for read-only workloads, such as reporting,
OLAP, or creating development environments with test data.

For DR and HA purposes, CloudNativePG now introduces the Distributed Topology
strategy for replica clusters. This new strategy allows you to build PostgreSQL
clusters across private, public, hybrid, and multi-cloud environments, spanning
multiple regions and potentially different cloud providers. It also provides an
API to control the switchover operation, ensuring that only one cluster acts as
the primary at any given time.

This Distributed Topology strategy enhances resilience and scalability, making
it a robust solution for modern, distributed applications that require high
availability and disaster recovery capabilities across diverse infrastructure
setups.

You can seamlessly transition from a previous replica cluster configuration to a
distributed topology by modifying all the `Cluster` resources involved in the
distributed PostgreSQL setup. Ensure the following steps are taken:

- Configure the `externalClusters` section to include all the clusters involved
  in the distributed topology. We strongly suggest using the same configuration
  across all `Cluster` resources for maintainability and consistency.
- Configure the `primary` and `source` fields in the `.spec.replica` stanza to
  reflect the distributed topology. The `primary` field should contain the name
  of the current primary cluster in the distributed topology, while the `source`
  field should contain the name of the cluster each `Cluster` resource is
  replicating from. It is important to note that the `enabled` field, which was
  previously set to `true` or `false`, should now be unset (default).

For more information, please refer to
the ["Distributed Topology" section for replica clusters](replica_cluster.md#distributed-topology).

### Upgrading to 1.23 from a previous minor version

#### User defined replication slots

CloudNativePG now offers automated synchronization of all replication slots
defined on the primary to any standby within the High Availability (HA)
cluster.

If you manually manage replication slots on a standby, it is essential to
exclude those replication slots from synchronization. Failure to do so may
result in CloudNativePG removing them from the standby. To implement this
exclusion, utilize the following YAML configuration. In this example,
replication slots with a name starting with 'foo' are prevented from
synchronization:

```yaml
...
  replicationSlots:
    synchronizeReplicas:
      enabled: true
      excludePatterns:
      - "^foo"
```

Alternatively, if you prefer to disable the synchronization mechanism entirely,
use the following configuration:

```yaml
...
  replicationSlots:
    synchronizeReplicas:
      enabled: false
```

#### Server-side apply of manifests

To ensure compatibility with Kubernetes 1.29 and upcoming versions,
CloudNativePG now mandates the utilization of
["Server-side apply"](https://kubernetes.io/docs/reference/using-api/server-side-apply/)
when deploying the operator manifest.

While employing this installation method poses no challenges for new
deployments, updating existing operator manifests using the `--server-side`
option may result in errors resembling the example below:

``` text
Apply failed with 1 conflict: conflict with "kubectl-client-side-apply" using..
```

If such errors arise, they can be resolved by explicitly specifying the
`--force-conflicts` option to enforce conflict resolution:

```sh
kubectl apply --server-side --force-conflicts -f <OPERATOR_MANIFEST>
```

Henceforth, `kube-apiserver` will be automatically acknowledged as a recognized
manager for the CRDs, eliminating the need for any further manual intervention
on this matter.
