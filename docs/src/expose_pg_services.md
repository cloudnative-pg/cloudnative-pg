# Exposing Postgres services

You can expose a PostgreSQL service externally, allowing access
to your PostgreSQL database from outside your Kubernetes cluster using
NGINX Ingress Controller.

If you followed the [Quick start](./quickstart.md), you have
a database that can be accessed inside the cluster by way of the
`cluster-example-rw` (primary) and `cluster-example-r` (read-only)
services in the `default` namespace. Both services use port 5432.

Suppose that you want to make the primary instance accessible from external
accesses on port 5432. When moving to a Kubernetes
infrastructure, a typical use case is the one represented by legacy applications
that can't be easily or sustainably "containerized." A sensible workaround
is to allow those applications that most likely reside in a virtual machine
or a physical server to access a PostgreSQL database inside a Kubernetes cluster
in the same network.

!!! Warning
    Allowing access to a database from the public network can expose
    your database to potential attacks from malicious users. Make sure that you
    secure your database before granting external access or that your
    Kubernetes cluster is reachable only from a private network.

For this example, because it's maintained directly by the Kubernetes project and can be set up
on every Kubernetes cluster, you use [NGINX Ingress Controller](https://kubernetes.github.io/ingress-nginx/). 
Many other controllers are available. (See the
[Kubernetes documentation](https://kubernetes.io/docs/concepts/services-networking/ingress-controllers/)
for a comprehensive list.)

The example assumes that:

* NGINX Ingress Controller was deployed and works correctly.
* It's possible to create a service of type `LoadBalancer` in your cluster.


!!! Important
    Ingresses are only required to expose HTTP and HTTPS traffic. While the NGINX
    Ingress Controller can, not all Ingress objects can expose arbitrary ports or
    protocols.

First, create a `tcp-services` ConfigMap whose data field
contains info on the externally exposed port and the namespace, service, and
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

If you installed NGINX Ingress Controller as suggested in their
documentation, you have an `ingress-nginx` service. To expose the 5432 port, you must add
it to the `ingress-nginx` service.
The Ingress redirects incoming connections on port 5432 to your database.

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

You can use [`cluster-expose-service.yaml`](samples/cluster-expose-service.yaml) and apply it
using `kubectl`.

!!! Warning
    Applying this file directly overwrites any previous change
    in your `ConfigMap` and `Service` of the Ingress.

You can now reach the PostgreSQL cluster from outside your Kubernetes cluster.

!!! Important
    Make sure you configure `pg_hba` to allow connections from the Ingress.

## Testing on Minikube

On Minikube, you can set up the Ingress Controller by running:

```sh
minikube addons enable ingress
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

Apply it to the `ingress-nginx-controller` deployment:

```sh
kubectl patch deployment ingress-nginx-controller --patch "$(cat patch.yaml)" -n ingress-nginx
```

You can access the primary from your machine by running:

```sh
psql -h $(minikube ip) -p 5432 -U postgres
```
