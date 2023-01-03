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






