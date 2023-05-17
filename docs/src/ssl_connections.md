# Client TLS/SSL Connections

!!! Seealso "Certificates"
    Please refer to the ["Certificates"](certificates.md)
    page for more details on how CloudNativePG supports TLS certificates.

The CloudNativePG operator has been designed to work with TLS/SSL for both encryption in transit and
authentication, on server and client sides. Clusters created using the CNPG operator comes with a Certification
Authority (CA) to create and sign TLS client certificates. Through the `cnpg` plugin for `kubectl` you can
issue a new TLS client certificate which can be used to authenticate a user instead of using passwords.

Please refer to the following steps to authenticate via TLS/SSL certificates, which assume you have
installed a cluster using the [cluster-example-pg-hba.yaml](samples/cluster-example-pg-hba.yaml)
manifest. According to the convention over configuration paradigm, that file automatically creates an `app`
database which is owned by a user called `app` (you can change this convention through the `initdb`
configuration in the `bootstrap` section).

## Issuing a new certificate

!!! Seealso "About CNPG plugin for kubectl"
    Please refer to the ["Certificates" section in the "CloudNativePG Plugin"](cnpg-plugin.md#certificates)
    page for details on how to use the plugin for `kubectl`.

You can create a certificate for the `app` user in the `cluster-example` PostgreSQL cluster as follows:

```shell
kubectl cnpg certificate cluster-app \
  --cnpg-cluster cluster-example \
  --cnpg-user app
```

You can now verify the certificate with:

```shell
kubectl get secret cluster-app \
  -o jsonpath="{.data['tls\.crt']}" \
  | base64 -d | openssl x509 -text -noout \
  | head -n 11
```

Output:

```console

Certificate:
  Data:
    Version: 3 (0x2)
    Serial Number:
      5d:e1:72:8a:39:9f:ce:51:19:9d:21:ff:1e:4b:24:5d
    Signature Algorithm: ecdsa-with-SHA256
    Issuer: OU = default, CN = cluster-example
    Validity
      Not Before: Mar 22 10:22:14 2021 GMT
      Not After : Mar 22 10:22:14 2022 GMT
    Subject: CN = app
```

As you can see, TLS client certificates by default are created with 90 days of validity, and with a simple CN that
corresponds to the username in PostgreSQL. This is necessary to leverage the `cert` authentication method for `hostssl`
entries in `pg_hba.conf`.

## Testing the connection via a TLS certificate

Now we will test this client certificate by configuring a demo client application that connects to our CloudNativePG
cluster.

The following manifest called `cert-test.yaml` creates a demo Pod with a test application
in the same namespace where your database cluster is running:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: cert-test
spec:
  replicas: 1
  selector:
    matchLabels:
      app: webtest
  template:
    metadata:
      labels:
        app: webtest
    spec:
      containers:
        - image: ghcr.io/cloudnative-pg/webtest:1.6.0
          name: cert-test
          volumeMounts:
            - name: secret-volume-root-ca
              mountPath: /etc/secrets/ca
            - name: secret-volume-app
              mountPath: /etc/secrets/app
          ports:
            - containerPort: 8080
          env:
            - name: DATABASE_URL
              value: >
                sslkey=/etc/secrets/app/tls.key
                sslcert=/etc/secrets/app/tls.crt
                sslrootcert=/etc/secrets/ca/ca.crt
                host=cluster-example-rw.default.svc
                dbname=app
                user=app
                sslmode=verify-full
            - name: SQL_QUERY
              value: SELECT 1
          readinessProbe:
            httpGet:
              port: 8080
              path: /tx
      volumes:
        - name: secret-volume-root-ca
          secret:
            secretName: cluster-example-ca
            defaultMode: 0600
        - name: secret-volume-app
          secret:
            secretName: cluster-app
            defaultMode: 0600
```

This Pod will mount secrets managed by the CloudNativePG operator, including:

* `sslcert`: the TLS client public certificate
* `sslkey`: the TLS client certificate private key
* `sslrootcert`: the TLS Certification Authority certificate, that signed the certificate on
  the server to be used to verify the identity of the instances

They will be used to create the default resources that `psql` (and other libpq based applications like `pgbench`)
requires to establish a TLS encrypted connection to the Postgres database.

By default `psql` searches for certificates inside the `~/.postgresql` directory of the current user, but we can use
the sslkey, sslcert, sslrootcert options to point libpq to the actual location of the cryptographic material.
The content of the above files is gathered from the secrets that were previously created by using the `cnpg` plugin for
kubectl.

Now deploy the application:

```shell
kubectl create -f cert-test.yaml
```

Then we will use created Pod as PostgreSQL client to validate SSL connection and
authentication using TLS certificates we just created.

A readiness probe has been configured to ensure that the application is ready when the database server can be
reached.

You can verify that the connection works by executing an interactive `bash` inside the Pod's container to run `psql`
using the necessary options. The PostgreSQL server is exposed through the read-write Kubernetes service. We will point
the `psql` command to connect to this service:

```shell
kubectl exec -it cert-test -- bash -c "psql
'sslkey=/etc/secrets/app/tls.key sslcert=/etc/secrets/app/tls.crt
sslrootcert=/etc/secrets/ca/ca.crt host=cluster-example-rw.default.svc dbname=app
user=app sslmode=verify-full' -c 'select version();'"
```

Output :

```console
                                        version
--------------------------------------------------------------------------------------
------------------
PostgreSQL 15.3 on x86_64-pc-linux-gnu, compiled by gcc (GCC) 8.3.1 20191121 (Red Hat
8.3.1-5), 64-bit
(1 row)
```
