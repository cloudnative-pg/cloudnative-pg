# Cloud Native PostgreSQL

**Cloud Native PostgreSQL** is a stack designed by [2ndQuadrant](https://www.2ndquadrant.com) to manage PostgreSQL
workloads on Kubernetes, particularly optimised for Private Cloud environments with Local Persistent Volumes (PV).

## Quickstart for local testing (TODO)

Temporary information on how to test PG Operator using private images on Quay.io:

```bash
kind create cluster --name pg
make deploy CONTROLLER_IMG=internal.2ndq.io/k8s/cloud-native-postgresql:$(git symbolic-ref --short HEAD | tr / _)
kubectl apply -f config/manager/2ndquadrant-k8s-postgresql-poc-secret.yaml
kubectl apply -f docs/src/samples/cluster-emptydir.yaml
```
