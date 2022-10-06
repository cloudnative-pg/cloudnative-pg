# Installation and upgrades

## Installation on Kubernetes

### Directly using the operator manifest

The operator can be installed like any other resource in Kubernetes,
through a YAML manifest applied via `kubectl`.

You can install the [latest operator manifest](https://raw.githubusercontent.com/cloudnative-pg/cloudnative-pg/release-1.17/releases/cnpg-1.17.1.yaml)
as follows:

```sh
kubectl apply -f \
  https://raw.githubusercontent.com/cloudnative-pg/cloudnative-pg/release-1.17/releases/cnpg-1.17.1.yaml
```

> **NOTE:** if you want to play with some trial features that haven't yet been release officially, we now have the [cloudnative-pg/artifacts](https://github.com/cloudnative-pg/artifacts)
> provided for persisting the pre-release operator manifests per branches (currently, we support `main` and `release/*`
> branches). You can use the following instruction to install a snapshot operator:
>
> ```sh
> curl -sSfL \
>   https://raw.githubusercontent.com/cloudnative-pg/artifacts/main/manifests/operator-manifest.yaml | \
>   kubectl apply -f -
> ```
>
> Or you can install the operator [e.g. the latest operator manifest on main branch](https://raw.githubusercontent.com/cloudnative-pg/artifacts/main/manifests/operator-manifest.yaml) directly as follows:
> ```sh
> kubectl apply -f \
>    https://raw.githubusercontent.com/cloudnative-pg/artifacts/main/manifests/operator-manifest.yaml
> ```
>
> Also, you can find out which exact commit in [cloudnative-pg/cloudnative-pg](https://github.com/cloudnative-pg/cloudnative-pg) that produces the corresponding operator manifest in the commit message of the manifest itself.

Once you have run the `kubectl` command, CloudNativePG will be installed in your Kubernetes cluster.

You can verify that with:

```sh
kubectl get deploy -n cnpg-system cnpg-controller-manager
```

### Using the Helm Chart

The operator can be installed using the provided [Helm chart](https://github.com/cloudnative-pg/charts).


## Details about the deployment

In Kubernetes, the operator is by default installed in the `cnpg-system` namespace as a Kubernetes
`Deployment` called `cnpg-controller-manager`. You can get more information by running:

```sh
kubectl describe deploy \
  -n cnpg-system \
  cnpg-controller-manager
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

!!! Important
    In 2022, EDB plans an LTS release for CloudNativePG in
    environments where frequent online updates are not possible.

The [release notes](release_notes.md) page contains a detailed list of the
changes introduced in every released version of CloudNativePG,
and it must be read before upgrading to a newer version of the software.

Most versions are directly upgradable and in that case, applying the newer
manifest for plain Kubernetes installations or using the native package
manager of the chosen distribution is enough.

When versions are not directly upgradable, the old version needs to be
removed before installing the new one. This won't affect user data but
only the operator itself.

