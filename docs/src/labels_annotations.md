# Labels and annotations

Resources in Kubernetes are organized in a flat structure, with no hierarchical
information or relationship between them. However, such resources and objects
can be linked together and put in relationship through **labels** and
**annotations**.

!!! info
    For more information, please refer to the Kubernetes documentation on
    [annotations](https://kubernetes.io/docs/concepts/overview/working-with-objects/annotations/) and
    [labels](https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/).

In short:

- an annotation is used to assign additional non-identifying information to
  resources with the goal to facilitate integration with external tools
- a label is used to group objects and query them through Kubernetes' native
  selector capability

You can select one or more labels and/or annotations you will use
in your CloudNativePG deployments. Then you need to configure the operator
so that when you define these labels and/or annotations in a cluster's metadata,
they are automatically inherited by all resources created by it (including pods).

!!! Note
    Label and annotation inheritance is the technique adopted by CloudNativePG
    in lieu of alternative approaches such as pod templates.

## Pre-requisites

By default, no label or annotation defined in the cluster's metadata is
inherited by the associated resources.
In order to enable label/annotation inheritance, you need to follow the
instructions provided in the ["Operator configuration"](operator_conf.md) section.

Below we will continue on that example and limit it to the following:

- annotations: `categories`
- labels: `app`, `environment`, and `workload`

!!! Note
    Feel free to select the names that most suit your context for both
    annotations and labels. Remember that you can also use wildcards
    in naming and adopt strategies like `mycompany/*` for all labels
    or annotations starting with `mycompany/` to be inherited.

## Defining cluster's metadata

When defining the cluster, **before** any resource is deployed, you can
properly set the metadata as follows:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-example
  annotations:
    categories: database
  labels:
    environment: production
    workload: database
    app: sso
spec:
     # ... <snip>
```

Once the cluster is deployed, you can verify, for example, that the labels
have been correctly set in the pods with:

```shell
kubectl get pods --show-labels
```

## Current limitations

Currently, CloudNativePG does not automatically propagate labels or
annotations deletions. Therefore, when an annotation or label is removed from
a Cluster, which was previously propagated to the underlying pods, the operator
will not automatically remove it on the associated resources.
