# Operator configuration

The operator for CloudNativePG is installed from a standard
deployment manifest and follows the convention over configuration paradigm.
While this is fine in most cases, there are some scenarios where you want
to change the default behavior, such as:

- defining annotations and labels to be inherited by all resources created
  by the operator and that are set in the cluster resource
- defining a different default image for PostgreSQL or an additional pull secret

By default, the operator is installed in the `cnpg-system`
namespace as a Kubernetes `Deployment` called `cnpg-controller-manager`.

!!! Note
    In the examples below we assume the default name and namespace for the operator deployment.

The behavior of the operator can be customized through a `ConfigMap`/`Secret` that
is located in the same namespace of the operator deployment and with
`cnpg-controller-manager-config` as the name.

!!! Important
    Any change to the config's `ConfigMap`/`Secret` will not be automatically
    detected by the operator, - and as such, it needs to be reloaded (see below).
    Moreover, changes only apply to the resources created after the configuration
    is reloaded.

!!! Important
    The operator first processes the ConfigMap values and then the Secret’s, in this order.
    As a result, if a parameter is defined in both places, the one in the Secret will be used.

## Available options

The operator looks for the following environment variables to be defined in the `ConfigMap`/`Secret`:

Name | Description
---- | -----------
`INHERITED_ANNOTATIONS` | list of annotation names that, when defined in a `Cluster` metadata, will be inherited by all the generated resources, including pods
`INHERITED_LABELS` | list of label names that, when defined in a `Cluster` metadata, will be inherited by all the generated resources, including pods
`PULL_SECRET_NAME` | name of an additional pull secret to be defined in the operator's namespace and to be used to download images
`ENABLE_AZURE_PVC_UPDATES` | Enables to delete Postgres pod if its PVC is stuck in Resizing condition. This feature is mainly for the Azure environment (default `false`)
`ENABLE_INSTANCE_MANAGER_INPLACE_UPDATES` | when set to `true`, enables in-place updates of the instance manager after an update of the operator, avoiding rolling updates of the cluster (default `false`)
`MONITORING_QUERIES_CONFIGMAP` | The name of a ConfigMap in the operator's namespace with a set of default queries (to be specified under the key `queries`) to be applied to all created Clusters
`MONITORING_QUERIES_SECRET` | The name of a Secret in the operator's namespace with a set of default queries (to be specified under the key `queries`) to be applied to all created Clusters
`CREATE_ANY_SERVICE` | when set to `true`, will create `-any` service for the cluster. Default is `false`

Values in `INHERITED_ANNOTATIONS` and `INHERITED_LABELS` support path-like wildcards. For example, the value `example.com/*` will match
both the value `example.com/one` and `example.com/two`.

When you specify an additional pull secret name using the `PULL_SECRET_NAME` parameter,
the operator will use that secret to create a pull secret for every created PostgreSQL
cluster. That secret will be named `<cluster-name>-pull`.

The namespace where the operator looks for the `PULL_SECRET_NAME` secret is where
you installed the operator. If the operator is not able to find that secret, it
will ignore the configuration parameter.

!!! Warning
    Previous versions of the operator copied the `PULL_SECRET_NAME` secret inside
    the namespaces where you deploy the PostgreSQL clusters. From version "1.11.0"
    the behavior changed to match the previous description. The pull secrets
    created by the previous versions of the operator are unused.

## Defining an operator config map

The example below customizes the behavior of the operator, by defining
the label/annotation names to be inherited by the resources created by
any `Cluster` object that is deployed at a later time, and by enabling
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

The example below customizes the behavior of the operator, by defining
the label/annotation names to be inherited by the resources created by
any `Cluster` object that is deployed at a later time, and by enabling
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

For the change to be effective, you need to recreate the operator pods to
reload the config map. If you have installed the operator on Kubernetes
using the manifest you can do that by issuing:

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
    Customizations will be applied only to `Cluster` resources created
    after the reload of the operator deployment.

Following the above example, if the `Cluster` definition contains a `categories`
annotation and any of the `environment`, `workload`, or `app` labels, these will
be inherited by all the resources generated by the deployment.

## PPROF HTTP SERVER

The operator can expose a PPROF HTTP server with the following endpoints on localhost:6060:

```
- `/debug/pprof/`. Responds to a request for "/debug/pprof/" with an HTML page listing the available profiles
- `/debug/pprof/cmdline`. Responds with the running program's command line, with arguments separated by NUL bytes.
- `/debug/pprof/profile`. Responds with the pprof-formatted cpu profile. Profiling lasts for duration specified in seconds GET parameter, or for 30 seconds if not specified.
- `/debug/pprof/symbol`. Looks up the program counters listed in the request, responding with a table mapping program counters to function names.
- `/debug/pprof/trace`. Responds with the execution trace in binary form.  Tracing lasts for duration specified in seconds GET parameter, or for 1 second if not specified.
```

To enable the operator you need to edit the operator deployment add the flag `--pprof-server=true`.

You can do this by executing these commands:
```
kubectl edit deployment -n cnpg-system cnpg-controller-manager
```

Then on the edit page scroll down the container args and add `--pprof-server=true`, example:
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
Save the changes, the deployment now will execute a rollout and the new pod will have the PPROF server enabled.

Once the pod is running you can exec inside the container by doing:
```
kubectl exec -ti -n cnpg-system <pod name> -- bash
```
Once inside execute:

```
curl localhost:6060/debug/pprof/
```

