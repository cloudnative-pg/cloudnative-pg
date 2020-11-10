# Client SSL Connections 

Cloud Native PostgreSQL currently creates a Certification Authority (CA) for
every cluster, this CA it's used to sign the certificates to offer to clients
and create a secure connection with the client

## Using SSL to connect to pods

Using SSL to connect to the cluster

```sh
psql postgresql://cluster-example-rw:5432/app?sslmode=require
```

This will generate a secure connection with the `rw` service of cluster
`cluster-example`.
