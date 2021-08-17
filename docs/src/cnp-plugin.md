# Cloud Native PostgreSQL Plugin

Cloud Native PostgreSQL provides a plugin for `kubectl` to manage a cluster in Kubernetes.
The plugin also works with `oc` in an OpenShift environment.

## Install

You can install the plugin in your system with:

```sh
curl -sSfL \
  https://github.com/EnterpriseDB/kubectl-cnp/raw/main/install.sh | \
  sudo sh -s -- -b /usr/local/bin
```

## Use

Once the plugin was installed and deployed, you can start using it like this:

```shell
kubectl cnp <command> <args...>
```

### Status

The `status` command provides a brief of the current status of your cluster.

```shell
kubectl cnp status cluster-example
```

```shell
Cluster in healthy state   
Name:              cluster-example
Namespace:         default
PostgreSQL Image:  quay.io/enterprisedb/postgresql:13
Primary instance:  cluster-example-1
Instances:         3
Ready instances:   3

Instances status
Pod name           Current LSN  Received LSN  Replay LSN  System ID            Primary  Replicating  Replay paused  Pending restart
--------           -----------  ------------  ----------  ---------            -------  -----------  -------------  ---------------
cluster-example-1  0/6000060                              6927251808674721812  ✓        ✗            ✗              ✗
cluster-example-2               0/6000060     0/6000060   6927251808674721812  ✗        ✓            ✗              ✗
cluster-example-3               0/6000060     0/6000060   6927251808674721812  ✗        ✓            ✗              ✗

```

You can also get a more verbose version of the status by adding `--verbose` or just `-v`

```shell
kubectl cnp status cluster-example --verbose
```

```shell
Cluster in healthy state   
Name:              cluster-example
Namespace:         default
PostgreSQL Image:  quay.io/enterprisedb/postgresql:13
Primary instance:  cluster-example-1
Instances:         3
Ready instances:   3

PostgreSQL Configuration
archive_command = '/controller/manager wal-archive %p'
archive_mode = 'on'
archive_timeout = '5min'
full_page_writes = 'on'
hot_standby = 'true'
listen_addresses = '*'
logging_collector = 'off'
max_parallel_workers = '32'
max_replication_slots = '32'
max_worker_processes = '32'
port = '5432'
ssl = 'on'
ssl_ca_file = '/controller/certificates/client-ca.crt'
ssl_cert_file = '/controller/certificates/server.crt'
ssl_key_file = '/controller/certificates/server.key'
unix_socket_directories = '/var/run/postgresql'
wal_keep_size = '512MB'
wal_level = 'logical'
wal_log_hints = 'on'


PostgreSQL HBA Rules
# Grant local access
local all all peer

# Require client certificate authentication for the streaming_replica user
hostssl postgres streaming_replica all cert
hostssl replication streaming_replica all cert

# Otherwise use md5 authentication
host all all all md5


Instances status
Pod name           Current LSN  Received LSN  Replay LSN  System ID            Primary  Replicating  Replay paused  Pending restart
--------           -----------  ------------  ----------  ---------            -------  -----------  -------------  ---------------
cluster-example-1  0/6000060                              6927251808674721812  ✓        ✗            ✗              ✗
cluster-example-2               0/6000060     0/6000060   6927251808674721812  ✗        ✓            ✗              ✗
cluster-example-3               0/6000060     0/6000060   6927251808674721812  ✗        ✓            ✗              ✗
```

The command also supports output in `yaml` and `json` format.

### Promote

The meaning of this command is to `promote` a pod in the cluster to primary, so you
can start with maintenance work or test a switch-over situation in your cluster

```shell
kubectl cnp promote cluster-example cluster-example-2
```
Or you can use the instance node number to promote
```shell
kubectl cnp promote cluster-example 2
```

### Certificates

Clusters created using the Cloud Native PostgreSQL operator work with a CA to sign
a TLS authentication certificate.

To get a certificate, you need to provide a name for the secret to store
the credentials, the cluster name, and a user for this certificate

```shell
kubectl cnp certificate cluster-cert --cnp-cluster cluster-example --cnp-user appuser
```

After the secrete it's created, you can get it using `kubectl`

```shell
kubectl get secret cluster-cert
```

And the content of the same in plain text using the following commands:

```shell
kubectl get secret cluster-cert -o json | jq -r '.data | map(@base64d) | .[]'
```

### Restart

The `kubectl cnp restart` command requests the operator to orchestrate
a rollout restart for a certain cluster. This is useful to apply
configuration changes to cluster dependent objects, such as ConfigMaps
containing custom monitoring queries.

The following command will restart a given cluster in a rollout fashion:

```shell
kubectl cnp restart [cluster_name]
```

!!! Note
    If you want ConfigMaps and Secrets to be **automatically** reloaded by instances, you can
    add a label with key `k8s.enterprisedb.io/reload` to it.

### Reload

The `kubectl cnp reload` command requests the operator to trigger a reconciliation
loop for a certain cluster. This is useful to apply configuration changes
to cluster dependent objects, such as ConfigMaps containing custom monitoring queries.

The following command will reload all configurations for a given cluster:

```shell
kubectl cnp reload [cluster_name]
```
