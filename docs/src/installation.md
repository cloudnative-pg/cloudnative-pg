# Installation

## Installation on Kubernetes

The operator can be installed like any other resource in Kubernetes,
through a YAML manifest applied via `kubectl`.

You can install the [latest operator manifest](samples/postgresql-operator-0.7.0.yaml)
as follows:

```sh
kubectl apply -f \
  https://docs.enterprisedb.io/cloud-native-postgresql/latest/samples/postgresql-operator-0.7.0.yaml
```

Once you have run the `kubectl` command, Cloud Native PostgreSQL will be installed in your Kubernetes cluster.

You can verify that with:

```sh
kubectl get deploy -n postgresql-operator-system postgresql-operator-controller-manager
```

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
kubectl describe deploy -n postgresql-operator-system postgresql-operator-controller-manager
```

As any deployment, it sits on top of a replica set and supports rolling upgrades.
By default, we currently support only 1 replica. In future versions we plan to
support multiple replicas and leader election, as well as taints and tolerations
so to enable deployment on the Kubernetes control plane.

In case the node where the pod is running is not reachable anymore,
the pod will be rescheduled on another node.

As far as OpenShift is concerned, details might differ depending on the
selected installation method.
