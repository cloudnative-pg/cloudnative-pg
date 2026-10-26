---
id: rbac
sidebar_position: 105
title: RBAC for Users
---

# RBAC for Users
<!-- SPDX-License-Identifier: CC-BY-4.0 -->

Human Database Administrators (DBAs) who inspect Clusters and connect to
PostgreSQL do not need to administer the Kubernetes platform. Granting them
`cluster-admin` gives unrestricted access to every API group in every
namespace, which is far more than the job requires. Instead, grant a narrowly
scoped, namespaced set of permissions, distinct from both platform
administrators and the operator's own service account (described in the
[Security](security.md#role-based-access-control-rbac) page).

This page describes a read-only persona. Granting write access to
CloudNativePG resources is a high-privilege decision, covered in
[RBAC on Custom Resources](security.md#rbac-on-custom-resources).

## Available roles

The operator manifest installs `cnpg-viewer-role`, a `ClusterRole` that grants
read-only access to every resource in the `postgresql.cnpg.io`,
`barmancloud.cnpg.io` and `klio.cnpg.io` API groups, including those added in
future releases. The Barman Cloud and Klio plugins are included because they
are CloudNativePG projects, maintained alongside the operator. Rules for a group
that is not installed have no effect. The role does not cover third-party
plugins, and it covers only custom resources, so a DBA needs more than this role
to diagnose a Cluster.

:::warning
    `cnpg-viewer-role` comes with the operator manifest, not with the Helm
    chart. If you install with Helm, the role does not exist in your cluster:
    the chart ships its own view ClusterRole instead (for example,
    `cnpg-cloudnative-pg-view` for a release named `cnpg`), which covers only
    the `postgresql.cnpg.io` group, not the Barman Cloud and Klio plugins. You
    can aggregate it into `view`, `edit` and `admin` with the
    `rbac.aggregateClusterRoles` value, which also aggregates the chart's edit
    role into `edit` and `admin`, giving them write access to CloudNativePG
    resources. To get `cnpg-viewer-role` with Helm, apply it from the operator
    manifest. The samples below do not depend on it.
:::

CloudNativePG also provides sample manifests that complete the persona. They
are split into separate roles so that each sensitive permission can be granted
(and revoked) on its own. Each sample contains an example `RoleBinding`.

| Sample | Role | Grants |
| ------ | ---- | ------ |
| [`rbac-dba-viewer.yaml`](samples/rbac-dba-viewer.yaml) | `cnpg-dba-viewer` | Self-contained read-only persona: CloudNativePG, Barman Cloud and Klio resources, plus the Kubernetes objects needed to diagnose a Cluster (Pods and their logs, events, Services, ConfigMaps, PersistentVolumeClaims, Jobs, PodDisruptionBudgets, VolumeSnapshots, StatefulSets and leases). It also grants `get` on `pods/proxy`, which `kubectl cnpg status` uses. |
| [`rbac-dba-portforward.yaml`](samples/rbac-dba-portforward.yaml) | `cnpg-dba-portforward` | Optional. `kubectl port-forward`, to open a local tunnel to PostgreSQL. |
| [`rbac-dba-app-secret.yaml`](samples/rbac-dba-app-secret.yaml) | `cnpg-dba-app-secret` | Optional. `get` on one named application credential Secret. |

Instead of `cnpg-dba-viewer`, you can combine `cnpg-viewer-role` with the
Kubernetes built-in `view` role, which already covers most of the objects
above (and excludes Secrets), and add only what is missing.

Bind the `ClusterRole` objects with a `RoleBinding` (not a
`ClusterRoleBinding`) to limit the access to a single namespace. A Klio
`Server` is not necessarily in the same namespace as the `Cluster` that uses
it: if yours is not, bind `cnpg-dba-viewer` in the namespace of the `Server`
too.

:::warning
    Kubernetes RBAC cannot select Pods by label, so these roles apply to every
    Pod in the namespace where they are bound, not only to PostgreSQL Pods. In
    particular, `get` on `pods/proxy` in `cnpg-dba-viewer` allows HTTP `GET`
    requests to any port of any Pod in that namespace, and
    `cnpg-dba-portforward` allows a tunnel to any Pod. Bind the roles only in
    namespaces that hold databases, and avoid sharing those namespaces with
    unrelated workloads.
:::

`cnpg-dba-viewer` deliberately does not grant:

- any write verb, on any resource
- any access to `secrets`
- `pods/exec` and `pods/attach`: Kubernetes RBAC handles subresources
  independently, so `pods/portforward` can be granted without giving a shell
  inside the database containers
- cluster-scoped resources such as `nodes`, which require a `ClusterRoleBinding`

:::warning
    Do not grant `list` or `watch` on `secrets`: both return the content of
    every Secret in the namespace, including the superuser, CA and replication
    credentials. The `cnpg-dba-app-secret` sample grants only `get` on a named
    Secret. As the names are fixed, it must be maintained whenever Clusters are
    created or deleted. If you automate the Cluster lifecycle, consider
    granting no Secret access at all.
:::

## What works with `cnpg-dba-viewer`

The table below is derived from the Kubernetes API calls made by each
`kubectl cnpg` command:

| Command | `cnpg-dba-viewer` | Notes |
| ------- | :---------------: | ----- |
| `kubectl cnpg status` | Yes, with gaps | Needs `get` on `pods/proxy`. Two parts of the output run `kubectl exec`, so they need `pods/exec`: the database `Size` line, and, with `-v -v`, the PostgreSQL configuration and HBA rules. Without it, those parts report that the permission is missing, and the rest of the output is unaffected. |
| `kubectl cnpg logs cluster` | Yes | |
| `kubectl cnpg report cluster` | Yes | Includes events, Pods, Jobs and PVCs. |
| `kubectl cnpg psql` | No | It runs `kubectl exec`, which needs `create` on `pods/exec`. |
| `kubectl cnpg backup` | No | It creates a `Backup` resource, which is a write operation. |
| `kubectl cnpg report operator` | No | It first lists the operator `Deployment` in the operator namespace, and fails without `list` on `deployments` there, which this role does not grant. With that permission, it collects the rest (Pods, ConfigMaps, events, webhook configurations) on a best-effort basis, skipping what is not allowed. |
| `kubectl exec` (Pod shell) | No | Needs `create` on `pods/exec`. |
| `kubectl port-forward` | With `cnpg-dba-portforward` | Then connect with any PostgreSQL client. |

Commands that change the state of a Cluster (`restart`, `promote`, `fence`,
`hibernate`, `maintenance`, and so on) are out of scope for a read-only
persona.

## Trying a role with impersonation

You can check what a role grants without creating a real user, by running
`kubectl` as a user and group through Kubernetes
[impersonation](https://kubernetes.io/docs/reference/access-authn-authz/user-impersonation/).
This requires an account allowed to impersonate, such as a cluster administrator.

Bind the role to a group in the namespace of your `Cluster`, as in the example
`RoleBinding` of the sample, and then pass the user and the group to `kubectl`
(`jane` and `dbas` are placeholders):

```sh
kubectl apply -f rbac-dba-viewer.yaml
kubectl --as=jane --as-group=dbas -n default get clusters
kubectl --as=jane --as-group=dbas -n default get pods
```

The plugin accepts the same flags, but `kubectl` requires them after the plugin
name:

```sh
kubectl cnpg status cluster-example --as=jane --as-group=dbas
kubectl cnpg logs cluster cluster-example --as=jane --as-group=dbas
```

:::warning
    Two plugin commands do not pass these flags on. `kubectl cnpg psql` runs
    `kubectl exec` without them, and `kubectl cnpg report` collects logs (with
    `--logs`) without them. Both run with your own identity, so they succeed
    even when the role does not allow them: do not use them to try a role.
:::

A request that the role does not allow fails with a `Forbidden` error, for
example:

```sh
kubectl --as=jane --as-group=dbas -n default delete cluster cluster-example
kubectl --as=jane --as-group=dbas -n default get secrets
```

To ask the API server directly, without running the command, use
`kubectl auth can-i`:

```sh
kubectl auth can-i get clusters.postgresql.cnpg.io -n default --as=jane --as-group=dbas
kubectl auth can-i delete clusters.postgresql.cnpg.io -n default --as=jane --as-group=dbas
kubectl auth can-i create pods --subresource=exec -n default --as=jane --as-group=dbas
```

The answer is `yes` for the first command, and `no` for the other two. Pass a
subresource with `--subresource`: `kubectl auth can-i` reads `pods/exec` as the
Pod named `exec`, so it checks `pods`, not the subresource.

:::note
    Use `kubectl auth can-i` only for namespaced resources. For cluster-scoped
    resources, it sends the current namespace and can report that a request is
    allowed when it is not: run a real request, as above, instead.
:::

## Kubernetes RBAC and PostgreSQL authentication

For this persona, Kubernetes RBAC decides who can inspect a Cluster and who can
open a tunnel to it. It does not decide who can do what inside PostgreSQL. Once
connected, authentication, roles, client certificates, `pg_hba.conf` rules and
auditing remain entirely the responsibility of PostgreSQL.

## Aggregating `cnpg-viewer-role` into `view`

If your users already hold the built-in `view` ClusterRole, which excludes
Secrets but cannot see custom resources, you can extend it with read access to
CloudNativePG and plugin resources by adding the standard aggregation label to
`cnpg-viewer-role`:

```sh
kubectl label clusterrole cnpg-viewer-role \
  rbac.authorization.k8s.io/aggregate-to-view=true
```

Everyone bound to `view` then gains read access to those resources,
without any additional binding. This widens `view` for the whole cluster, so it
should be a deliberate decision of the platform team. With the Helm chart, use
the `rbac.aggregateClusterRoles` value instead, which labels the chart's own
view role, but also gives everyone bound to `edit` or `admin` write access to
CloudNativePG resources through the chart's edit role. Do not apply the label to
`cnpg-dba-viewer`, which would also give every `view` user access to
`pods/proxy`, leases and VolumeSnapshots.
