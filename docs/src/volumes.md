# Volumes

CloudNativePG utilizes several volumes to store data and configuration. These
volumes are created by the operator and are automatically added to the `Cluster`
pods. There are the following default volumes that are created for each pod in
the cluster:

## Default Volumes

### pgdata

This volume is used to store the Postgres database files. It's backed by a Persistent 
Volume Claim (PVC) which ensures that the data will persist across pod restarts.

### scratch-data

An ephemeral volume used for temporary storage. An upper bound on the size can be 
configured via the `spec.ephemeralVolumesSizeLimit.temporaryData` field in the cluster 
spec. This volume exists on the node's filesystem and is ephemeral, meaning
that it will not persist across pod restarts.

### shm

This volume is used as shared memory space for Postgres, also an ephemeral type but 
stored in-memory. An upper bound on the size can be configured via the 
`spec.ephemeralVolumesSizeLimit.shm` field in the cluster spec.

## Conditional Volumes

### superuser-secret

Created if the `spec.enableSuperuserAccess` field is set to true. This volume mounts a 
Kubernetes Secret that contains superuser credentials for the Postgres database.

### app-secret

Created when application database creation is enabled through various methods. This
volume mounts a Kubernetes Secret that contains credentials for the application database.

### pg-wal

Generated if the `spec.walStorage` specification exists. When set to true, WAL files will
be stored in a separate volume from the main database files. This volume is backed by a
Persistent Volume Claim (PVC) which ensures that the data will persist across pod restarts.

### projected

Created if the `spec.projectedVolumeTemplate` exists in the cluster spec. This volume creates
a [projected volume](https://kubernetes.io/docs/concepts/storage/projected-volumes/) from 
the spec provided in the `spec.projectedVolumeTemplate` field.


