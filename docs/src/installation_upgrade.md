# Installation and upgrades

## Installation on Kubernetes

### Directly using the operator manifest

The operator can be installed like any other resource in Kubernetes,
through a YAML manifest applied via `kubectl`.

You can install the [latest operator manifest](https://raw.githubusercontent.com/cloudnative-pg/cloudnative-pg/release-1.23/releases/cnpg-1.23.0.yaml)
for this minor release as follows:

```sh
kubectl apply --server-side -f \
  https://raw.githubusercontent.com/cloudnative-pg/cloudnative-pg/release-1.23/releases/cnpg-1.23.0.yaml
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
  https://raw.githubusercontent.com/cloudnative-pg/artifacts/release-1.23/manifests/operator-manifest.yaml | \
  kubectl apply --server-side -f -
```

!!! Important
    Snapshots are not supported by the CloudNativePG and not intended for production usage.

### Using the Helm Chart

The operator can be installed using the provided [Helm chart](https://github.com/cloudnative-pg/charts).

### Using OLM

CloudNativePG can also be installed using the
[Operator Lifecycle Manager (OLM)](https://olm.operatorframework.io/docs/)
directly from [OperatorHub.io](https://operatorhub.io/operator/cloudnative-pg).

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

!!! Warning
    If you are upgrading to version 1.20, please read carefully
    the [dedicated section below](#upgrading-to-120-from-a-previous-minor-version).

Upgrading CloudNativePG operator is a two-step process:

1. upgrade the controller and the related Kubernetes resources
2. upgrade the instance manager running in every PostgreSQL pod

Unless differently stated in the release notes, the first step is normally done
by applying the manifest of the newer version for plain Kubernetes
installations, or using the native package manager of the used distribution
(please follow the instructions in the above sections).

The second step is automatically executed after having updated the controller,
by default triggering a rolling update of every deployed PostgreSQL instance to
use the new instance manager. The rolling update procedure culminates with a
switchover, which is controlled by the `primaryUpdateStrategy` option, by
default set to `unsupervised`. When set to `supervised`, users need to complete
the rolling update by manually promoting a new instance through the `cnpg`
plugin for `kubectl`.

!!! Seealso "Rolling updates"
    This process is discussed in-depth on the [Rolling Updates](rolling_update.md) page.

!!! Important
    In case `primaryUpdateStrategy` is set to the default value of `unsupervised`,
    an upgrade of the operator will trigger a switchover on your PostgreSQL cluster,
    causing a (normally negligible) downtime.

Since version 1.10.0, the rolling update behavior can be replaced with in-place
updates of the instance manager. The latter don't require a restart of the
PostgreSQL instance and, as a result, a switchover in the cluster.
This behavior, which is disabled by default, is described below.

### In-place updates of the instance manager

By default, CloudNativePG issues a rolling update of the cluster
every time the operator is updated. The new instance manager shipped with the
operator is added to each PostgreSQL pod via an init container.

However, this behavior can be changed via configuration to enable in-place
updates of the instance manager, which is the PID 1 process that keeps the
container alive.

Internally, any instance manager from version 1.10 of CloudNativePG
supports injection of a new executable that will replace the existing one,
once the integrity verification phase is completed, as well as graceful
termination of all the internal processes. When the new instance manager
restarts using the new binary, it adopts the already running *postmaster*.

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

<!--
### Upgrading to 1.23.0, 1.22.2 or 1.21.4

!!! Important
    We encourage all existing users of CloudNativePG to upgrade to version
    1.23.0 or at least to the latest stable version of the minor release you are
    currently using (namely 1.22.2 or 1.21.4).

!!! Warning
    Every time you are upgrading to a higher minor release, make sure you
    go through the release notes and upgrade instructions of all the
    intermediate minor releases. For example, if you want to move
    from 1.21.x to 1.23, make sure you go through the release notes
    and upgrade instructions for 1.22 and 1.23.

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

-->

### Upgrading to 1.22 from a previous minor version

CloudNativePG continues to adhere to the security-by-default approach. As of
version 1.22, the usage of the `ALTER SYSTEM` command is now disabled by
default.

The reason behind this choice is to ensure that, by default, changes to the
PostgreSQL configuration in a database cluster controlled by CloudNativePG are
allowed only through the Kubernetes API.

At the same time, we are providing an option to enable `ALTER SYSTEM` if you
need to use it, even temporarily, from versions 1.22.0, 1.21.2, and 1.20.5,
by setting `.spec.postgresql.enableAlterSystem` to `true`, as in the following
excerpt:

```yaml
...
  postgresql:
    enableAlterSystem: true
...
```

Clusters in 1.22 will have `enableAlterSystem` set to `false` by default.
If you want to retain the existing behavior, in 1.22, you need to explicitly
set `enableAlterSystem` to `true` as shown above.

In versions 1.21.2 and 1.20.5, and later patch releases in the 1.20 and 1.21
branches, `enableAlterSystem` will be set to `true` by default, keeping with
the existing behavior. If you don't need to use `ALTER SYSTEM`, we recommend
that you set `enableAlterSystem` explicitly to `false`.

!!! Important
    You can set the desired value for  `enableAlterSystem` immediately
    following your upgrade to version 1.22.0, 1.21.2, or 1.20.5, as shown in
    the example above.

### Upgrading to 1.21 from a previous minor version

With the goal to keep improving out-of-the-box the *convention over
configuration* behavior of the operator, CloudNativePG changes the default
value of several knobs in the following areas:

- startup and shutdown control of the PostgreSQL instance
- self-healing
- security
- labels

!!! Warning
    Please read carefully the list of changes below, and how to modify the
    `Cluster` manifests to retain the existing behavior if you don't want to
    disrupt your existing workloads. Alternatively, postpone the upgrade to
    until you are sure. In general, we recommend adopting these default
    values unless you have valid reasons not to.

#### Superuser access disabled

Pushing towards *security-by-default*, CloudNativePG now disables access
`postgres` superuser access via the network in all new clusters, unless
explicitly enabled.

If you want to ensure superuser access to the PostgreSQL cluster, regardless
which version of CloudNativePG you are running, we advise you to explicitly
declare it by setting:

```yaml
spec:
   ...
   enableSuperuserAccess: true
```

#### Replication slots for HA

Replication slots for High Availability are enabled by default.

If you want to ensure replication slots are disabled, regardless of which
version of CloudNativePG you are running, we advise you to explicitly declare
it by setting:

```yaml
spec:
   ...
   replicationSlots:
     highAvailability:
       enabled: false
```

#### Delay for PostgreSQL shutdown

Up to 1.20.2, [the `stopDelay` parameter](instance_manager.md#shutdown-control)
was set to 30 seconds. Despite the recommendations to change and tune this
value, almost all the cases we have examined during support incidents or
community issues show that this value is left unchanged.

The [new default value is 1800 seconds](https://github.com/cloudnative-pg/cloudnative-pg/commit/9f7f18c5b9d9103423a53d180c0e2f2189e71c3c),
the equivalent of 30 minutes.

The new `smartShutdownTimeout` parameter has been introduced to define
the maximum time window within the `stopDelay` value reserved to complete
the `smart` shutdown procedure in PostgreSQL. During this time, the
Postgres server rejects any new connections while waiting for all regular
sessions to terminate.

Once elapsed, the remaining time up to `stopDelay` will be reserved for
PostgreSQL to complete its duties regarding WAL commitments with both the
archive and the streaming replicas to ensure the cluster doesn't lose any data.

If you want to retain the old behavior, you need to set explicitly:

```yaml
spec:
   ...
   stopDelay: 30
```

And, **after** the upgrade has completed, specify `smartShutdownTimeout`:

```yaml
spec:
   ...
   stopDelay: 30
   smartShutdownTimeout: 15
```

#### Delay for PostgreSQL startup

Up to 1.20.2, [the `startDelay` parameter](instance_manager.md#startup-liveness-and-readiness-probes)
was set to 30 seconds, and CloudNativePG used this parameter as
`initialDelaySeconds` for the Kubernetes liveness probe. Given that all the
supported Kubernetes releases provide [startup probes](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/#define-startup-probes),
`startDelay` is now automatically divided into periods of 10 seconds of
duration  each.

!!! Important
    In order to add the `startupProbe`, each pod needs to be restarted.
    As a result, when you upgrade the operator, a one-time rolling
    update of the cluster will be executed even in the online update case.

Despite the recommendations to change and tune this value, almost all the cases
we have examined during support incidents or community issues show that this
value is left unchanged. Given that this parameter influences the startup of
a PostgreSQL instance, a low value of `startDelay` would cause Postgres
never to reach a consistent recovery state and be restarted indefinitely.

For this reason, `startDelay` has been [raised by default to 3600 seconds](https://github.com/cloudnative-pg/cloudnative-pg/commit/4f4cd96bc6f8e284a200705c11a2b41652d58146),
the equivalent of 1 hour.

If you want to retain the existing behavior using the new implementation, you
can do that by explicitly setting:

```yaml
spec:
   ...
   startDelay: 30
```

#### Delay for PostgreSQL switchover

Up to 1.20.2, [the `switchoverDelay` parameter](instance_manager.md#shutdown-of-the-primary-during-a-switchover)
was set by default to 40000000 seconds (over 15 months) to simulate a very long
interval.

The [default value has been lowered to 3600 seconds](https://github.com/cloudnative-pg/cloudnative-pg/commit/9565f9f2ebab8bc648d9c361198479974664c322),
the equivalent of 1 hour.

If you want to retain the old behavior, you need to set explicitly:

```yaml
spec:
   ...
   switchoverDelay: 40000000
```

#### Labels

In version 1.18, we deprecated the `postgresql` label in pods to identify the
name of the cluster, and replaced it with the more canonical `cnpg.io/cluster`
label. The `postgresql` label is no longer maintained.

Similarly, from this version, the `role` label is deprecated. The new label
`cnpg.io/instanceRole` is now used, and will entirely replace the `role` label
in a future release.

#### Shortcut for keeping the existing behavior

If you want to explicitly keep the behavior of CloudNativePG up to version
1.20.2 (we advise not to), you need to set these values in all your `Cluster`
definitions **before upgrading** to a higher version:

```yaml
spec:
   ...
   # Changed in 1.21.0, 1.20.3 and 1.19.5
   startDelay: 30
   stopDelay: 30
   switchoverDelay: 40000000
   # Changed in 1.21.0 only
   enableSuperuserAccess: true
   replicationSlots:
     highAvailability:
       enabled: false
```

Once the upgrade is completed, also add:

```yaml
spec:
   ...
   smartShutdownTimeout: 15
```

### Upgrading to 1.20 from a previous minor version

CloudNativePG 1.20 introduces some changes from previous versions of the
operator in the default behavior of a few features, with the goal to improve
resilience and usability of a Postgres cluster out of the box, through
convention over configuration.

!!! Important
    These changes all involve cases where at least one replica is present, and
    **only affect new `Cluster` resources**.

#### Backup from a standby

[Backup from a standby](backup.md#backup-from-a-standby)
was introduced in CloudNativePG 1.19, but disabled by default - meaning that
the base backup is taken from the primary unless the target is explicitly
set to prefer standby.

From version 1.20, if one or more replicas are available, the operator
will prefer the most aligned standby to take a full base backup.

If you are upgrading your CloudNativePG deployment to 1.20 and are concerned that
this feature might impact your production environment for the new `Cluster` resources
that you create, you can explicitly set the target to the primary by adding the
following line to all your `Cluster` resources:

```yaml
spec:
   ...
   backup:
     target: "primary"
```

#### Restart of a primary after a rolling update

[Automated rolling updates](rolling_update.md#automated-updates-unsupervised)
have been always available in CloudNativePG, and by default they update the
primary after having performed a switchover to the most aligned replica.

From version 1.20, we are changing the default update method
of the primary from switchover to restart as, in most cases, this is
the fastest and safest way.

If you are upgrading your CloudNativePG deployment to 1.20 and are concerned that
this feature might impact your production environment for the new `Cluster`
resources that you create, you can explicitly set the update method of the
primary to switchover by adding the following line to all your `Cluster`
resources:

```yaml
spec:
   ...
   primaryUpdateMethod: switchover
```

#### Replication slots for High Availability

[Replication slots for High Availability](replication.md#replication-slots-for-high-availability)
were introduced in CloudNativePG in version 1.18, but disabled by default.

Version 1.20 prepares the ground for enabling this feature by default in any
future release, as replication slots enhance the resilience and robustness of a
High Availability cluster.

For future compatibility, if you already know that your environments won't ever
need replication slots, our recommendation is that you explicitly disable their
management by adding from now the following lines to your `Cluster` resources:

```yaml
spec:
   ...
   replicationSlots:
     highAvailability:
       enabled: false
```
