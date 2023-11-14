# Kubectl plugin

CloudNativePG provides a plugin for kubectl to manage a cluster in Kubernetes. 

## Install

You can install the cnpg plugin using a variety of methods.

!!! Note
    For air-gapped systems, installation by way of package managers, using previously
    downloaded files, might be a good option.

### Via the installation script

```sh
curl -sSfL \
  https://github.com/cloudnative-pg/cloudnative-pg/raw/main/hack/install-cnpg-plugin.sh | \
  sudo sh -s -- -b /usr/local/bin
```

### Using the Debian or RedHat packages

In the
[releases section of the GitHub repository](https://github.com/cloudnative-pg/cloudnative-pg/releases),
you can navigate to any release of interest. (Select the same release
as your CloudNativePG operator or a newer one.) In that section is an **Assets**
section that has prebuilt packages for a variety of systems.
As a result, you can follow standard practices and instructions to install
them in your systems.

#### Debian packages

For example, suppose you want to install the 1.18.1 release of the plugin for an Intel-based
64-bit server. First, download the right `.deb` file:

``` sh
$ wget https://github.com/cloudnative-pg/cloudnative-pg/releases/download/v1.18.1/kubectl-cnpg_1.18.1_linux_x86_64.deb
```

Then, install from the local file using `dpkg`:

``` sh
$ dpkg -i kubectl-cnpg_1.18.1_linux_x86_64.deb 
(Reading database ... 16102 files and directories currently installed.)
Preparing to unpack kubectl-cnpg_1.18.1_linux_x86_64.deb ...
Unpacking cnpg (1.18.1) over (1.18.1) ...
Setting up cnpg (1.18.1) ...
```

#### RPM packages

As in the example for `.deb` packages, suppose you're installing the 1.18.1 release for an
Intel 64-bit machine. Use the `--output` flag to provide a file name.

``` sh
curl -L https://github.com/cloudnative-pg/cloudnative-pg/releases/download/v1.18.1/kubectl-cnpg_1.18.1_linux_x86_64.rpm \
  --output kube-plugin.rpm
```

Install with yum, and it's ready to use:

``` sh
$ yum --disablerepo=* localinstall kube-plugin.rpm
yum --disablerepo=* localinstall kube-plugin.rpm    
Failed to set locale, defaulting to C.UTF-8
Dependencies resolved.
====================================================================================================
 Package            Architecture         Version                   Repository                  Size
====================================================================================================
Installing:
 cnpg               x86_64               1.18.1-1                  @commandline                14 M

Transaction Summary
====================================================================================================
Install  1 Package

Total size: 14 M
Installed size: 43 M
Is this ok [y/N]: y
```

### Using Krew

If you already have [Krew](https://krew.sigs.k8s.io/) installed, you can
run:

```sh
kubectl krew install cnpg
```

When a new version of the plugin is released, you can update the existing
installation:

```sh
kubectl krew update
kubectl krew upgrade cnpg
```

### Supported architectures

CloudNativePG plugin is currently built for the following
operating system and architectures:

* Linux
  * amd64
  * arm 5/6/7
  * arm64
  * s390x
  * ppc64le
* macOS
  * amd64
  * arm64
* Windows
  * 386
  * amd64
  * arm 5/6/7
  * arm64

## Use

After the plugin is installed and deployed, you can start using it like this:

```shell
kubectl cnpg <command> <args...>
```

### Generation of installation manifests

You can use the cnpg plugin to generate the YAML manifest for
installing the operator. This option is typically used
to override some default configurations, such as number of replicas,
installation namespace, namespaces to watch, and so on.

For details and available options, run:

```shell
kubectl cnpg install generate --help
```

The main options are:

- `-n` - Namespace in which to install the operator (by default: `cnpg-system`).
- `--replicas` – Number of replicas in the deployment.
- `--version` – Minor version of the operator to install, such as `1.17`.
  If you specify a minor version, the plugin installs the latest patch
  version of that minor version. If you don't supply a version, the plugin
  installs the latest `MAJOR.MINOR.PATCH` version of the operator.
- `--watch-namespace` – Comma-separated string containing the namespaces to
  watch (by default all namespaces).

This example shows the `generate` command, which generates a YAML manifest that
installs the operator:

```shell
kubectl cnpg install generate \
  -n king \
  --version 1.17 \
  --replicas 3 \
  --watch-namespace "albert, bb, freddie" \
  > operator.yaml
```

Where:
- `-n king` installs the cnpg operator into the `king` namespace.
- `--version 1.17` installs the latest patch version for minor version 1.17.
- `--replicas 3` installs the operator with three replicas.
- `--watch-namespaces "albert, bb, freddie"` has the operator watch for
  changes in only the `albert`, `bb`, and `freddie` namespaces.

### Status

The `status` command provides an overview of the current status of your
cluster, including:

* **General information** – Name of the cluster, PostgreSQL's system ID, number of
  instances, current timeline, and position in the WAL.
* **Backup** – Point of recoverability and WAL archiving status as returned by
  the `pg_stat_archiver` view from the primary or designated primary in the
  case of a replica cluster.
* **Streaming replication** – Information taken directly from the `pg_stat_replication`
  view on the primary instance.
* **Instances** – Information about each Postgres instance, taken directly by each
  instance manager. In the case of a standby, the `Current LSN` field corresponds
  to the latest write-ahead log location that was replayed during recovery
  (replay LSN).

!!! Important
    This status information is taken at different times and at different
    locations, resulting in slightly inconsistent returned values. For example,
    the `Current Write LSN` location in the main header might be different
    from the `Current LSN` field in the instances status, as it's taken at
    two different time intervals.

The command also supports output in yaml and json format.

```shell
kubectl cnpg status sandbox
```

```shell
Cluster in healthy state
Name:               sandbox
Namespace:          default
System ID:          7039966298120953877
PostgreSQL Image:   ghcr.io/cloudnative-pg/postgresql:16.0
Primary instance:   sandbox-2
Instances:          3
Ready instances:    3
Current Write LSN:  3AF/EAFA6168 (Timeline: 8 - WAL File: 00000008000003AF00000075)

Continuous Backup status
First Point of Recoverability:  Not Available
Working WAL archiving:          OK
Last Archived WAL:              00000008000003AE00000079   @   2021-12-14T10:16:29.340047Z
Last Failed WAL: -

Certificates Status
Certificate Name             Expiration Date                Days Left Until Expiration
----------------             ---------------                --------------------------
cluster-example-ca           2022-05-05 15:02:42 +0000 UTC  87.23
cluster-example-replication  2022-05-05 15:02:42 +0000 UTC  87.23
cluster-example-server       2022-05-05 15:02:42 +0000 UTC  87.23

Streaming Replication status
Name       Sent LSN      Write LSN     Flush LSN     Replay LSN    Write Lag        Flush Lag        Replay Lag       State      Sync State  Sync Priority
----       --------      ---------     ---------     ----------    ---------        ---------        ----------       -----      ----------  -------------
sandbox-1  3AF/EB0524F0  3AF/EB011760  3AF/EAFEDE50  3AF/EAFEDE50  00:00:00.004461  00:00:00.007901  00:00:00.007901  streaming  quorum      1
sandbox-3  3AF/EB0524F0  3AF/EB030B00  3AF/EB030B00  3AF/EB011760  00:00:00.000977  00:00:00.004194  00:00:00.008252  streaming  quorum      1

Instances status
Name       Database Size  Current LSN   Replication role  Status  QoS         Manager Version
----       -------------  -----------   ----------------  ------  ---         ---------------
sandbox-1  302 GB         3AF/E9FFFFE0  Standby (sync)    OK      Guaranteed  1.11.0
sandbox-2  302 GB         3AF/EAFA6168  Primary           OK      Guaranteed  1.11.0
sandbox-3  302 GB         3AF/EBAD5D18  Standby (sync)    OK      Guaranteed  1.11.0
```

You can also get a more verbose version of the status by adding
`--verbose` or `-v`:

```shell
kubectl cnpg status sandbox --verbose
```

```shell
Cluster in healthy state
Name:               sandbox
Namespace:          default
System ID:          7039966298120953877
PostgreSQL Image:   ghcr.io/cloudnative-pg/postgresql:16.0
Primary instance:   sandbox-2
Instances:          3
Ready instances:    3
Current Write LSN:  3B1/61DE3158 (Timeline: 8 - WAL File: 00000008000003B100000030)

PostgreSQL Configuration
archive_command = '/controller/manager wal-archive --log-destination /controller/log/postgres.json %p'
archive_mode = 'on'
archive_timeout = '5min'
checkpoint_completion_target = '0.9'
checkpoint_timeout = '900s'
cluster_name = 'sandbox'
dynamic_shared_memory_type = 'sysv'
full_page_writes = 'on'
hot_standby = 'true'
jit = 'on'
listen_addresses = '*'
log_autovacuum_min_duration = '1s'
log_checkpoints = 'on'
log_destination = 'csvlog'
log_directory = '/controller/log'
log_filename = 'postgres'
log_lock_waits = 'on'
log_min_duration_statement = '1000'
log_rotation_age = '0'
log_rotation_size = '0'
log_statement = 'ddl'
log_temp_files = '1024'
log_truncate_on_rotation = 'false'
logging_collector = 'on'
maintenance_work_mem = '2GB'
max_connections = '1000'
max_parallel_workers = '32'
max_replication_slots = '32'
max_wal_size = '15GB'
max_worker_processes = '32'
pg_stat_statements.max = '10000'
pg_stat_statements.track = 'all'
port = '5432'
shared_buffers = '16GB'
shared_memory_type = 'sysv'
shared_preload_libraries = 'pg_stat_statements'
ssl = 'on'
ssl_ca_file = '/controller/certificates/client-ca.crt'
ssl_cert_file = '/controller/certificates/server.crt'
ssl_key_file = '/controller/certificates/server.key'
synchronous_standby_names = 'ANY 1 ("sandbox-1","sandbox-3")'
unix_socket_directories = '/controller/run'
wal_keep_size = '512MB'
wal_level = 'logical'
wal_log_hints = 'on'
cnpg.config_sha256 = '3cfa683e23fe513afaee7c97b50ce0628e0cc634bca8b096517538a9a4428efc'

PostgreSQL HBA Rules

# Grant local access
local all all peer map=local

# Require client certificate authentication for the streaming_replica user
hostssl postgres streaming_replica all cert
hostssl replication streaming_replica all cert
hostssl all cnpg_pooler_pgbouncer all cert

# Otherwise use the default authentication method
host all all all scram-sha-256


Continuous Backup status
First Point of Recoverability:  Not Available
Working WAL archiving:          OK
Last Archived WAL:              00000008000003B00000001D   @   2021-12-14T10:20:42.272815Z
Last Failed WAL: -

Streaming Replication status
Name       Sent LSN      Write LSN     Flush LSN     Replay LSN    Write Lag        Flush Lag        Replay Lag       State      Sync State  Sync Priority
----       --------      ---------     ---------     ----------    ---------        ---------        ----------       -----      ----------  -------------
sandbox-1  3B1/61E26448  3B1/61DF82F0  3B1/61DF82F0  3B1/61DF82F0  00:00:00.000333  00:00:00.000333  00:00:00.005484  streaming  quorum      1
sandbox-3  3B1/61E26448  3B1/61E26448  3B1/61DF82F0  3B1/61DF82F0  00:00:00.000756  00:00:00.000756  00:00:00.000756  streaming  quorum      1

Instances status
Name       Database Size  Current LSN   Replication role  Status  QoS         Manager Version
----       -------------  -----------   ----------------  ------  ---         ---------------
sandbox-1                 3B1/610204B8  Standby (sync)    OK      Guaranteed  1.11.0
sandbox-2                 3B1/61DE3158  Primary           OK      Guaranteed  1.11.0
sandbox-3                 3B1/62618470  Standby (sync)    OK      Guaranteed  1.11.0
```

### Promote

This command promotes a pod in the cluster to primary, enabling you to
start with maintenance work or test a switchover situation in your cluster:

```shell
kubectl cnpg promote cluster-example cluster-example-2
```

Or, you can use the instance node number to promote:

```shell
kubectl cnpg promote cluster-example 2
```

### Certificates

Clusters created using the CloudNativePG operator work with a certificate authority (CA) to sign
a TLS authentication certificate.

To get a certificate, you need to provide a name for the secret to store
the credentials, the cluster name, and a user for this certificate:

```shell
kubectl cnpg certificate cluster-cert --cnpg-cluster cluster-example --cnpg-user appuser
```

After the secret is created, you can get it using `kubectl`:

```shell
kubectl get secret cluster-cert
```

You can also get the content in plain text:

```shell
kubectl get secret cluster-cert -o json | jq -r '.data | map(@base64d) | .[]'
```

### Restart

You can use the `kubectl cnpg restart` command in two cases:

- Requesting the operator to orchestrate a rollout restart
  for a certain cluster. This case is useful to apply
  configuration changes to cluster-dependent objects, such as ConfigMaps
  containing custom monitoring queries.

- Request a single instance restart. If the instance is
  the cluster's primary, the restart occurs in place. 
  If the instance is a replica, it deletes the pod and recreates it.

```shell
# this command will restart a whole cluster in a rollout fashion
kubectl cnpg restart [clusterName]

# this command will restart a single instance, according to the policy above
kubectl cnpg restart [clusterName] [pod]
```

If the in-place restart is requested but the change can't be applied without
a switchover, the switchover takes precedence over the in-place restart. A
common case for this is a minor upgrade of PostgreSQL image.

!!! Note
    If you want the instances to reload the ConfigMaps and secrets,
    you can add a label to it with key `cnpg.io/reload`.

### Reload

The `kubectl cnpg reload` command requests for the operator to trigger a reconciliation
loop for a certain cluster. This command is useful to apply configuration changes
to cluster-dependent objects, such as ConfigMaps containing custom monitoring queries.

This command reloads all configurations for a given cluster:

```shell
kubectl cnpg reload [cluster_name]
```

### Maintenance

The `kubectl cnpg maintenance` command helps to modify one or more clusters
across namespaces and set the maintenance window values. It changes
the following fields:

* `.spec.nodeMaintenanceWindow.inProgress`
* `.spec.nodeMaintenanceWindow.reusePVC`

This command accepts as arguments `set` and `unset`. The `set` argument sets
`inProgress` to `true`. The `unset` argument sets it to to `false`.

By default, `reusePVC` is always set to `false` unless the `--reusePVC` flag is passed.

The plugin asks for a confirmation with a list of the cluster to modify
and their new values. If you accept, this action is applied to
all the clusters in the list.

If you want to set maintenance in all the PostgreSQL in your Kubernetes cluster,
use the following command:

```shell
kubectl cnpg maintenance set --all-namespaces
```

This command returns the list of all the clusters to update:

```shell
The following are the new values for the clusters
Namespace  Cluster Name     Maintenance  reusePVC
---------  ------------     -----------  --------
default    cluster-example  true         false
default    pg-backup        true         false
test       cluster-example  true         false
Do you want to proceed? [y/n]: y
```

### Report

The `kubectl cnpg report` command bundles various pieces
of information into a ZIP file.
It aims to provide the needed context to debug problems
with clusters in production.

It has two subcommands: `operator` and `cluster`.

#### Report operator

The `operator` subcommand requests for the operator to provide information
regarding the operator deployment, configuration, and events.

!!! Important
    All confidential information in secrets and ConfigMaps is redacted.
    The data map shows the keys, but the values are empty.
    The flag `-S` / `--stopRedaction` defeats the redaction and shows the
    values. Use the flag at your own risk, as doing so shares private data.

!!! Note
    By default, operator logs aren't collected. You can enable operator
    log collection using the `--logs` flag.

* **Deployment information** – The operator deployment and operator pod.
* **Configuration** – The secrets and ConfigMaps in the operator namespace.
* **Events** – The events in the operator namespace.
* **Webhook configuration** – The mutating and validating webhook configurations.
* **Webhook service** – The webhook service.
* **Logs** – Logs for the operator pod in JSON-lines format. (This option is optional and off by default.) 

The command generates a ZIP file containing various manifests in YAML format
by default. (You can set the format to JSON using the `-o` flag.)
Use the `-f` flag to name a result file explicitly. If you don't use the `-f` flag, a
default timestamped filename is created for the ZIP file.

!!! Note
    The report plugin obeys kubectl conventions and looks for objects constrained
    by namespace. The cnpg operator generally isn't installed in the same
    namespace as the clusters.
    For example, the default installation namespace is `cnpg-system`.

```shell
kubectl cnpg report operator -n <namespace>
```

This command results in:

```shell
Successfully written report to "report_operator_<TIMESTAMP>.zip" (format: "yaml")
```

With the `-f` flag set:

```shell
kubectl cnpg report operator -n <namespace> -f reportRedacted.zip
```

Unzipping the file produces a timestamped top-level folder to keep the
directory tidy:

```shell
unzip reportRedacted.zip
```

This command results in:

```shell
Archive:  reportRedacted.zip
   creating: report_operator_<TIMESTAMP>/
   creating: report_operator_<TIMESTAMP>/manifests/
  inflating: report_operator_<TIMESTAMP>/manifests/deployment.yaml
  inflating: report_operator_<TIMESTAMP>/manifests/operator-pod.yaml
  inflating: report_operator_<TIMESTAMP>/manifests/events.yaml
  inflating: report_operator_<TIMESTAMP>/manifests/validating-webhook-configuration.yaml
  inflating: report_operator_<TIMESTAMP>/manifests/mutating-webhook-configuration.yaml
  inflating: report_operator_<TIMESTAMP>/manifests/webhook-service.yaml
  inflating: report_operator_<TIMESTAMP>/manifests/cnpg-ca-secret.yaml
  inflating: report_operator_<TIMESTAMP>/manifests/cnpg-webhook-cert.yaml
```

If you activate the `--logs` option, an extra subdirectory appears:

```shell
Archive:  report_operator_<TIMESTAMP>.zip
  <snipped …>
  creating: report_operator_<TIMESTAMP>/operator-logs/
  inflating: report_operator_<TIMESTAMP>/operator-logs/cnpg-controller-manager-66fb98dbc5-pxkmh-logs.jsonl
```

!!! Note
    The plugin tries to get the previous operator's logs, which is helpful
    when investigating restarted operators.
    In all cases, it also tries to get the current operator logs. If current
    and previous logs are available, it shows them both.

``` json
====== Begin of Previous Log =====
2023-03-28T12:56:41.251711811Z {"level":"info","ts":"2023-03-28T12:56:41Z","logger":"setup","msg":"Starting CloudNativePG Operator","version":"1.19.1","build":{"Version":"1.19.0+dev107","Commit":"cc9bab17","Date":"2023-03-28"}}
2023-03-28T12:56:41.251851909Z {"level":"info","ts":"2023-03-28T12:56:41Z","logger":"setup","msg":"Starting pprof HTTP server","addr":"0.0.0.0:6060"}
  <snipped …>

====== End of Previous Log =====
2023-03-28T12:57:09.854306024Z {"level":"info","ts":"2023-03-28T12:57:09Z","logger":"setup","msg":"Starting CloudNativePG Operator","version":"1.19.1","build":{"Version":"1.19.0+dev107","Commit":"cc9bab17","Date":"2023-03-28"}}
2023-03-28T12:57:09.854363943Z {"level":"info","ts":"2023-03-28T12:57:09Z","logger":"setup","msg":"Starting pprof HTTP server","addr":"0.0.0.0:6060"}
```

If the operator wasn't restarted, you still see the `====== Begin …`
and  `====== End …` guards, with no content inside.

You can verify that the confidential information is redacted by default:

```shell
cd report_operator_<TIMESTAMP>/manifests/
head cnpg-ca-secret.yaml
```

```yaml
data:
  ca.crt: ""
  ca.key: ""
metadata:
  creationTimestamp: "2022-03-22T10:42:28Z"
  managedFields:
  - apiVersion: v1
    fieldsType: FieldsV1
    fieldsV1:
```

With the `-S` (`--stopRedaction`) option activated, secrets are shown:

```shell
kubectl cnpg report operator -n <namespace> -f reportNonRedacted.zip -S
```

You get a reminder that you're about to view confidential information:

```shell
WARNING: secret Redaction is OFF. Use it with caution
Successfully written report to "reportNonRedacted.zip" (format: "yaml")
```

```shell
unzip reportNonRedacted.zip
head cnpg-ca-secret.yaml
```

```yaml
data:
  ca.crt: LS0tLS1CRUdJTiBD…
  ca.key: LS0tLS1CRUdJTiBF…
metadata:
  creationTimestamp: "2022-03-22T10:42:28Z"
  managedFields:
  - apiVersion: v1
    fieldsType: FieldsV1
```

#### Report cluster

The `cluster` subcommand gathers the following:

* **Cluster resources** – The cluster information, same as `kubectl get cluster -o yaml`.
* **Cluster pods** – Pods in the cluster namespace matching the cluster name.
* **Cluster jobs** – Jobs in the cluster namespace matching the cluster name, if any.
* **Events** – Events in the cluster namespace.
* **Pod logs** – Logs for the cluster pods in JSON-lines format. (This option is optional and off by default.)
* **Job logs** – Logs for the pods created by jobs in JSON-lines format. (This option is optional and off by default.)

The `cluster` subcommand accepts the `-f` and `-o` flags, as the `operator` does.
If the `-f` flag isn't used, a default timestamped report name is used.
The cluster information doesn't contain configuration secrets or ConfigMaps,
so the `-S` is disabled.

!!! Note
    By default, cluster logs aren't collected, but you can enable cluster
    log collection using the `--logs` flag.

Usage:

```shell
kubectl cnpg report cluster <clusterName> [flags]
```

Unlike the `operator` subcommand, for the `cluster` subcommand you
need to provide the cluster name and, very likely, the namespace, unless the cluster
is in the default one.

```shell
kubectl cnpg report cluster example -f report.zip -n example_namespace
```

Then:

```shell
unzip report.zip
```

```shell
Archive:  report.zip
   creating: report_cluster_example_<TIMESTAMP>/
   creating: report_cluster_example_<TIMESTAMP>/manifests/
  inflating: report_cluster_example_<TIMESTAMP>/manifests/cluster.yaml
  inflating: report_cluster_example_<TIMESTAMP>/manifests/cluster-pods.yaml
  inflating: report_cluster_example_<TIMESTAMP>/manifests/cluster-jobs.yaml
  inflating: report_cluster_example_<TIMESTAMP>/manifests/events.yaml
```

You can use the `--logs` flag to add the pod and job logs to the ZIP.

```shell
kubectl cnpg report cluster example -n example_namespace --logs
```

This command results in:

```shell
Successfully written report to "report_cluster_example_<TIMESTAMP>.zip" (format: "yaml")
```

```shell
unzip report_cluster_<TIMESTAMP>.zip
```

```shell
Archive:  report_cluster_example_<TIMESTAMP>.zip
   creating: report_cluster_example_<TIMESTAMP>/
   creating: report_cluster_example_<TIMESTAMP>/manifests/
  inflating: report_cluster_example_<TIMESTAMP>/manifests/cluster.yaml
  inflating: report_cluster_example_<TIMESTAMP>/manifests/cluster-pods.yaml
  inflating: report_cluster_example_<TIMESTAMP>/manifests/cluster-jobs.yaml
  inflating: report_cluster_example_<TIMESTAMP>/manifests/events.yaml
   creating: report_cluster_example_<TIMESTAMP>/logs/
  inflating: report_cluster_example_<TIMESTAMP>/logs/cluster-example-full-1.jsonl
   creating: report_cluster_example_<TIMESTAMP>/job-logs/
  inflating: report_cluster_example_<TIMESTAMP>/job-logs/cluster-example-full-1-initdb-qnnvw.jsonl
  inflating: report_cluster_example_<TIMESTAMP>/job-logs/cluster-example-full-2-join-tvj8r.jsonl
```

### Logs

The `kubectl cnpg logs` command allows you to follow the logs of a collection
of pods related to CloudNativePG in a single go.

It has one available subcommand: `cluster`.

#### Cluster logs

The `cluster` subcommand gathers all the pod logs for a cluster in a single
stream or file.
This means that you can get all the pod logs in a single terminal window, with a
single invocation of the command.

As in all the cnpg plugin subcommands, you can get instructions and help using
the `-h` flag:

`kubectl cnpg logs cluster -h`

The `logs` command displays logs in  JSON-lines format, unless the
`--timestamps` flag is used. In that case, a human-readable timestamp is
prepended to each line. The lines are no longer valid JSON,
and tools such as `jq` might not work as desired.

If the `logs cluster` subcommand is given the `-f` flag (aka `--follow`), it
follows the cluster pod logs and also watches for any new pods created
in the cluster after the command was invoked.
Any new pods found, including pods that were restarted or re-created,
also have their pods followed.
The logs are displayed in the terminal's standard-out.
This command exits when the cluster has no more pods left or when you
interrupt it.

If `logs` is called without the `-f` option, it reads the logs from all
cluster pods until the time of invocation and displays them in the terminal's
standard-out. Then it exits.

You can provide the `-o` or `--output` flag to specify the name
of the file to save the logs to instead of displaying in
standard-out.

You can use the `--tail` flag to specify how many log lines to retrieve
from each pod in the cluster. By default, the `logs cluster` subcommand
displays all the logs from each pod in the cluster. If combined with the "follow"
flag `-f`, the number of logs specified by `--tail` are retrieved until the
current time. From then on, the new logs are followed.

!!! Note
    Unlike other cnpg plugin commands, `-f` is used to denote "follow"
    rather than specify a file. This keeps with the convention of `kubectl logs`,
    which takes `-f` to mean to follow the logs.

Usage:

```shell
kubectl cnpg logs cluster <clusterName> [flags]
```

Using the `-f` option to follow:

```shell
kubectl cnpg report cluster cluster-example -f
```

Using `--tail` option to display three lines from each pod and the `-f` option
to follow:

```shell
kubectl cnpg report cluster cluster-example -f --tail 3
```

``` json
{"level":"info","ts":"2023-06-30T13:37:33Z","logger":"postgres","msg":"2023-06-30 13:37:33.142 UTC [26] LOG:  ending log output to stderr","source":"/controller/log/postgres","logging_pod":"cluster-example-3"}
{"level":"info","ts":"2023-06-30T13:37:33Z","logger":"postgres","msg":"2023-06-30 13:37:33.142 UTC [26] HINT:  Future log output will go to log destination \"csvlog\".","source":"/controller/log/postgres","logging_pod":"cluster-example-3"}
…
…
```

With the `-o` option omitted, and with `--output` specified:

``` sh
kubectl-cnpg logs cluster cluster-example --output my-cluster.log

Successfully written logs to "my-cluster.log"
```

### Destroy

The `kubectl cnpg destroy` command helps to remove an instance and all the
associated PVCs from a Kubernetes cluster.

The optional `--keep-pvc` flag allows you to keep the PVCs
while removing all `metadata.ownerReferences` that were set by the instance.
Also, the `cnpg.io/pvcStatus` label on the PVCs changes from
`ready` to `detached` to signify that they're no longer in use.

Running the command again without the `--keep-pvc` flag removes the
detached PVCs.

Usage:

```
kubectl cnpg destroy [CLUSTER_NAME] [INSTANCE_ID]
```

This example removes the `cluster-example-2` pod and the associated
PVCs:

```
kubectl cnpg destroy cluster-example 2
```

### Cluster hibernation

You might want to suspend the execution of a CloudNativePG cluster
while retaining its data and then resume its activity at a later time. This
feature is called *cluster hibernation*.

Hibernation is available only by way of the `kubectl cnpg hibernate [on|off]`
commands.

Hibernating a CloudNativePG cluster means destroying all the resources
generated by the cluster, except the PVCs that belong to the PostgreSQL primary
instance.

To hibernate a cluster:

```
kubectl cnpg hibernate on <cluster-name>
```

This command:

1. Shuts down every PostgreSQL instance.
2. Detaches the PVCs containing the data of the primary instance and annotates
   them with the latest database status and the latest cluster configuration.
3. Deletes the `Cluster` resource, including every generated resource except
   the aforementioned PVCs.

When hibernated, a CloudNativePG cluster is represented by just a group of
PVCs, in which the one containing the `PGDATA` is annotated with the latest
available status, including content from `pg_controldata`.

!!! Warning
    You can't hibernate a cluster having fenced instances, as fencing is
    part of the hibernation procedure.

If an error occurs, the operator can't revert the procedure. You can
still force the operation:

```
kubectl cnpg hibernate on cluster-example --force
```

To resume hibernating the cluster:

```
kubectl cnpg hibernate off <cluster-name>
```

Once the cluster has been hibernated, you can show the last
configuration and the status that PostgreSQL had after it was shut down:

```
kubectl cnpg hibernate status <cluster-name>
```

### Benchmarking the database with pgbench

You can run pgbench against an existing PostgreSQL cluster:

```
kubectl cnpg pgbench <cluster-name> -- --time 30 --client 1 --jobs 1
```

See [Benchmarking pgbench](benchmarking.md#pgbench) for more
details.

### Benchmarking the storage with fio

Use the following command to run fio on an existing storage class:

```
kubectl cnpg fio <fio-job-name> -n <namespace>
```

See [Benchmarking fio section](benchmarking.md#fio) for more details.

### Requesting a new physical backup

The `kubectl cnpg backup` command requests a new physical backup for
an existing Postgres cluster by creating a new `Backup` resource.

!!! Info
    From release 1.21, the `backup` command accepts a new flag, `-m`,
    to specify the backup method.
    To request a backup using volume snapshots, set `-m volumeSnapshot`.

This example requests an on-demand backup for a given cluster:

```shell
kubectl cnpg backup [cluster_name]
```

Alternatively, if using volume snapshots (from release 1.21):

```shell
kubectl cnpg backup [cluster_name] -m volumeSnapshot
```

The created backup is named using the request time:

```shell
kubectl cnpg backup cluster-example
backup/cluster-example-20230121002300 created
```

By default, a newly created backup uses the backup target policy defined
in the cluster to choose the instance to run on.
However, you can override this policy using the `--backup-target` option.

In the case of volume snapshot backups, you can also use the `--online` option
to request an online/hot backup or an offline/cold one. You can
also tune online backups by explicitly setting the `--immediate-checkpoint` and
`--wait-for-archive` options.

["Backup"](./backup.md#backup) contains more information about
the configuration settings.

### Launching psql

The `kubectl cnpg psql` command starts a new PostgreSQL interactive front-end
process (psql) connected to an existing Postgres cluster as if you were running
it from the actual pod. This means that you're using the postgres user.

!!! Important
    As you're connecting as the postgres user, in production environments use this
    method with extreme care, by authorized personnel only.

```shell
kubectl cnpg psql cluster-example

psql (16.0 (Debian 16.0-1.pgdg110+1))
Type "help" for help.

postgres=#
```

By default, the command connects to the primary instance. You can
specify to work against a replica by using the `--replica` option:

```shell
kubectl cnpg psql --replica cluster-example
psql (16.0 (Debian 16.0-1.pgdg110+1))

Type "help" for help.

postgres=# select pg_is_in_recovery();
 pg_is_in_recovery
-------------------
 t
(1 row)

postgres=# \q
```

This command starts `kubectl exec`. The `kubectl` executable must be
reachable in your `PATH` variable to work correctly.

### Snapshotting a Postgres cluster

!!! Warning
    The `kubectl cnpg snapshot` command was removed.
    Use the [`backup` command](#requesting-a-new-backup) to request
    backups using volume snapshots.
