# Instance Pod Configuration

## Projected Volumes 

CloudNativePG supports to mount custom files inside the Postgres pods through 
`.spec.projectedVolumeTemplate`, this is useful for several Postgres features and extensions 
that require additional data files. In CloudNativePG, `.spec.projectedVolumeTemplate` field is a
[projected volume](https://kubernetes.io/docs/concepts/storage/projected-volumes/) template in kubernetes,
which allows user to mount arbitrary data under `/projected` folder in Postgres pods. 

Here is a simple example about how to mount an existing tls Secret (named sample-secret) as files 
into Postgres pods. The values for the Secret keys `tls.crt` and `tls.key` in sample-secret will be mounted 
as files into path `/projected/certificate/tls.crt` and `/projected/certificate/tls.key` in Postgres pod. 

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-example-projected-volumes
spec:
  instances: 3
  projectedVolumeTemplate:
    sources:
      - secret:
          name: sample-secret
          items:
            - key: tls.crt
              path: certificate/tls.crt
            - key: tls.key
              path: certificate/tls.key
  storage:
    size: 1Gi
```

You can find a complete example using projected volume template to mount Secret and Configmap in
the [cluster-example-projected-volume.yaml](samples/cluster-example-projected-volume.yaml) deployment manifest.

## Ephemeral volumes

CloudNativePG relies on [ephemeral volumes](https://kubernetes.io/docs/concepts/storage/ephemeral-volumes/)
for part of the internal activities. Ephemeral volumes exist for the sole duration of
a pod's life, without persisting across pod restarts.

### Volume for temporary storage

An ephemeral volume used for temporary storage. An upper bound on the size can be
configured via the `spec.ephemeralVolumesSizeLimit.temporaryData` field in the cluster
spec.

### Volume for shared memory

This volume is used as shared memory space for Postgres, also an ephemeral type but
stored in-memory. An upper bound on the size can be configured via the
`spec.ephemeralVolumesSizeLimit.shm` field in the cluster spec. This is used only
in case of [PostgreSQL running with `posix` shared memory dynamic allocation](postgresql_conf.md#dynamic-shared-memory-settings).

## Environment variables

Some system behavior can be customized using environment variables. One example is
the `LDAPCONF` variable, which may point to a custom LDAP configuration file. Another
example is the `TZ` environment variable, which represents the timezone used by the
PostgreSQL container.

CloudNativePG allows the user to set custom environment variables via the `env` and
the `envFrom` stanza of the cluster specification.

The following is a definition of a PostgreSQL cluster using the `Australia/Sydney`
timezone as the default cluster-level timezone:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-example
spec:
  instances: 3

  env:
  - name: TZ
    value: Australia/Sydney

  storage:
    size: 1Gi
```

The `envFrom` stanza can refer to ConfigMaps or Secrets to use their content
as environment variables:

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: cluster-example
spec:
  instances: 3

  envFrom:
  - configMapRef:
      name: config-map-name
  - secretRef:
      name: secret-name

  storage:
    size: 1Gi
```

The operator doesn't allow setting the following environment variables:

- `POD_NAME`
- `NAMESPACE`
- any environment variable whose name starts with `PG`.

Any change in the `env` or in the `envFrom` section will trigger a rolling
update of the PostgreSQL Pods.

If the `env` or the `envFrom` section refers to a Secret or a ConfigMap, the
operator will not detect any changes in them and will not trigger a rollout.
The Kubelet use the same behavior with Pods, and the user is supposed to
trigger the Pod rollout manually.
