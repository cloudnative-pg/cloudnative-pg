# Installation and upgrades

## Installation on Kubernetes

### Directly using the operator manifest

You can install the operator like any other resource in Kubernetes,
through a YAML manifest applied using `kubectl`.

You can install the [latest operator manifest](https://raw.githubusercontent.com/cloudnative-pg/cloudnative-pg/release-1.21/releases/cnpg-1.21.0.yaml)
for this minor release with:

```sh
kubectl apply -f \
  https://raw.githubusercontent.com/cloudnative-pg/cloudnative-pg/release-1.21/releases/cnpg-1.21.0.yaml
```

You can verify that with:

```sh
kubectl get deployment -n cnpg-system cnpg-controller-manager
```

### Using the `cnpg` plugin for `kubectl`

You can use the `cnpg` plugin to override the default configuration options
that are in the static manifests. 

For example, to generate the default latest manifest but change the watch
namespaces to be only a specific namespace, you can run:

```shell
kubectl cnpg install generate \
  --watch-namespaces "specific-namespace" \
  > cnpg_for_specific_namespace.yaml
```

See [`cnpg` plugin](./kubectl-plugin.md#generation-of-installation-manifests)
for a more comprehensive example. 

!!! Warning
    If you're deploying CloudNativePG on GKE and get an error (`... failed to
    call webhook...`), be aware that by default traffic between worker nodes
    and control plane is blocked by the firewall except for a few specific
    ports, as explained in the official
    [docs](https://cloud.google.com/kubernetes-engine/docs/how-to/private-clusters#add_firewall_rules)
    and by this
    [issue](https://github.com/cloudnative-pg/cloudnative-pg/issues/1360).
    You need to either change the `targetPort` in the webhook service to be
    one of the allowed ones or open the webhooks' port (`9443`) on the
    firewall.

### Testing the latest development snapshot

If you want to test or evaluate the latest development snapshot of
CloudNativePG before the next official patch release, you can download the
manifests from the
[`cloudnative-pg/artifacts`](https://github.com/cloudnative-pg/artifacts). This site
provides easy access to the current trunk (main) as well as to each
supported release.

For example, you can install the latest snapshot of the operator with:

```sh
curl -sSfL \
  https://raw.githubusercontent.com/cloudnative-pg/artifacts/main/manifests/operator-manifest.yaml | \
  kubectl apply -f -
```

If you're instead looking for the latest snapshot of the operator for this
specific minor release, you can run:

```sh
curl -sSfL \
  https://raw.githubusercontent.com/cloudnative-pg/artifacts/release-1.21/manifests/operator-manifest.yaml | \
  kubectl apply -f -
```

!!! Important
    Snapshots aren't supported by the CloudNativePG and aren't intended for production use.

### Using the Helm chart

You can install the operator using the provided [Helm chart](https://github.com/cloudnative-pg/charts).

### Using OLM

You can also install CloudNativePG using the
[Operator Lifecycle Manager (OLM)](https://olm.operatorframework.io/docs/)
directly from [OperatorHub.io](https://operatorhub.io/operator/cloudnative-pg).

## Details about the deployment

In Kubernetes, the operator is by default installed in the `cnpg-system`
namespace as a Kubernetes `Deployment`. The name of this deployment
depends on the installation method.
When installed through the manifest or the `cnpg` plugin, the default name is
`cnpg-controller-manager`. When installed using Helm, the default name
is `cnpg-cloudnative-pg`.

!!! Note
    With Helm, you can customize the name of the deployment by way of the
    `fullnameOverride` field in the [`values.yaml` file](https://helm.sh/docs/chart_template_guide/values_files/).

To get more information using the `describe` command in `kubectl`:

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

As with any deployment, it sits on top of a ReplicaSet and supports rolling
upgrades. The default configuration of the CloudNativePG operator
comes with a deployment of a single replica, which is suitable for most
installations. In case the node where the pod is running isn't reachable
anymore, the pod is rescheduled on another node.

If you require high availability at the operator level, you can
specify multiple replicas in the deployment configuration, given that the
operator supports leader election. Also, you can take advantage of taints and
tolerations to make sure that the operator doesn't run on the same nodes where
the actual PostgreSQL clusters are running. (This might even include the control
plane for self-managed Kubernetes installations.)

!!! Seealso "Operator configuration"
    You can change the default behavior of the operator by overriding
    some default options. For more information, see
    ["Operator configuration"](operator_conf.md).

## Upgrades

!!! Important
    Carefully read the [release notes](release_notes.md)
    before performing an upgrade as some versions might require
    extra steps.

!!! Warning
    If you're upgrading to version 1.20, carefully read 
    [Upgrading to 1.20 from a previous minor version](#upgrading-to-120-from-a-previous-minor-version).

Upgrading CloudNativePG operator is a two-step process:

1. Upgrade the controller and the related Kubernetes resources.
2. Upgrade the instance manager running in every PostgreSQL pod.

Unless differently stated in the release notes, the first step is normally done
by applying the manifest of the newer version for plain Kubernetes
installations or using the native package manager of the used distribution.
(Follow the earlier instructions.)

The second step is automatically executed after updating the controller,
by default triggering a rolling update of every deployed PostgreSQL instance to
use the new instance manager. The rolling update procedure culminates with a
switchover, which is controlled by the `primaryUpdateStrategy` option. This option is by
default set to `unsupervised`. When set to `supervised`, you need to complete
the rolling update by manually promoting a new instance using the cnpg
plugin for kubectl.

!!! Seealso "Rolling updates"
    This process is discussed in depth in [Rolling updates](rolling_update.md).

!!! Important
    If `primaryUpdateStrategy` is set to the default value of `unsupervised`,
    an upgrade of the operator triggers a switchover on your PostgreSQL cluster,
    causing a (normally negligible) downtime.

Since version 1.10.0, the rolling update behavior can be replaced with in-place
updates of the instance manager. The latter don't require a restart of the
PostgreSQL instance and, as a result, a switchover in the cluster.
This behavior is disabled by default.

### In-place updates of the instance manager

By default, CloudNativePG issues a rolling update of the cluster
every time the operator is updated. The new instance manager shipped with the
operator is added to each PostgreSQL pod by way of an init container.

However, you can change this behavior by enabling in-place
updates of the instance manager, which is the PID 1 process that keeps the
container alive.

Internally, any instance manager from version 1.10 of CloudNativePG
supports injection of a new executable that replaces the existing one,
once the integrity verification phase is completed, as well as graceful
termination of all the internal processes. When the new instance manager
restarts using the new binary, it adopts the already running postmaster.

As a result, the PostgreSQL process is unaffected by the update, so you don't
need to perform a switchover. However,
the pod is changed after the start, breaking the pure concept of immutability.

You can enable this feature by setting the `ENABLE_INSTANCE_MANAGER_INPLACE_UPDATES`
environment variable to `'true'` in the
[operator configuration](operator_conf.md#available-options).

The in-place upgrade process doesn't change the init container image inside the
pods. Therefore, the pod definition doesn't reflect the current version of the
operator.

!!! Important
    This feature requires that all pods (operators and operands) run on the
    same platform/architecture, for example, all `linux/amd64`.

### Compatibility among versions

CloudNativePG follows semantic versioning. Every release of the
operator in the same API version is compatible with the previous one.
The current API version is v1, corresponding to versions 1.x.y of the operator.

In addition to new features, new versions of the operator contain bug fixes and
stability enhancements. Because of this, **we strongly recommend upgrading
to the latest version of the operator**, as each version is released in order to
maintain the most secure and stable Postgres environment.

CloudNativePG currently releases new versions of the operator at
least monthly. If you can't apply updates as each version becomes
available, we recommend upgrading through each version in sequential order to
become current periodically, without skipping versions.

The [release notes](release_notes.md) contain a detailed list of the
changes introduced in every released version of CloudNativePG.
Read the release notes before upgrading to a newer version of the software.

Most versions are directly upgradable. In that case, applying the newer
manifest for plain Kubernetes installations or using the native package
manager of the chosen distribution is enough.

When versions aren't directly upgradable, you need to remove the old version
before installing the new one. This change doesn't affect user data. It affects
only the operator itself.

### Upgrading to 1.21.0, 1.20.3, or 1.19.5

!!! Important
    We encourage all existing users of CloudNativePG to upgrade to version
    1.21.0 or at least to the latest stable version of the minor release you're
    currently using (namely 1.20.3 or 1.19.5).

!!! Warning
    Every time you're upgrading to a higher minor release, make sure you
    read the release notes and upgrade instructions of all the
    intermediate minor releases. For example, if you want to move
    from 1.19.x to 1.21, make sure you read the release notes
    and upgrade instructions for 1.20 and 1.21.

With the goal to keep improving the out-of-the-box *convention over
configuration* behavior of the operator, CloudNativePG changes the default
value of several knobs in the following areas:

- Startup and shutdown control of the PostgreSQL instance
- Self-healing
- Security
- Labels

Some of these changes were back ported to 1.20.3 and 1.19.5, including 
delays for PostgreSQL shutdown, PostgreSQL startup, and PostgreSQL switchover and labels.
Most of the changes affect only new PostgreSQL clusters.

!!! Warning
    If you don't want to disrupt your existing workloads, carefully read the following 
    list of changes and how to modify the
    `Cluster` manifests to retain the existing behavior. Alternatively, postpone the upgrade
    until you're sure. In general, we recommend adopting these default
    values unless you have valid reasons not to.

#### Superuser access disabled

!!! Important
    This change takes effect starting from CloudNativePG 1.21.0.

Pushing toward *security-by-default*, CloudNativePG now disables
postgres superuser access by way of the network in all new clusters unless
explicitly enabled.

If you want to ensure superuser access to the PostgreSQL cluster, regardless
of the version of CloudNativePG you're running, we recommend that you explicitly
declare it by setting:

```yaml
spec:
   ...
   enableSuperuserAccess: true
```

#### Replication slots for HA

!!! Important
    This change takes effect starting from CloudNativePG 1.21.0.

[As already anticipated in release 1.20](installation_upgrade.md#replication-slots-for-high-availability),
replication slots for high availability are now enabled by default.

If you want to ensure replication slots are disabled, regardless of the
version of CloudNativePG you're running, we recommend that you explicitly declare
it by setting:

```yaml
spec:
   ...
   replicationSlots:
     highAvailability:
       enabled: false
```

#### Delay for PostgreSQL shutdown

!!! Important
    This change was back ported to all supported minor releases. As a
    result, it's available starting from versions 1.21.0, 1.20.3, and
    1.19.5.

Until now, [the `stopDelay` parameter](instance_manager.md#shutdown-control)
was set to 30 seconds. Despite the recommendations to change and tune this
value, almost all the cases we examined during support incidents or
community issues show that this value is left unchanged.

The [new default value is 1800 seconds](https://github.com/cloudnative-pg/cloudnative-pg/commit/9f7f18c5b9d9103423a53d180c0e2f2189e71c3c),
the equivalent of 30 minutes.

The new `smartShutdownTimeout` parameter was introduced to define
the maximum time window in the `stopDelay` value reserved to complete
the `smart` shutdown procedure in PostgreSQL. During this time, the
Postgres server rejects any new connections while waiting for all regular
sessions to terminate.

Once elapsed, the remaining time up to `stopDelay` is reserved for
PostgreSQL to complete its duties regarding WAL commitments with both the
archive and the streaming replicas. This behavior ensures the cluster doesn't lose any data.

If you want to retain the old behavior, you need to set it explicitly:

```yaml
spec:
   ...
   stopDelay: 30
```

After the upgrade has completed, specify `smartShutdownTimeout`:

```yaml
spec:
   ...
   stopDelay: 30
   smartShutdownTimeout: 15
```

#### Delay for PostgreSQL startup

!!! Important
    This change was back ported to all supported minor releases. As a
    result, it's available starting from versions 1.21.0, 1.20.3, and
    1.19.5.

Until now, [the `startDelay` parameter](instance_manager.md#startup-liveness-and-readiness-probes)
was set to 30 seconds, and CloudNativePG used this parameter as
`initialDelaySeconds` for the Kubernetes liveness probe. Given that all the
supported Kubernetes releases provide [startup probes](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/#define-startup-probes),
version 1.21 has adopted this approach as well. (`startDelay` is now
automatically divided into periods of 10 seconds of duration  each.)

!!! Important
    To add the `startupProbe`, each pod needs to be restarted.
    As a result, when you upgrade the operator, a one-time rolling
    update of the cluster is executed, even in the online update case.

Despite the recommendations to change and tune this value, almost all the cases
we examined during support incidents or community issues show that this
value is left unchanged. Given that this parameter influences the startup of
a PostgreSQL instance, a low value of `startDelay` would cause Postgres
never to reach a consistent recovery state and be restarted indefinitely.

For this reason, `startDelay` was [raised by default to 3600 seconds](https://github.com/cloudnative-pg/cloudnative-pg/commit/4f4cd96bc6f8e284a200705c11a2b41652d58146),
the equivalent of 1 hour.

If you want to retain the existing behavior using the new implementation, you
can do that by explicitly setting:

```yaml
spec:
   ...
   startDelay: 30
```

#### Delay for PostgreSQL switchover

!!! Important
    This change was back ported to all supported minor releases. As a
    result, it's available starting from versions 1.21.0, 1.20.3, and
    1.19.5.

Until now, [the `switchoverDelay` parameter](instance_manager.md#shutdown-of-the-primary-during-a-switchover)
was set by default to 40000000 seconds (over 15 months) to simulate a very long
interval.

The [default value was lowered to 3600 seconds](https://github.com/cloudnative-pg/cloudnative-pg/commit/9565f9f2ebab8bc648d9c361198479974664c322),
the equivalent of 1 hour.

If you want to retain the old behavior, you need to set it explicitly:

```yaml
spec:
   ...
   switchoverDelay: 40000000
```

#### Labels

!!! Important
    This change was back ported to all supported minor releases. As a
    result, it's available starting from versions 1.21.0, 1.20.3, and
    1.19.5.

In version 1.18, we deprecated the `postgresql` label in pods to identify the
name of the cluster and replaced it with the more canonical `cnpg.io/cluster`
label. The `postgresql` label is no longer maintained.

Similarly, from this version, the `role` label is deprecated. The new label
`cnpg.io/instanceRole` is now used and will entirely replace the `role` label
in a future release.

#### Shortcut for keeping the existing behavior

If you want to explicitly keep the existing behavior of CloudNativePG
(we recommend that you don't), you need to set these values in all your `Cluster`
definitions before upgrading to version 1.21.0, 1.20.3, or 1.19.5:

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
operator in the default behavior of a few features, with the goal of improving
resilience and usability of a Postgres cluster out of the box, through
convention over configuration.

!!! Important
    These changes all involve cases where at least one replica is present and
    affect only new `Cluster` resources.

#### Backup from a standby

[Backup from a standby](backup.md#backup-from-a-standby)
was introduced in CloudNativePG 1.19 but disabled by default. That is,
the base backup is taken from the primary unless the target is explicitly
set to prefer standby.

From version 1.20, if one or more replicas are available, the operator
prefers the most aligned standby to take a full base backup.

If you're upgrading your CloudNativePG deployment to 1.20 and are concerned that
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
have been always available in CloudNativePG. By default, they update the
primary after having performed a switchover to the most aligned replica.

From version 1.20, the default update method
of the primary from switchover is changing to restart. In most cases, this method is
the fastest and safest.

If you're upgrading your CloudNativePG deployment to 1.20 and are concerned that
this feature might impact your production environment for the new `Cluster`
resources that you create, you can explicitly set the update method of the
primary to switchover by adding the following line to all your `Cluster`
resources:

```yaml
spec:
   ...
   primaryUpdateMethod: switchover
```

#### Replication slots for high availability

[Replication slots for high availability](replication.md#replication-slots-for-high-availability)
were introduced in CloudNativePG in version 1.18. They were disabled by default.

In version 1.20, we're preparing to enable this feature by default from version
1.21, as replication slots enhance the resilience and robustness of a high
availability cluster.

For future compatibility, if you already know that your environments won't ever
need replication slots, we recommend that you explicitly disable their
management starting now by adding the following lines to your `Cluster` resources:

```yaml
spec:
   ...
   replicationSlots:
     highAvailability:
       enabled: false
```
