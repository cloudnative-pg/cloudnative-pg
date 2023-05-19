# Exposing Postgres Services

This section explains how to expose a PostgreSQL service externally, allowing access
to your PostgreSQL database **from outside your Kubernetes cluster** using
NGINX Ingress Controller.

If you followed the [QuickStart](./quickstart.md), you should have by now
a database that can be accessed inside the cluster via the
`cluster-example-rw` (primary) and `cluster-example-r` (read-only)
services in the `default` namespace. Both services use port `5432`.

Let's assume that you want to make the primary instance accessible from external
accesses on port `5432`. A typical use case, when moving to a Kubernetes
infrastructure, is indeed the one represented by **legacy applications**
that cannot be easily or sustainably "containerized". A sensible workaround
is to allow those applications that most likely reside in a virtual machine
or a physical server, to access a PostgreSQL database inside a Kubernetes cluster
in the same network.

!!! Warning
    Allowing access to a database from the public network could expose
    your database to potential attacks from malicious users. Ensure you
    secure your database before granting external access or that your
    Kubernetes cluster is only reachable from a private network.

For this example, you will use [NGINX Ingress Controller](https://kubernetes.github.io/ingress-nginx/),
since it is maintained directly by the Kubernetes project and can be set up
on every Kubernetes cluster. Many other controllers are available (see the
[Kubernetes documentation](https://kubernetes.io/docs/concepts/services-networking/ingress-controllers/)
for a comprehensive list).

We assume that:

* the NGINX Ingress controller has been deployed and works correctly
* it is possible to create a service of type `LoadBalancer` in your cluster


!!! Important
    Ingresses are only required to expose HTTP and HTTPS traffic. While the NGINX
    Ingress controller can, not all Ingress objects can expose arbitrary ports or
    protocols.

The first step is to create a `tcp-services` `ConfigMap` whose data field
contains info on the externally exposed port and the namespace, service and
port to point to internally.

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: tcp-services
  namespace: ingress-nginx
data:
  5432: default/cluster-example-rw:5432
```

Then, if you've installed NGINX Ingress Controller as suggested in their
documentation, you should have an `ingress-nginx` service. You'll have to add
the 5432 port to the `ingress-nginx` service to expose it.
The ingress will redirect incoming connections on port 5432 to your database.

```yaml
apiVersion: v1
kind: Service
metadata:
  name: ingress-nginx
  namespace: ingress-nginx
  labels:
    app.kubernetes.io/name: ingress-nginx
    app.kubernetes.io/part-of: ingress-nginx
spec:
  type: LoadBalancer
  ports:
    - name: http
      port: 80
      targetPort: 80
      protocol: TCP
    - name: https
      port: 443
      targetPort: 443
      protocol: TCP
    - name: postgres
      port: 5432
      targetPort: 5432
      protocol: TCP
  selector:
    app.kubernetes.io/name: ingress-nginx
    app.kubernetes.io/part-of: ingress-nginx
```

You can use [`cluster-expose-service.yaml`](samples/cluster-expose-service.yaml)  and apply it
using `kubectl`.

!!! Warning
    If you apply this file directly, you will overwrite any previous change
    in your `ConfigMap` and `Service` of the Ingress

Now you will be able to reach the PostgreSQL Cluster from outside your Kubernetes cluster.

!!! Important
    Make sure you configure `pg_hba` to allow connections from the Ingress.

## Testing on Minikube

On Minikube you can setup the ingress controller running:

```sh
minikube addons enable ingress
```

Then, patch the `tcp-services` ConfigMap to redirect to the primary the
connections on port 5432 of the Ingress:

```sh
kubectl patch configmap tcp-services -n ingress-nginx \
  --patch '{"data":{"5432":"default/cluster-example-rw:5432"}}'
```

You can then patch the deployment to allow access on port 5432.
Create a file called `patch.yaml` with the following content:

```yaml
spec:
  template:
    spec:
      containers:
      - name: controller
        ports:
         - containerPort: 5432
           hostPort: 5432
```

and apply it to the `ingress-nginx-controller` deployment:

```sh
kubectl patch deployment ingress-nginx-controller --patch "$(cat patch.yaml)" -n ingress-nginx
```

You can access the primary from your machine running:

```sh
psql -h $(minikube ip) -p 5432 -U postgres
```
