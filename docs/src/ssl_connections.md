---
id: ssl_connections
sidebar_position: 330
title: Client TLS/SSL connections
---

# Client TLS/SSL connections
<!-- SPDX-License-Identifier: CC-BY-4.0 -->

:::note[Certificates]
    See [Certificates](certificates.md)
    for more details on how CloudNativePG supports TLS certificates.
:::

The CloudNativePG operator was designed to work with TLS/SSL for both
encryption in transit and authentication on the server and client sides.
Clusters created using the CNPG operator come with a certification authority
(CA) to create and sign TLS client certificates. Using the cnpg plugin for
kubectl, you can issue a new TLS client certificate for authenticating a user
instead of using passwords.

These instructions for authenticating using TLS/SSL certificates assume you
installed a cluster using the
[cluster-example-pg-hba.yaml](samples/cluster-example-pg-hba.yaml) manifest.
According to the convention-over-configuration paradigm, that file creates an
`app` database that's owned by a user called app. (You can change this
convention by way of the `initdb` configuration in the `bootstrap` section.)

## Issuing a new certificate

:::note[About CNPG plugin for kubectl]
    See the [Certificates in the CloudNativePG plugin](kubectl-plugin.md#certificates)
    content for details on how to use the plugin for kubectl.
:::

You can create a certificate for the app user in the `cluster-example`
PostgreSQL cluster as follows:

```shell
kubectl cnpg certificate cluster-app \
  --cnpg-cluster cluster-example \
  --cnpg-user app
```

You can now verify the certificate:

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

As you can see, TLS client certificates by default are created with 90 days of
validity, and with a simple CN that corresponds to the username in PostgreSQL.
You can specify the validity and threshold values using the
`EXPIRE_CHECK_THRESHOLD` and `CERTIFICATE_DURATION` parameters. This is
necessary to leverage the `cert` authentication method for `hostssl` entries in
`pg_hba.conf`.

## Testing the connection via a TLS certificate

Next, test this client certificate by configuring a demo client application
that connects to your CloudNativePG cluster.

The following manifest, called `cert-test.yaml`, creates a demo pod with a test
application in the same namespace where your database cluster is running:

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
        - image: ghcr.io/cloudnative-pg/webtest:1.7.0
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

This pod mounts secrets managed by the CloudNativePG operator, including:

* `sslcert` – The TLS client public certificate.
* `sslkey` – The TLS client certificate private key.
* `sslrootcert` – The TLS CA certificate that signed the certificate on
  the server to use to verify the identity of the instances.

They're used to create the default resources that psql (and other libpq-based
applications, like pgbench) requires to establish a TLS-encrypted connection to
the Postgres database.

By default, psql searches for certificates in the `~/.postgresql` directory of
the current user, but you can use the `sslkey`, `sslcert`, and `sslrootcert`
options to point libpq to the actual location of the cryptographic material.
The content of these files is gathered from the secrets that were previously
created by using the cnpg plugin for kubectl.

Deploy the application:

```shell
kubectl create -f cert-test.yaml
```

Then use the created pod as the PostgreSQL client to validate the SSL
connection and authentication using the TLS certificates you just created.

A readiness probe was configured to ensure that the application is ready when
the database server can be reached.

You can verify that the connection works. To do so, execute an interactive
`bash` inside the pod's container to run psql using the necessary options. The
PostgreSQL server is exposed through the read-write Kubernetes service. Point
the psql command to connect to this service:

```shell
kubectl exec -it cert-test -- bash -c "psql
'sslkey=/etc/secrets/app/tls.key sslcert=/etc/secrets/app/tls.crt
sslrootcert=/etc/secrets/ca/ca.crt host=cluster-example-rw.default.svc dbname=app
user=app sslmode=verify-full' -c 'select version();'"
```

Output:

```console
                                        version
--------------------------------------------------------------------------------------
------------------
PostgreSQL 18.1 on x86_64-pc-linux-gnu, compiled by gcc (GCC) 8.3.1 20191121 (Red Hat
8.3.1-5), 64-bit
(1 row)
```

## About TLS protocol versions

By default, the operator sets both [`ssl_min_protocol_version`](https://www.postgresql.org/docs/current/runtime-config-connection.html#GUC-SSL-MIN-PROTOCOL-VERSION)
and [`ssl_max_protocol_version`](https://www.postgresql.org/docs/current/runtime-config-connection.html#GUC-SSL-MAX-PROTOCOL-VERSION)
to `TLSv1.3`.

This assumes that the PostgreSQL operand images include an OpenSSL library that
supports the `TLSv1.3` version. If not, or if your client applications need a
lower version number, you need to manually configure it in the PostgreSQL
configuration as any other Postgres GUC.
