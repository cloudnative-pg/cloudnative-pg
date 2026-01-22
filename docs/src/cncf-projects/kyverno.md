---
id: kyverno
title: Kyverno
---

# Kyverno

## About

[Kyverno](https://kyverno.io/) is a CNCF Incubating project that was accepted in
2021. It is a policy engine designed for Kubernetes that allows you to manage
policies as Kubernetes resources. Kyverno provides powerful capabilities for
admission control, resource validation, and security policy enforcement without
requiring you to write custom admission controllers.

Key features of Kyverno:

- **Policy as Code:** Define policies using Kubernetes YAML manifests
- **Admission Control:** Validate and mutate resources at admission time
- **Resource Validation:** Enforce best practices and security standards
- **Generate Policies:** Automatically create supporting resources
- **Background Scanning:** Audit existing resources for policy compliance
- **Image Verification:** Verify container image signatures and attestations

To install Kyverno in your environment, follow the instructions in the documentation:
[https://kyverno.io/docs/installation/](https://kyverno.io/docs/installation/)

## Integration with PostgreSQL and CloudNativePG

Kyverno can be used alongside [CloudNativePG](https://cloudnative-pg.io/) to
provide admission controls, resource validation, and security policies for
PostgreSQL clusters. This enables teams to enforce organizational standards,
security best practices, and operational requirements for CloudNativePG clusters
at the Kubernetes API level.

Common use cases for Kyverno with CloudNativePG include:

- **Resource Validation:** Ensure CloudNativePG clusters meet organizational
  standards (e.g., required labels, resource limits)
- **Security Policies:** Enforce security best practices (e.g., TLS requirements,
  image pull policies)
- **Compliance:** Automatically add required annotations, labels, or sidecars
- **Cost Management:** Enforce resource quotas and limits to control costs

## Examples

:::note
The following examples are illustrative and not officially supported or maintained
by the CloudNativePG project. They are provided to demonstrate common integration
patterns with Kyverno.
:::

### Example: Validating Resource Requirements

The following example creates a `ClusterPolicy` that ensures all CloudNativePG
clusters have resource limits defined for their instances:

```yaml
apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: require-cnpg-resource-limits
  annotations:
    policies.kyverno.io/title: Require Resource Limits for CloudNativePG Clusters
    policies.kyverno.io/category: Best Practices
    policies.kyverno.io/severity: medium
    policies.kyverno.io/subject: Cluster
    policies.kyverno.io/description: >-
      All CloudNativePG clusters must define resource limits for their instances
      to prevent resource exhaustion and ensure fair resource allocation.
spec:
  validationFailureAction: enforce
  background: true
  rules:
    - name: check-resource-limits
      match:
        any:
          - resources:
              kinds:
                - Cluster
              apiVersions:
                - postgresql.cnpg.io/v1
      validate:
        message: "Resource limits must be specified for all instances"
        pattern:
          spec:
            instances: ">=1"
            resources:
              requests:
                memory: "?*"
                cpu: "?*"
              limits:
                memory: "?*"
                cpu: "?*"
```

This policy ensures that any `Cluster` resource created or updated must have
both resource requests and limits defined. The `validationFailureAction: enforce`
setting means that clusters without resource limits will be rejected at
admission time.

### Example: Mutating Clusters with Default Values

Kyverno can also mutate resources to add default values or required fields. The
following example automatically adds required labels and annotations to all
CloudNativePG clusters:

```yaml
apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: add-cnpg-defaults
  annotations:
    policies.kyverno.io/title: Add Default Labels and Annotations
    policies.kyverno.io/category: Best Practices
    policies.kyverno.io/severity: low
    policies.kyverno.io/subject: Cluster
    policies.kyverno.io/description: >-
      Automatically add default labels and annotations to CloudNativePG clusters.
spec:
  rules:
    - name: add-default-metadata
      match:
        any:
          - resources:
              kinds:
                - Cluster
              apiVersions:
                - postgresql.cnpg.io/v1
      mutate:
        patchStrategicMerge:
          metadata:
            labels:
              +(managed-by): "cloudnative-pg"
              +(policy-enforced): "kyverno"
            annotations:
              +(cnpg.io/kyverno-policy): "add-cnpg-defaults"
```

This policy uses the `mutate` action to add labels and annotations to all
CloudNativePG clusters. The `+` prefix indicates that these fields should be
added if they don't already exist, without overwriting existing values.

Kyverno also supports generating supporting resources (such as NetworkPolicies)
and enforcing security policies (TLS requirements, image restrictions, etc.).
Refer to the [Kyverno documentation](https://kyverno.io/docs/) for additional
policy patterns and use cases.

## Observability and Compliance

Kyverno provides PolicyReports and background scanning capabilities that can be
used to audit CloudNativePG clusters for compliance over time. You can check
policy compliance using:

```sh
kubectl get policyreports -n <namespace>
kubectl get clusterpolicyreports
```

Refer to the [Kyverno documentation](https://kyverno.io/docs/) for details on
background scanning and policy reporting.

For more information about Kyverno policies, advanced use cases, and best
practices, refer to the [official Kyverno documentation](https://kyverno.io/docs/).
