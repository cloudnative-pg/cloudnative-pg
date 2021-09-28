# Installation and upgrades

## Installation on Kubernetes

### Directly using the operator manifest

The operator can be installed like any other resource in Kubernetes,
through a YAML manifest applied via `kubectl`.

You can install the [latest operator manifest](https://get.enterprisedb.io/cnp/postgresql-operator-1.9.0.yaml)
as follows:

```sh
kubectl apply -f \
  https://get.enterprisedb.io/cnp/postgresql-operator-1.9.0.yaml
```

Once you have run the `kubectl` command, Cloud Native PostgreSQL will be installed in your Kubernetes cluster.

You can verify that with:

```sh
kubectl get deploy -n postgresql-operator-system postgresql-operator-controller-manager
```

### Using the Operator Lifecycle Manager (OLM)

OperatorHub is a community-sourced index of operators available via the
[Operator Lifecycle Manager](https://github.com/operator-framework/operator-lifecycle-manager),
which is a package managing system for operators.

You can install Cloud Native PostgreSQL using the metadata available in the
[Cloud Native PostgreSQL page](https://operatorhub.io/operator/cloud-native-postgresql)
from the [OperatorHub.io website](https://operatorhub.io), following the installation steps listed on that page.

### Using the Helm Chart

The operator can be installed using the provided [Helm chart](https://github.com/EnterpriseDB/cloud-native-postgresql-helm).

!!! Important
    Helm does not support the update of CRDs. For further information, please refer to the
    [instructions in the Helm chart documentation](https://helm.sh/docs/chart_best_practices/custom_resource_definitions/#some-caveats-and-explanations).

## Installation on Openshift

### Via the web interface

Log in to the console as `kubeadmin` and navigate to the  `Operator → OperatorHub` page.

Find the `Cloud Native PostgreSQL` box scrolling or using the search filter.

Select the operator and click `Install`. Click `Install` again in the following
`Install Operator`, using the default settings. For an in-depth explanation of
those settings, see the [Openshift documentation](https://docs.openshift.com/container-platform/4.6/operators/admin/olm-adding-operators-to-cluster.html#olm-installing-from-operatorhub-using-web-console_olm-adding-operators-to-a-cluster).

The operator will soon be available in all the namespaces.

Depending on the security levels applied to the OpenShift cluster you may be
required to create a proper set of roles and permissions for the operator to
be used in different namespaces.
For more information on this matter see the
[Openshift documentation](https://docs.openshift.com/container-platform/4.6/operators/understanding/olm/olm-understanding-operatorgroups.html).

### Via the `oc` command line

You can add the [`subscription`](samples/subscription.yaml) to install the operator in all the namespaces
as follows:

```sh
oc apply -f \
  https://docs.enterprisedb.io/cloud-native-postgresql/latest/samples/subscription.yaml
```

The operator will soon be available in all the namespaces.

More information on
[how to install operators via CLI](https://docs.openshift.com/container-platform/4.6/operators/admin/olm-adding-operators-to-cluster.html#olm-installing-operator-from-operatorhub-using-cli_olm-adding-operators-to-a-cluster)
is available in the Openshift documentation.

## Details about the deployment

In Kubernetes, the operator is by default installed in the `postgresql-operator-system` namespace as a Kubernetes
`Deployment` called `postgresql-operator-controller-manager`. You can get more information by running:

```sh
kubectl describe deploy \
  -n postgresql-operator-system \
  postgresql-operator-controller-manager
```

As with any Deployment, it sits on top of a ReplicaSet and supports rolling
upgrades. The default configuration of the Cloud Native PostgreSQL operator
comes with a Deployment of a single replica, which is suitable for most
installations. In case the node where the pod is running is not reachable
anymore, the pod will be rescheduled on another node.

If you require high availability at the operator level, it is possible to
specify multiple replicas in the Deployment configuration - given that the
operator supports leader election. Also, you can take advantage of taints and
tolerations to make sure that the operator does not run on the same nodes where
the actual PostgreSQL clusters are running (this might even include the control
plane for self-managed Kubernetes installations).

As far as OpenShift is concerned, details might differ depending on the
selected installation method.

!!! Seealso "Operator configuration"
    You can change the default behavior of the operator by overriding
    some default options. For more information, please refer to the
    ["Operator configuration"](operator_conf.md) section.

## Upgrades

!!! Important
    Please carefully read the [release notes](release_notes.md)
    before performing an upgrade as some versions might require
    extraordinary measures.

Upgrading Cloud Native PostgreSQL operator is a two-step process:

1. upgrade the controller and the related Kubernetes resources
2. upgrade the instance manager running in every PostgreSQL pod

Unless differently stated in the release notes, the first step is normally done
by applying the manifest of the newer version for plain Kubernetes
installations, or using the native package manager of the used distribution
(please follow the instructions in the above sections).

The second step is automatically executed after having updated the controller,
triggering a rolling update of every deployed PostgreSQL instance to use the
new instance manager. If the `primaryUpdateStrategy` is set to `supervised`,
users need to complete the rolling update by manually promoting a new instance
through the `cnp` plugin for `kubectl`.

!!! Seealso "Rolling updates"
    This process is discussed in-depth on the [Rolling Updates](rolling_update.md) page.

!!! Important
    In case `primaryUpdateStrategy` is set to the default value of `unsupervised`,
    an upgrade of the operator will trigger a switchover on your PostgreSQL cluster,
    causing a (normally negligible) downtime.

### Compatibility among versions

We strive to maintain compatibility between different operator versions, but in
some cases, this might not be possible.
Every version of the operator is compatible with the previous one, unless
[release notes](release_notes.md) state the opposite.
The release notes page indeed contains a detailed list of the changes introduced
in every released version of the Cloud Native PostgreSQL Operator, and it must
be read before upgrading to a newer version of the software.

Most versions are directly upgradable and in that case, applying the newer
manifest for plain Kubernetes installations or using the native package
manager of the chosen distribution is enough.

When versions are not directly upgradable, the old version needs to be
removed before installing the new one. This won't affect user data but
only the operator itself. Please consult the release notes for
detailed information on how to upgrade to any released version.

#### Upgrading to version 1.4.0

If you have installed the operator on Kubernetes using the distributed YAML manifest
you must delete the operator controller deployment before installing the
1.4.0 manifest with the following command:

```bash
kubectl delete deployments \
  -n postgresql-operator-system \
  postgresql-operator-controller-manager
```

!!! Important
    Removing the operator controller deployment will not delete or remove any
    of your deployed PostgreSQL clusters.

!!! Warning
    Remember to install the new version of the operator after having performed
    the above command. Otherwise, your PostgreSQL clusters will keep running
    without an operator and, as such, without any self-healing and high-availability
    capabilities.

!!! Note
    In case you deployed the operator in a different namespace than the default
    (`postgresql-operator-system`), you need to use the correct namespace for
    the `-n` option in the above command.
