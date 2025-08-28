# Experimental features
<!-- SPDX-License-Identifier: CC-BY-4.0 -->

This page lists experimental (alpha) features in CloudNativePG. Alpha features are intended for early evaluation and feedback. They may change or be removed without prior notice and aren’t recommended for production use. Interfaces, flags, annotations, and defaults can change between releases.

Unless stated otherwise, experimental toggles are exposed under the `alpha.cnpg.io/*` annotation namespace on the Cluster resource and are not inherited by other objects unless explicitly specified.

## Instance pprof

`alpha.cnpg.io/enableInstancePprof`
:   When set to `"true"` on a `Cluster` resource, each instance Pod exposes the Go pprof HTTP server started by the instance manager. 
    The server listens on `0.0.0.0:6060` inside the pod, and a container port named `pprof` on `6060/TCP` is added to the pod spec.

    Pprof endpoints are served under `/debug/pprof` (the root path `/` is not served), for example:

      - `/debug/pprof/` (index)
      - `/debug/pprof/heap`
      - `/debug/pprof/profile?seconds=30`

### How to enable

Add the annotation to your Cluster metadata:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-example
  annotations:
    alpha.cnpg.io/enableInstancePprof: "true"
spec:
  instances: 3
  # ...
```

- Changing this annotation updates the instance pod spec (adds port 6060 and flag) and triggers a rolling update.

### Quick local test (port-forward)

!!! Warning
    This is a simple local testing example using `kubectl port-forward`, not the intended way to expose the feature in production.
    Treat pprof as a sensitive debugging interface and avoid exposing it publicly; if you must access it remotely, secure it with proper network policies and access controls.

Use port-forwarding and hit the pprof endpoints:

```bash
kubectl port-forward pod/<instance-pod> 6060
curl -sS http://localhost:6060/debug/pprof/ | head
go tool pprof http://localhost:6060/debug/pprof/profile?seconds=30
```

### Troubleshooting

- Connection refused:
  - Verify the cluster has the `alpha.cnpg.io/enableInstancePprof: "true"` annotation.
  - Check the instance manager command includes `--pprof-server` and port `6060/TCP` is present:
    `kubectl -n <ns> describe pod <instance-pod>` and inspect the container Command/Ports.
  - Ensure NetworkPolicies permit access if you’re not using port-forwarding.
- TLS: the pprof server is plain HTTP on 6060.

### Disable

Remove the annotation or set it to `"false"`. The operator will execute a rolling update to remove the pprof port/flag.

### See also

- Operator controller pprof (not per-instance): see [Operator configuration](operator_conf.md) for `--pprof-server=true` on the operator deployment.
