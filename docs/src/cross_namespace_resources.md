---
id: cross_namespace_resources
sidebar_position: 245
title: Cross-namespace resources
---

# Cross-namespace resources
<!-- SPDX-License-Identifier: CC-BY-4.0 -->

By default, the [`Database`](declarative_database_management.md),
[`DatabaseRole`](declarative_role_management.md),
[`Publication` and `Subscription`](logical_replication.md) resources must live
in the same namespace as the `Cluster` they reference.

**Cross-namespace resources** lift this restriction for a `Cluster` that opts
in: those four kinds of objects can then be created in any namespace, and
reference the cluster through `spec.cluster.namespace`. This is useful when a
platform team runs a shared PostgreSQL cluster in its own namespace, and
application teams declare their databases, roles, publications and
subscriptions next to their applications, in their own namespaces, without
being given access to the namespace of the cluster.

:::warning
Enabling cross-namespace resources lets **every namespace** of the Kubernetes
cluster target the PostgreSQL cluster, and it cannot be disabled afterwards.
Read the [security model](#security-model) before enabling it, and restrict
who can create these objects as described in
[Restricting which namespaces can target a cluster](#restricting-which-namespaces-can-target-a-cluster).
:::

## Enabling cross-namespace resources

A `Cluster` opts in by setting `spec.enableCrossNamespaceResources` to `true`:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: shared-pg
  namespace: databases
spec:
  instances: 3
  enableCrossNamespaceResources: true
  storage:
    size: 10Gi
```

The setting can be added when the cluster is created or later, on a running
cluster.

:::info[Important]
`enableCrossNamespaceResources` **cannot be disabled once enabled**: the
validating webhook rejects any change setting it back to `false`. Turning the
feature off would orphan the objects already created in other namespaces, whose
PostgreSQL counterparts and finalizers would no longer be managed. To stop
using the feature, delete the cross-namespace objects first, and then recreate
the cluster if you need to remove the permissions granted to it.
:::

### Enabling the feature on a running cluster

The instance manager of each Pod watches the `Database`, `DatabaseRole`,
`Publication` and `Subscription` objects only in the namespace of its cluster,
unless the cluster opted in, in which case it watches them in every namespace.
This scope is decided when the instance manager starts.

When `enableCrossNamespaceResources` is set on a running cluster, each instance
manager waits until the operator has granted it access to every namespace
(it checks that through a `SelfSubjectAccessReview`), and then **restarts
itself** to widen its watch. **PostgreSQL keeps running** across this restart:
the Pod is not recreated, and no switchover or connection drop is involved.
Cross-namespace objects created before the restart are reconciled as soon as
the new instance manager is up.

The same restart happens if an instance manager cannot read its cluster when
it starts, for example during a brief unavailability of the API server, or if
it starts before the operator granted it access to every namespace: it then
watches the namespace of its cluster only, and widens its watch once the
cluster can be read and the access is granted, instead of failing to start.

## Referencing a cluster in another namespace

Each of the four resources references its cluster through `spec.cluster`,
which accepts an optional `namespace` field next to `name`:

- When `namespace` is omitted, or equal to the namespace of the object, the
  cluster lives in the same namespace, as usual.
- Otherwise, the object is a cross-namespace object, and the referenced
  cluster must have `enableCrossNamespaceResources: true`.

Like the rest of `spec.cluster`, the namespace is **immutable** after creation.
The reference is compared as written: adding `namespace` to an existing object,
even set to the namespace of the object itself, is rejected as a change. Keep
writing the reference of an existing object the way it was created.

The examples below create objects in a `team-a` namespace, referencing the
`shared-pg` cluster in the `databases` namespace.

### Database

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Database
metadata:
  name: shared-pg-orders
  namespace: team-a
spec:
  cluster:
    name: shared-pg
    namespace: databases
  name: orders
  owner: orders_owner
```

The `Database` validating webhook rejects a cross-namespace `Database` whose
cluster does not exist or has not opted in:

```
spec.cluster.namespace: Forbidden: cluster "shared-pg" does not have enableCrossNamespaceResources set to true
```

The other three kinds have no such webhook check: a `DatabaseRole`,
`Publication` or `Subscription` referencing a cluster that has not opted in is
accepted by the API server but never reconciled, because the instance manager
of that cluster does not watch other namespaces. The operator does not
generate the password of such a `DatabaseRole` nor issue its client
certificate either, and records why in `status.password.message` and
`status.clientCertificate.message`.

### DatabaseRole

A cross-namespace `DatabaseRole` keeps its password Secret in **its own
namespace**, not in the namespace of the cluster. This holds both for a Secret
you supply with [`mode: secret`](declarative_role_management.md#mode-secret)
and for the one the operator creates with
[`mode: generate`](declarative_role_management.md#mode-generate), as well as
for the [client certificate Secret](declarative_role_management.md#generated-certificate-secret),
so that the team owning the role is also the one able to read its credentials.

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: orders-owner-password
  namespace: team-a
  labels:
    cnpg.io/reload: "true"
type: kubernetes.io/basic-auth
stringData:
  username: orders_owner
  password: change-me
---
apiVersion: postgresql.cnpg.io/v1
kind: DatabaseRole
metadata:
  name: shared-pg-orders-owner
  namespace: team-a
spec:
  cluster:
    name: shared-pg
    namespace: databases
  name: orders_owner
  login: true
  password:
    mode: secret
    secret: orders-owner-password
```

The instance manager reads that Secret through a narrowly scoped `Role`
created by the operator in the `team-a` namespace (see
[Security model](#security-model)).

### Publication

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Publication
metadata:
  name: shared-pg-orders-publication
  namespace: team-a
spec:
  cluster:
    name: shared-pg
    namespace: databases
  dbname: orders
  name: orders_pub
  target:
    allTables: true
```

### Subscription

The `externalClusterName` of a `Subscription` refers to an entry of the
`externalClusters` section of the **referenced cluster**, which is defined by
whoever manages the `Cluster`, not by the namespace holding the
`Subscription`.

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Subscription
metadata:
  name: shared-pg-orders-subscription
  namespace: team-a
spec:
  cluster:
    name: shared-pg
    namespace: databases
  dbname: orders
  name: orders_sub
  publicationName: orders_pub
  externalClusterName: orders-source
```

### Listing cross-namespace objects

`kubectl get` shows the namespace of the referenced cluster in the
`Cluster Namespace` column, which is empty when `spec.cluster.namespace` is
omitted. To see every object targeting a cluster, list them across
all namespaces:

```sh
kubectl get databases,databaseroles,publications,subscriptions -A
```

## Exclusivity and conflicts

A given PostgreSQL object of a cluster (a database, a role, a publication or a
subscription, identified by its `spec.name`) can be managed by **only one**
Kubernetes object, across **all** namespaces. The object that reconciled it
first keeps managing it. Any other object claiming the same PostgreSQL object
of the same cluster, in the same namespace or in another one, is marked as
failed and **never touches PostgreSQL**:

```yaml
status:
  applied: false
  message: '"orders" is already managed by object "team-a/shared-pg-orders"'
```

The namespace of the managing object is shown when it differs from the one of
the rejected object. The rejected object is retried periodically, and takes
over once the managing object is gone.

The operator does not issue a
[client certificate](declarative_role_management.md#generated-certificate-secret)
for a rejected `DatabaseRole` either: the certificate would let the namespace
of the rejected object log in as a role managed by another object. A
certificate issued before the object was rejected, as when two objects claim
the same role before either is reconciled, is deleted. The reason is recorded
in `status.clientCertificate.message`.

Deleting a rejected object never changes PostgreSQL, **even when its reclaim
policy is `delete`**: since it never reconciled, it does not own the
PostgreSQL object, and dropping it would destroy what the managing object
declares. Only the object that actually manages the database, role,
publication or subscription can drop it when deleted with a `delete` reclaim
policy (`databaseReclaimPolicy`, `databaseRoleReclaimPolicy`,
`publicationReclaimPolicy` or `subscriptionReclaimPolicy`).

:::warning
"First" is decided by reconciliation, not by namespace ownership. When several
teams can target the same cluster, a team can claim a database or role name
before the team that expected to use it. Agree on a naming convention (for
example, prefixing PostgreSQL names with the namespace) and enforce it, for
instance with an admission policy.
:::

## Security model

### Objects created for the cluster

When `enableCrossNamespaceResources` is `true`, the operator creates the
following objects. Their name is
`cnpg-cross-ns-<hash>`, where `<hash>` is the first 16 hexadecimal digits of the
SHA-256 of `<cluster namespace>/<cluster name>`, so that clusters with the same
name in different namespaces never collide.

A **`ClusterRole`** and a **`ClusterRoleBinding`**, binding the `ClusterRole`
to the ServiceAccount the instances run with: the one named after the cluster,
or the one set in `serviceAccountName`. The `ClusterRole` grants, in **all
namespaces**:

| API group | Resources | Verbs |
|---|---|---|
| `postgresql.cnpg.io` | `databases`, `databaseroles`, `publications`, `subscriptions` | `get`, `list`, `watch`, `update` |
| `postgresql.cnpg.io` | `databases/status`, `databaseroles/status`, `publications/status`, `subscriptions/status` | `get`, `patch`, `update` |

Kubernetes RBAC cannot filter objects by the cluster they reference, so this
access covers every object of those four kinds in the Kubernetes cluster, not
only the ones referencing this cluster. The instance manager itself only acts
on the objects referencing its own cluster.

A **`Role`** and a **`RoleBinding`** in each namespace, other than the one of
the cluster, holding at least one cross-namespace `DatabaseRole` with a
password Secret. The `Role` grants `get` and `watch` on `secrets`, limited
through `resourceNames` to the password Secrets of the `DatabaseRole` objects
of that namespace targeting this cluster. It is updated when those roles
change, and removed when the namespace no longer holds any.

:::info
The Secret permissions use a `Role` per namespace, and not the `ClusterRole`,
on purpose. `resourceNames` in a `ClusterRole` match Secrets by name in
**every** namespace, so granting `orders-owner-password` that way would also
expose any Secret with the same name elsewhere. A rule without `resourceNames`
would expose every Secret. A `Role` confines the access to the Secrets
actually referenced, in the namespace that references them. No `Role` is
created in a namespace without Secrets to read, since an empty
`resourceNames` list would grant access to all of its Secrets.
:::

All these objects carry the labels `cnpg.io/cluster`,
`cnpg.io/clusterNamespace` and `app.kubernetes.io/managed-by: cloudnative-pg`.
They cannot carry an owner reference to the `Cluster`, which lives in another
namespace (or, for cluster-scoped objects, in a namespace at all), so the
operator relies on these labels:

- These objects are reconciled apart from the rest of the cluster, so a
  failure, for example in a tenant namespace being deleted, never stops the
  management of the instances nor changes the phase of the cluster. The
  failure is recorded as a `CrossNamespaceRBACFailed` warning event on the
  cluster and retried, while the other namespaces are still reconciled.
  Changes made to these objects by someone else are reverted.
- An existing object with the expected name but without the matching labels is
  **never modified or deleted**. For the `ClusterRole` and
  `ClusterRoleBinding`, this is reported as a `CrossNamespaceRBACFailed`
  event. For a `Role` or `RoleBinding` in a tenant namespace, the operator
  records a `CrossNamespaceRBACNotOwned` warning event on the cluster and leaves
  it alone, so that a tenant cannot block the cluster by creating it: the
  password Secrets of that namespace are then not readable by the cluster, and
  no `RoleBinding` to a foreign `Role` is created.
- When the cluster is deleted, the operator deletes all of them, even when
  some of the objects depending on the cluster cannot be updated.
- A sweep, run when the operator starts and then every 15 minutes, deletes the
  objects left behind by clusters that were deleted while the operator was not
  running, including the `Role` and `RoleBinding` objects of the tenant
  namespaces. It also deletes the ones of a cluster that does not enable the
  feature, such as a new cluster created with the name of a deleted one, which
  must not inherit its access.
- The operator only caches the `ClusterRole` and `ClusterRoleBinding` objects
  carrying these labels, not every one of the Kubernetes cluster.

### Additional permissions of the operator

To manage these objects, the operator's own `ClusterRole` includes `create`,
`delete`, `get`, `list`, `patch`, `update` and `watch` on `clusterroles`,
`clusterrolebindings`, `roles` and `rolebindings` of the
`rbac.authorization.k8s.io` group. Kubernetes only lets the operator grant
permissions it holds itself, so the operator can never create a `ClusterRole`
broader than its own permissions on CloudNativePG resources. These permissions
are part of the operator installation, whether or not any cluster enables
cross-namespace resources.

An operator whose RBAC lacks the `list` and `watch` permissions on
`clusterroles` and `clusterrolebindings`, as one installed from manifests or a
Helm chart predating cross-namespace resources, still starts, and logs a
warning. The clusters enabling cross-namespace resources then get a
`CrossNamespaceRBACForbidden` warning event, and their instances keep watching
the namespace of their cluster only. Restart the operator once its RBAC is
updated.

See [Role Based Access Control (RBAC)](security.md#role-based-access-control-rbac)
for the other permissions of the operator.

### Who can target the cluster

Once the feature is enabled, **anyone able to create a `Database`,
`DatabaseRole`, `Publication` or `Subscription` in any namespace** can target
the cluster. As explained in
[RBAC on Custom Resources](security.md#rbac-on-custom-resources), write access
to `DatabaseRole` is equivalent to superuser access on the targeted cluster,
since a `DatabaseRole` can set `superuser: true` or join the `postgres` role.
Enabling cross-namespace resources therefore extends that superuser-equivalent
access to every user who can write `DatabaseRole` objects **anywhere** in the
Kubernetes cluster.

Conversely, the ServiceAccount of the cluster can read and update every
`Database`, `DatabaseRole`, `Publication` and `Subscription` object in the
Kubernetes cluster. Anything running with that ServiceAccount, including code
run by a PostgreSQL superuser inside the instance Pods, could alter those
objects, including ones that reference other clusters. Treat the Pods of a
cluster with `enableCrossNamespaceResources: true` as being as privileged as a
user allowed to write those four kinds in every namespace.

:::note
If several clusters share a ServiceAccount through
[`serviceAccountName`](security.md#using-a-shared-serviceaccount), they all
accumulate the cross-namespace permissions of any of them that opts in.
:::

### Restricting which namespaces can target a cluster

CloudNativePG does not offer a list of allowed namespaces: use the standard
Kubernetes mechanisms to decide who can create these objects, and where they
may point.

**Kubernetes RBAC.** Only grant `create` and `update` on `databases`,
`databaseroles`, `publications` and `subscriptions` to the users and
namespaces that should use the shared cluster. Review which roles already
include them: for example, the Helm chart's `rbac.aggregateClusterRoles`
value gives everyone bound to the built-in `edit` or `admin` `ClusterRole`
write access to CloudNativePG resources (see [RBAC for Users](rbac.md)).

**Admission policies.** A
[`ValidatingAdmissionPolicy`](https://kubernetes.io/docs/reference/access-authn-authz/validating-admission-policy/),
or an equivalent policy engine, can restrict which namespaces may reference a
cluster in another namespace. The following policy only admits a
cross-namespace object when its namespace carries a label, set by the platform
team, naming the namespace of the cluster it may target:

```yaml
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingAdmissionPolicy
metadata:
  name: cnpg-cross-namespace-targets
spec:
  failurePolicy: Fail
  matchConstraints:
    resourceRules:
      - apiGroups: ["postgresql.cnpg.io"]
        apiVersions: ["v1"]
        operations: ["CREATE", "UPDATE"]
        resources: ["databases", "databaseroles", "publications", "subscriptions"]
  validations:
    - expression: >-
        !has(object.spec.cluster.namespace) ||
        object.spec.cluster.namespace == "" ||
        object.spec.cluster.namespace == object.metadata.namespace ||
        (has(namespaceObject.metadata.labels) &&
         "example.com/postgres-cluster-namespace" in namespaceObject.metadata.labels &&
         namespaceObject.metadata.labels["example.com/postgres-cluster-namespace"] ==
           object.spec.cluster.namespace)
      message: "this namespace is not allowed to reference a cluster in another namespace"
---
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingAdmissionPolicyBinding
metadata:
  name: cnpg-cross-namespace-targets
spec:
  policyName: cnpg-cross-namespace-targets
  validationActions: ["Deny"]
```

With this policy, labelling the `team-a` namespace with
`example.com/postgres-cluster-namespace: databases` allows it, and only it, to
target the clusters of the `databases` namespace. Make sure tenants cannot
label their own namespaces, which is usually the case since `Namespace` objects
are cluster-scoped. The same mechanism can enforce a naming convention on
`spec.name`, to avoid the [conflicts](#exclusivity-and-conflicts) between
teams described above.

## Limitations

- `enableCrossNamespaceResources` cannot be disabled once enabled. Removing the
  cross-namespace permissions of a cluster requires recreating it.
- Only the `Database` resource is checked by the webhook against the opt-in of
  the cluster. Cross-namespace `DatabaseRole`, `Publication` and
  `Subscription` objects targeting a cluster that has not opted in are accepted
  and are never reconciled. Only a `DatabaseRole` gets a status message, about
  its password and client certificate, which the operator does not generate.
- `spec.cluster` is compared as written: setting `namespace` on an existing
  object, even to its own namespace, is rejected.
- A cross-namespace `Database` can only be created once its cluster exists,
  since the webhook needs to read it.
- The operator must be able to see the namespaces holding the cross-namespace
  objects: when it only watches some namespaces, objects in the other
  namespaces are not handled, for example their generated passwords and the
  Secret permissions of their `DatabaseRole` objects. The operator also needs
  permission to create `ClusterRole` and `ClusterRoleBinding` objects.
- A cross-namespace `DatabaseRole` can only read password Secrets from its own
  namespace.
- The `externalClusters` used by a cross-namespace `Subscription` are defined
  in the referenced `Cluster`, and are not under the control of the namespace
  holding the `Subscription`.
- All the [limitations](declarative_database_management.md#limitations-and-caveats)
  of the individual resources still apply, including the behavior on
  [replica clusters](replica_cluster.md).
