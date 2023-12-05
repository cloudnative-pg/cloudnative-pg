# Operator configuration

The operator for CloudNativePG is installed from a standard
deployment manifest and follows the convention-over-configuration paradigm.
While this is fine in most cases, in some scenarios you might want
to change the default behavior, such as:

- Defining annotations and labels to be inherited by all resources created
  by the operator and that are set in the cluster resource
- Defining a different default image for PostgreSQL or an additional pull secret

By default, the operator is installed in the `cnpg-system`
namespace as a Kubernetes `Deployment` called `cnpg-controller-manager`.

!!! Note
    The examples that follow assume the default name and namespace for the operator deployment.

You can customize the behavior of the operator through a `ConfigMap`/`Secret` that's
located in the same namespace as the operator deployment and with the name
`cnpg-controller-manager-config`.

!!! Important
    The operator doesn't detect changes to the config's `ConfigMap`/`Secret`.
    As such, you need to reload it. See [Restarting the operator to reload configs](#restarting-the-operator-to-reload-configs).
    The changes apply only to the resources created after you reload the configuration.

!!! Important
    The operator first processes the ConfigMap values and then the secret’s.
    As a result, if a parameter is defined in both places, the one in the secret is used.

## Available options

The operator looks for the following environment variables to be defined in the `ConfigMap`/`Secret`:

Name | Description
---- | -----------
`INHERITED_ANNOTATIONS` | List of annotation names that, when defined in a `Cluster` metadata, are inherited by all the generated resources, including pods.
`INHERITED_LABELS` | List of label names that, when defined in a `Cluster` metadata, are inherited by all the generated resources, including pods.
`PULL_SECRET_NAME` | Name of an additional pull secret to define in the operator's namespace and to use to download images.
`ENABLE_AZURE_PVC_UPDATES` | Enables deletion of Postgres pod if its PVC is stuck in resizing condition. This feature is mainly for the Azure environment (default `false`).
`ENABLE_INSTANCE_MANAGER_INPLACE_UPDATES` | When set to `true`, enables in-place updates of the instance manager after an update of the operator, avoiding rolling updates of the cluster (default `false`).
`MONITORING_QUERIES_CONFIGMAP` | Name of a ConfigMap in the operator's namespace with a set of default queries (to be specified under the key `queries`) to apply to all created clusters.
`MONITORING_QUERIES_SECRET` | The name of a secret in the operator's namespace with a set of default queries (to be specified under the key `queries`) to apply to all created clusters.
`CREATE_ANY_SERVICE` | When set to `true`, creates `-any` service for the cluster (default `false`).

Values in `INHERITED_ANNOTATIONS` and `INHERITED_LABELS` support path-like wildcards. For example, the value `example.com/*` matches
both `example.com/one` and `example.com/two`.

When you specify an additional pull secret name using the `PULL_SECRET_NAME` parameter,
the operator uses that secret to create a pull secret for every created PostgreSQL
cluster. That secret is named `<cluster-name>-pull`.

The namespace where the operator looks for the `PULL_SECRET_NAME` secret is where
you installed the operator. If the operator can't find that secret, it
ignores the configuration parameter.

!!! Warning
    Previous versions of the operator copied the `PULL_SECRET_NAME` secret inside
    the namespaces where you deploy the PostgreSQL clusters. Starting with version 1.11.0,
    the behavior changed to match the previous description. The pull secrets
    created by the previous versions of the operator are unused.

## Defining an operator config map

This example customizes the behavior of the operator. It defines
the label/annotation names to be inherited by the resources created by
any `Cluster` object that's deployed later. It also enables
[in-place updates for the instance
manager](installation_upgrade.md#in-place-updates-of-the-instance-manager).

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: cnpg-controller-manager-config
  namespace: cnpg-system
data:
  INHERITED_ANNOTATIONS: categories
  INHERITED_LABELS: environment, workload, app
  ENABLE_INSTANCE_MANAGER_INPLACE_UPDATES: 'true'
```

## Defining an operator secret

This example customizes the behavior of the operator. It defines
the label/annotation names to be inherited by the resources created by
any `Cluster` object that's deployed later. It also enables
[in-place updates for the instance
manager](installation_upgrade.md#in-place-updates-of-the-instance-manager).

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: cnpg-controller-manager-config
  namespace: cnpg-system
type: Opaque
stringData:
  INHERITED_ANNOTATIONS: categories
  INHERITED_LABELS: environment, workload, app
  ENABLE_INSTANCE_MANAGER_INPLACE_UPDATES: 'true'
```

## Restarting the operator to reload configs

For the change to be effective, you need to re-create the operator pods to
reload the config map. If you installed the operator on Kubernetes
using the manifest, you can do that with this command:

```shell
kubectl rollout restart deployment \
    -n cnpg-system \
    cnpg-controller-manager
```

In general, given a specific namespace, you can delete the operator pods with
the following command:

```shell
kubectl delete pods -n [NAMESPACE_NAME_HERE] \
  -l app.kubernetes.io/name=cloudnative-pg
```

!!! Warning
    Customizations are applied only to `Cluster` resources created
    after reloading the operator deployment.

Following this example, if the `Cluster` definition contains a `categories`
annotation and any of the `environment`, `workload`, or `app` labels, these are
inherited by all the resources generated by the deployment.

## PPROF HTTP server

The operator can expose a PPROF HTTP server with the following endpoints on `localhost:6060`:

- `/debug/pprof/` – Responds to a request for `/debug/pprof/` with an HTML page listing the available profiles.
- `/debug/pprof/cmdline` – Responds with the running program's command line, with arguments separated by NULL bytes.
- `/debug/pprof/profile` – Responds with the PPROF-formatted CPU profile. Profiling lasts for the duration specified in seconds in the `GET` parameter. The default is 30 seconds.
- `/debug/pprof/symbol` – Looks up the program counters listed in the request. Responds with a table that maps program counters to function names.
- `/debug/pprof/trace` – Responds with the execution trace in binary form. Tracing lasts for the duration specified in seconds in the `GET` parameter. The default is 1 second.

To enable the operator, you need to edit the operator deployment. Add the flag `--pprof-server=true`. To do so, execute these commands:

```shell
kubectl edit deployment -n cnpg-system cnpg-controller-manager
```

Then, on the edit page, in the container args, add `--pprof-server=true`:

```yaml
      containers:
      - args:
        - controller
        - --enable-leader-election
        - --config-map-name=cnpg-controller-manager-config
        - --secret-name=cnpg-controller-manager-config
        - --log-level=info
        - --pprof-server=true # relevant line
        command:
        - /manager
```

Save the changes. The deployment executes a rollout, and the new pod has the PPROF server enabled.

Once the pod is running, you can exec inside the container:

```shell
kubectl exec -ti -n cnpg-system <pod name> -- bash
```

Once inside, execute:

```shell
curl localhost:6060/debug/pprof/
```
