# Installation and upgrades

## Installation on Kubernetes

### Directly using the operator manifest

The operator can be installed like any other resource in Kubernetes,
through a YAML manifest applied via `kubectl`.

You can install the [latest operator manifest](https://raw.githubusercontent.com/cloudnative-pg/cloudnative-pg/release-1.19/releases/cnpg-1.19.5.yaml)
for this minor release as follows:

```sh
kubectl apply -f \
  https://raw.githubusercontent.com/cloudnative-pg/cloudnative-pg/release-1.19/releases/cnpg-1.19.5.yaml
```

You can verify that with:

```sh
kubectl get deploy -n cnpg-system cnpg-controller-manager
```

### Using the `cnpg` plugin for `kubectl`

You can use the `cnpg` plugin to override the default configuration options
that are in the static manifests. 

For example, to generate the default latest manifest but change the watch
namespaces to only be a specific namespace, you could run:

```shell
kubectl cnpg install generate \
  --watch-namespaces "specific-namespace" \
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
  https://raw.githubusercontent.com/cloudnative-pg/artifacts/release-1.19/manifests/operator-manifest.yaml | \
  kubectl apply -f -
```

If you are instead looking for the latest snapshot of the operator for this
specific minor release, you can just run:

```sh
curl -sSfL \
  https://raw.githubusercontent.com/cloudnative-pg/artifacts/release-1.19/manifests/operator-manifest.yaml | \
  kubectl apply -f -
```

!!! Important
    Snapshots are not supported by the CloudNativePG and not intended for production usage.

### Using the Helm Chart

The operator can be installed using the provided [Helm chart](https://github.com/cloudnative-pg/charts).

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

!!! Important
    This feature requires that all pods (operators and operands) run on the
    same platform/architecture (for example, all `linux/amd64`).

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

### Upgrading to 1.19.5

!!! Important
    We encourage all existing users of CloudNativePG 1.19.x to upgrade at your
    earliest possible convenience to 1.19.5, the latest stable version for this
    minor release.

With the goal to keep improving out-of-the-box the *convention over
configuration* behavior of the operator, CloudNativePG changes the default
value of several knobs in the following areas:

- startup and shutdown control of the PostgreSQL instance
- self-healing
- labels

Most of the changes will affect new PostgreSQL clusters only.

!!! Warning
    Please read carefully the list of changes below, and how to modify the
    `Cluster` manifests to retain the existing behavior if you don't want to
    disrupt your existing workloads. Alternatively, postpone the upgrade to
    until you are sure. In general, we recommend adopting these default
    values unless you have valid reasons not to.

#### Delay for PostgreSQL shutdown

Up to now, [the `stopDelay` parameter](instance_manager.md#shutdown-control)
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

Until now, [the `startDelay` parameter](instance_manager.md#startup-liveness-and-readiness-probes)
was set to 30 seconds, and CloudNativePG used this parameter as
`initialDelaySeconds` for the Kubernetes liveness probe. Given that all the
supported Kubernetes releases provide [startup probes](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/#define-startup-probes),
from this version we have adopted this approach as well (`startDelay` is now
automatically divided into periods of 10 seconds of duration  each).

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

Up to now, [the `switchoverDelay` parameter](instance_manager.md#shutdown-of-the-primary-during-a-switchover)
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

If you want to explicitly keep the existing behavior of CloudNativePG
(we advise not to), you need to set these values in all your `Cluster`
definitions **before upgrading** to version 1.19.5:

```yaml
spec:
   ...
   startDelay: 30
   stopDelay: 30
   switchoverDelay: 40000000
```

Once the upgrade is completed, also add:

```yaml
spec:
   ...
   smartShutdownTimeout: 15
```
