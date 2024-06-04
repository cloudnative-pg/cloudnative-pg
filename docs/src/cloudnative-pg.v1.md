# API Reference

<p>Package v1 contains API Schema definitions for the postgresql v1 API group</p>


## Resource Types


- [Backup](#postgresql-cnpg-io-v1-Backup)
- [Cluster](#postgresql-cnpg-io-v1-Cluster)
- [ClusterImageCatalog](#postgresql-cnpg-io-v1-ClusterImageCatalog)
- [ImageCatalog](#postgresql-cnpg-io-v1-ImageCatalog)
- [Pooler](#postgresql-cnpg-io-v1-Pooler)
- [ScheduledBackup](#postgresql-cnpg-io-v1-ScheduledBackup)

## Backup     {#postgresql-cnpg-io-v1-Backup}



<p>Backup is the Schema for the backups API</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>apiVersion</code> <B>[Required]</B><br/>string</td><td><code>postgresql.cnpg.io/v1</code></td></tr>
<tr><td><code>kind</code> <B>[Required]</B><br/>string</td><td><code>Backup</code></td></tr>
<tr><td><code>metadata</code> <B>[Required]</B><br/>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#objectmeta-v1-meta"><i>meta/v1.ObjectMeta</i></a>
</td>
<td>
   <span class="text-muted">No description provided.</span>Refer to the Kubernetes API documentation for the fields of the <code>metadata</code> field.</td>
</tr>
<tr><td><code>spec</code> <B>[Required]</B><br/>
<a href="#postgresql-cnpg-io-v1-BackupSpec"><i>BackupSpec</i></a>
</td>
<td>
   <p>Specification of the desired behavior of the backup.
More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status</p>
</td>
</tr>
<tr><td><code>status</code><br/>
<a href="#postgresql-cnpg-io-v1-BackupStatus"><i>BackupStatus</i></a>
</td>
<td>
   <p>Most recently observed status of the backup. This data may not be up to
date. Populated by the system. Read-only.
More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status</p>
</td>
</tr>
</tbody>
</table>

## Cluster     {#postgresql-cnpg-io-v1-Cluster}



<p>Cluster is the Schema for the PostgreSQL API</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>apiVersion</code> <B>[Required]</B><br/>string</td><td><code>postgresql.cnpg.io/v1</code></td></tr>
<tr><td><code>kind</code> <B>[Required]</B><br/>string</td><td><code>Cluster</code></td></tr>
<tr><td><code>metadata</code> <B>[Required]</B><br/>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#objectmeta-v1-meta"><i>meta/v1.ObjectMeta</i></a>
</td>
<td>
   <span class="text-muted">No description provided.</span>Refer to the Kubernetes API documentation for the fields of the <code>metadata</code> field.</td>
</tr>
<tr><td><code>spec</code> <B>[Required]</B><br/>
<a href="#postgresql-cnpg-io-v1-ClusterSpec"><i>ClusterSpec</i></a>
</td>
<td>
   <p>Specification of the desired behavior of the cluster.
More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status</p>
</td>
</tr>
<tr><td><code>status</code><br/>
<a href="#postgresql-cnpg-io-v1-ClusterStatus"><i>ClusterStatus</i></a>
</td>
<td>
   <p>Most recently observed status of the cluster. This data may not be up
to date. Populated by the system. Read-only.
More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status</p>
</td>
</tr>
</tbody>
</table>

## ClusterImageCatalog     {#postgresql-cnpg-io-v1-ClusterImageCatalog}



<p>ClusterImageCatalog is the Schema for the clusterimagecatalogs API</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>apiVersion</code> <B>[Required]</B><br/>string</td><td><code>postgresql.cnpg.io/v1</code></td></tr>
<tr><td><code>kind</code> <B>[Required]</B><br/>string</td><td><code>ClusterImageCatalog</code></td></tr>
<tr><td><code>metadata</code> <B>[Required]</B><br/>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#objectmeta-v1-meta"><i>meta/v1.ObjectMeta</i></a>
</td>
<td>
   <span class="text-muted">No description provided.</span>Refer to the Kubernetes API documentation for the fields of the <code>metadata</code> field.</td>
</tr>
<tr><td><code>spec</code> <B>[Required]</B><br/>
<a href="#postgresql-cnpg-io-v1-ImageCatalogSpec"><i>ImageCatalogSpec</i></a>
</td>
<td>
   <p>Specification of the desired behavior of the ClusterImageCatalog.
More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status</p>
</td>
</tr>
</tbody>
</table>

## ImageCatalog     {#postgresql-cnpg-io-v1-ImageCatalog}



<p>ImageCatalog is the Schema for the imagecatalogs API</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>apiVersion</code> <B>[Required]</B><br/>string</td><td><code>postgresql.cnpg.io/v1</code></td></tr>
<tr><td><code>kind</code> <B>[Required]</B><br/>string</td><td><code>ImageCatalog</code></td></tr>
<tr><td><code>metadata</code> <B>[Required]</B><br/>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#objectmeta-v1-meta"><i>meta/v1.ObjectMeta</i></a>
</td>
<td>
   <span class="text-muted">No description provided.</span>Refer to the Kubernetes API documentation for the fields of the <code>metadata</code> field.</td>
</tr>
<tr><td><code>spec</code> <B>[Required]</B><br/>
<a href="#postgresql-cnpg-io-v1-ImageCatalogSpec"><i>ImageCatalogSpec</i></a>
</td>
<td>
   <p>Specification of the desired behavior of the ImageCatalog.
More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status</p>
</td>
</tr>
</tbody>
</table>

## Pooler     {#postgresql-cnpg-io-v1-Pooler}



<p>Pooler is the Schema for the poolers API</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>apiVersion</code> <B>[Required]</B><br/>string</td><td><code>postgresql.cnpg.io/v1</code></td></tr>
<tr><td><code>kind</code> <B>[Required]</B><br/>string</td><td><code>Pooler</code></td></tr>
<tr><td><code>metadata</code> <B>[Required]</B><br/>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#objectmeta-v1-meta"><i>meta/v1.ObjectMeta</i></a>
</td>
<td>
   <span class="text-muted">No description provided.</span>Refer to the Kubernetes API documentation for the fields of the <code>metadata</code> field.</td>
</tr>
<tr><td><code>spec</code> <B>[Required]</B><br/>
<a href="#postgresql-cnpg-io-v1-PoolerSpec"><i>PoolerSpec</i></a>
</td>
<td>
   <p>Specification of the desired behavior of the Pooler.
More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status</p>
</td>
</tr>
<tr><td><code>status</code><br/>
<a href="#postgresql-cnpg-io-v1-PoolerStatus"><i>PoolerStatus</i></a>
</td>
<td>
   <p>Most recently observed status of the Pooler. This data may not be up to
date. Populated by the system. Read-only.
More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status</p>
</td>
</tr>
</tbody>
</table>

## ScheduledBackup     {#postgresql-cnpg-io-v1-ScheduledBackup}



<p>ScheduledBackup is the Schema for the scheduledbackups API</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>apiVersion</code> <B>[Required]</B><br/>string</td><td><code>postgresql.cnpg.io/v1</code></td></tr>
<tr><td><code>kind</code> <B>[Required]</B><br/>string</td><td><code>ScheduledBackup</code></td></tr>
<tr><td><code>metadata</code> <B>[Required]</B><br/>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#objectmeta-v1-meta"><i>meta/v1.ObjectMeta</i></a>
</td>
<td>
   <span class="text-muted">No description provided.</span>Refer to the Kubernetes API documentation for the fields of the <code>metadata</code> field.</td>
</tr>
<tr><td><code>spec</code> <B>[Required]</B><br/>
<a href="#postgresql-cnpg-io-v1-ScheduledBackupSpec"><i>ScheduledBackupSpec</i></a>
</td>
<td>
   <p>Specification of the desired behavior of the ScheduledBackup.
More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status</p>
</td>
</tr>
<tr><td><code>status</code><br/>
<a href="#postgresql-cnpg-io-v1-ScheduledBackupStatus"><i>ScheduledBackupStatus</i></a>
</td>
<td>
   <p>Most recently observed status of the ScheduledBackup. This data may not be up
to date. Populated by the system. Read-only.
More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status</p>
</td>
</tr>
</tbody>
</table>

## AffinityConfiguration     {#postgresql-cnpg-io-v1-AffinityConfiguration}


**Appears in:**

- [ClusterSpec](#postgresql-cnpg-io-v1-ClusterSpec)


<p>AffinityConfiguration contains the info we need to create the
affinity rules for Pods</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>enablePodAntiAffinity</code><br/>
<i>bool</i>
</td>
<td>
   <p>Activates anti-affinity for the pods. The operator will define pods
anti-affinity unless this field is explicitly set to false</p>
</td>
</tr>
<tr><td><code>topologyKey</code><br/>
<i>string</i>
</td>
<td>
   <p>TopologyKey to use for anti-affinity configuration. See k8s documentation
for more info on that</p>
</td>
</tr>
<tr><td><code>nodeSelector</code><br/>
<i>map[string]string</i>
</td>
<td>
   <p>NodeSelector is map of key-value pairs used to define the nodes on which
the pods can run.
More info: https://kubernetes.io/docs/concepts/configuration/assign-pod-node/</p>
</td>
</tr>
<tr><td><code>nodeAffinity</code><br/>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#nodeaffinity-v1-core"><i>core/v1.NodeAffinity</i></a>
</td>
<td>
   <p>NodeAffinity describes node affinity scheduling rules for the pod.
More info: https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/#node-affinity</p>
</td>
</tr>
<tr><td><code>tolerations</code><br/>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#toleration-v1-core"><i>[]core/v1.Toleration</i></a>
</td>
<td>
   <p>Tolerations is a list of Tolerations that should be set for all the pods, in order to allow them to run
on tainted nodes.
More info: https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration/</p>
</td>
</tr>
<tr><td><code>podAntiAffinityType</code><br/>
<i>string</i>
</td>
<td>
   <p>PodAntiAffinityType allows the user to decide whether pod anti-affinity between cluster instance has to be
considered a strong requirement during scheduling or not. Allowed values are: &quot;preferred&quot; (default if empty) or
&quot;required&quot;. Setting it to &quot;required&quot;, could lead to instances remaining pending until new kubernetes nodes are
added if all the existing nodes don't match the required pod anti-affinity rule.
More info:
https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/#inter-pod-affinity-and-anti-affinity</p>
</td>
</tr>
<tr><td><code>additionalPodAntiAffinity</code><br/>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#podantiaffinity-v1-core"><i>core/v1.PodAntiAffinity</i></a>
</td>
<td>
   <p>AdditionalPodAntiAffinity allows to specify pod anti-affinity terms to be added to the ones generated
by the operator if EnablePodAntiAffinity is set to true (default) or to be used exclusively if set to false.</p>
</td>
</tr>
<tr><td><code>additionalPodAffinity</code><br/>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#podaffinity-v1-core"><i>core/v1.PodAffinity</i></a>
</td>
<td>
   <p>AdditionalPodAffinity allows to specify pod affinity terms to be passed to all the cluster's pods.</p>
</td>
</tr>
</tbody>
</table>

## AvailableArchitecture     {#postgresql-cnpg-io-v1-AvailableArchitecture}


**Appears in:**

- [ClusterStatus](#postgresql-cnpg-io-v1-ClusterStatus)


<p>AvailableArchitecture represents the state of a cluster's architecture</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>goArch</code> <B>[Required]</B><br/>
<i>string</i>
</td>
<td>
   <p>GoArch is the name of the executable architecture</p>
</td>
</tr>
<tr><td><code>hash</code> <B>[Required]</B><br/>
<i>string</i>
</td>
<td>
   <p>Hash is the hash of the executable</p>
</td>
</tr>
</tbody>
</table>

## AzureCredentials     {#postgresql-cnpg-io-v1-AzureCredentials}


**Appears in:**

- [BarmanCredentials](#postgresql-cnpg-io-v1-BarmanCredentials)


<p>AzureCredentials is the type for the credentials to be used to upload
files to Azure Blob Storage. The connection string contains every needed
information. If the connection string is not specified, we'll need the
storage account name and also one (and only one) of:</p>
<ul>
<li>
<p>storageKey</p>
</li>
<li>
<p>storageSasToken</p>
</li>
<li>
<p>inheriting the credentials from the pod environment by setting inheritFromAzureAD to true</p>
</li>
</ul>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>connectionString</code><br/>
<a href="#postgresql-cnpg-io-v1-SecretKeySelector"><i>SecretKeySelector</i></a>
</td>
<td>
   <p>The connection string to be used</p>
</td>
</tr>
<tr><td><code>storageAccount</code><br/>
<a href="#postgresql-cnpg-io-v1-SecretKeySelector"><i>SecretKeySelector</i></a>
</td>
<td>
   <p>The storage account where to upload data</p>
</td>
</tr>
<tr><td><code>storageKey</code><br/>
<a href="#postgresql-cnpg-io-v1-SecretKeySelector"><i>SecretKeySelector</i></a>
</td>
<td>
   <p>The storage account key to be used in conjunction
with the storage account name</p>
</td>
</tr>
<tr><td><code>storageSasToken</code><br/>
<a href="#postgresql-cnpg-io-v1-SecretKeySelector"><i>SecretKeySelector</i></a>
</td>
<td>
   <p>A shared-access-signature to be used in conjunction with
the storage account name</p>
</td>
</tr>
<tr><td><code>inheritFromAzureAD</code><br/>
<i>bool</i>
</td>
<td>
   <p>Use the Azure AD based authentication without providing explicitly the keys.</p>
</td>
</tr>
</tbody>
</table>

## BackupConfiguration     {#postgresql-cnpg-io-v1-BackupConfiguration}


**Appears in:**

- [ClusterSpec](#postgresql-cnpg-io-v1-ClusterSpec)


<p>BackupConfiguration defines how the backup of the cluster are taken.
The supported backup methods are BarmanObjectStore and VolumeSnapshot.
For details and examples refer to the Backup and Recovery section of the
documentation</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>volumeSnapshot</code><br/>
<a href="#postgresql-cnpg-io-v1-VolumeSnapshotConfiguration"><i>VolumeSnapshotConfiguration</i></a>
</td>
<td>
   <p>VolumeSnapshot provides the configuration for the execution of volume snapshot backups.</p>
</td>
</tr>
<tr><td><code>barmanObjectStore</code><br/>
<a href="#postgresql-cnpg-io-v1-BarmanObjectStoreConfiguration"><i>BarmanObjectStoreConfiguration</i></a>
</td>
<td>
   <p>The configuration for the barman-cloud tool suite</p>
</td>
</tr>
<tr><td><code>retentionPolicy</code><br/>
<i>string</i>
</td>
<td>
   <p>RetentionPolicy is the retention policy to be used for backups
and WALs (i.e. '60d'). The retention policy is expressed in the form
of <code>XXu</code> where <code>XX</code> is a positive integer and <code>u</code> is in <code>[dwm]</code> -
days, weeks, months.
It's currently only applicable when using the BarmanObjectStore method.</p>
</td>
</tr>
<tr><td><code>target</code><br/>
<a href="#postgresql-cnpg-io-v1-BackupTarget"><i>BackupTarget</i></a>
</td>
<td>
   <p>The policy to decide which instance should perform backups. Available
options are empty string, which will default to <code>prefer-standby</code> policy,
<code>primary</code> to have backups run always on primary instances, <code>prefer-standby</code>
to have backups run preferably on the most updated standby, if available.</p>
</td>
</tr>
</tbody>
</table>

## BackupMethod     {#postgresql-cnpg-io-v1-BackupMethod}

(Alias of `string`)

**Appears in:**

- [BackupSpec](#postgresql-cnpg-io-v1-BackupSpec)

- [BackupStatus](#postgresql-cnpg-io-v1-BackupStatus)

- [ScheduledBackupSpec](#postgresql-cnpg-io-v1-ScheduledBackupSpec)


<p>BackupMethod defines the way of executing the physical base backups of
the selected PostgreSQL instance</p>




## BackupPhase     {#postgresql-cnpg-io-v1-BackupPhase}

(Alias of `string`)

**Appears in:**

- [BackupStatus](#postgresql-cnpg-io-v1-BackupStatus)


<p>BackupPhase is the phase of the backup</p>




## BackupPluginConfiguration     {#postgresql-cnpg-io-v1-BackupPluginConfiguration}


**Appears in:**

- [BackupSpec](#postgresql-cnpg-io-v1-BackupSpec)

- [ScheduledBackupSpec](#postgresql-cnpg-io-v1-ScheduledBackupSpec)


<p>BackupPluginConfiguration contains the backup configuration used by
the backup plugin</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>name</code> <B>[Required]</B><br/>
<i>string</i>
</td>
<td>
   <p>Name is the name of the plugin managing this backup</p>
</td>
</tr>
<tr><td><code>parameters</code><br/>
<i>map[string]string</i>
</td>
<td>
   <p>Parameters are the configuration parameters passed to the backup
plugin for this backup</p>
</td>
</tr>
</tbody>
</table>

## BackupSnapshotElementStatus     {#postgresql-cnpg-io-v1-BackupSnapshotElementStatus}


**Appears in:**

- [BackupSnapshotStatus](#postgresql-cnpg-io-v1-BackupSnapshotStatus)


<p>BackupSnapshotElementStatus is a volume snapshot that is part of a volume snapshot method backup</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>name</code> <B>[Required]</B><br/>
<i>string</i>
</td>
<td>
   <p>Name is the snapshot resource name</p>
</td>
</tr>
<tr><td><code>type</code> <B>[Required]</B><br/>
<i>string</i>
</td>
<td>
   <p>Type is tho role of the snapshot in the cluster, such as PG_DATA, PG_WAL and PG_TABLESPACE</p>
</td>
</tr>
<tr><td><code>tablespaceName</code> <B>[Required]</B><br/>
<i>string</i>
</td>
<td>
   <p>TablespaceName is the name of the snapshotted tablespace. Only set
when type is PG_TABLESPACE</p>
</td>
</tr>
</tbody>
</table>

## BackupSnapshotStatus     {#postgresql-cnpg-io-v1-BackupSnapshotStatus}


**Appears in:**

- [BackupStatus](#postgresql-cnpg-io-v1-BackupStatus)


<p>BackupSnapshotStatus the fields exclusive to the volumeSnapshot method backup</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>elements</code><br/>
<a href="#postgresql-cnpg-io-v1-BackupSnapshotElementStatus"><i>[]BackupSnapshotElementStatus</i></a>
</td>
<td>
   <p>The elements list, populated with the gathered volume snapshots</p>
</td>
</tr>
</tbody>
</table>

## BackupSource     {#postgresql-cnpg-io-v1-BackupSource}


**Appears in:**

- [BootstrapRecovery](#postgresql-cnpg-io-v1-BootstrapRecovery)


<p>BackupSource contains the backup we need to restore from, plus some
information that could be needed to correctly restore it.</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>LocalObjectReference</code><br/>
<a href="#postgresql-cnpg-io-v1-LocalObjectReference"><i>LocalObjectReference</i></a>
</td>
<td>(Members of <code>LocalObjectReference</code> are embedded into this type.)
   <span class="text-muted">No description provided.</span></td>
</tr>
<tr><td><code>endpointCA</code><br/>
<a href="#postgresql-cnpg-io-v1-SecretKeySelector"><i>SecretKeySelector</i></a>
</td>
<td>
   <p>EndpointCA store the CA bundle of the barman endpoint.
Useful when using self-signed certificates to avoid
errors with certificate issuer and barman-cloud-wal-archive.</p>
</td>
</tr>
</tbody>
</table>

## BackupSpec     {#postgresql-cnpg-io-v1-BackupSpec}


**Appears in:**

- [Backup](#postgresql-cnpg-io-v1-Backup)


<p>BackupSpec defines the desired state of Backup</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>cluster</code> <B>[Required]</B><br/>
<a href="#postgresql-cnpg-io-v1-LocalObjectReference"><i>LocalObjectReference</i></a>
</td>
<td>
   <p>The cluster to backup</p>
</td>
</tr>
<tr><td><code>target</code><br/>
<a href="#postgresql-cnpg-io-v1-BackupTarget"><i>BackupTarget</i></a>
</td>
<td>
   <p>The policy to decide which instance should perform this backup. If empty,
it defaults to <code>cluster.spec.backup.target</code>.
Available options are empty string, <code>primary</code> and <code>prefer-standby</code>.
<code>primary</code> to have backups run always on primary instances,
<code>prefer-standby</code> to have backups run preferably on the most updated
standby, if available.</p>
</td>
</tr>
<tr><td><code>method</code><br/>
<a href="#postgresql-cnpg-io-v1-BackupMethod"><i>BackupMethod</i></a>
</td>
<td>
   <p>The backup method to be used, possible options are <code>barmanObjectStore</code>,
<code>volumeSnapshot</code> or <code>plugin</code>. Defaults to: <code>barmanObjectStore</code>.</p>
</td>
</tr>
<tr><td><code>pluginConfiguration</code><br/>
<a href="#postgresql-cnpg-io-v1-BackupPluginConfiguration"><i>BackupPluginConfiguration</i></a>
</td>
<td>
   <p>Configuration parameters passed to the plugin managing this backup</p>
</td>
</tr>
<tr><td><code>online</code><br/>
<i>bool</i>
</td>
<td>
   <p>Whether the default type of backup with volume snapshots is
online/hot (<code>true</code>, default) or offline/cold (<code>false</code>)
Overrides the default setting specified in the cluster field '.spec.backup.volumeSnapshot.online'</p>
</td>
</tr>
<tr><td><code>onlineConfiguration</code><br/>
<a href="#postgresql-cnpg-io-v1-OnlineConfiguration"><i>OnlineConfiguration</i></a>
</td>
<td>
   <p>Configuration parameters to control the online/hot backup with volume snapshots
Overrides the default settings specified in the cluster '.backup.volumeSnapshot.onlineConfiguration' stanza</p>
</td>
</tr>
</tbody>
</table>

## BackupStatus     {#postgresql-cnpg-io-v1-BackupStatus}


**Appears in:**

- [Backup](#postgresql-cnpg-io-v1-Backup)


<p>BackupStatus defines the observed state of Backup</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>BarmanCredentials</code><br/>
<a href="#postgresql-cnpg-io-v1-BarmanCredentials"><i>BarmanCredentials</i></a>
</td>
<td>(Members of <code>BarmanCredentials</code> are embedded into this type.)
   <p>The potential credentials for each cloud provider</p>
</td>
</tr>
<tr><td><code>endpointCA</code><br/>
<a href="#postgresql-cnpg-io-v1-SecretKeySelector"><i>SecretKeySelector</i></a>
</td>
<td>
   <p>EndpointCA store the CA bundle of the barman endpoint.
Useful when using self-signed certificates to avoid
errors with certificate issuer and barman-cloud-wal-archive.</p>
</td>
</tr>
<tr><td><code>endpointURL</code><br/>
<i>string</i>
</td>
<td>
   <p>Endpoint to be used to upload data to the cloud,
overriding the automatic endpoint discovery</p>
</td>
</tr>
<tr><td><code>destinationPath</code><br/>
<i>string</i>
</td>
<td>
   <p>The path where to store the backup (i.e. s3://bucket/path/to/folder)
this path, with different destination folders, will be used for WALs
and for data. This may not be populated in case of errors.</p>
</td>
</tr>
<tr><td><code>serverName</code><br/>
<i>string</i>
</td>
<td>
   <p>The server name on S3, the cluster name is used if this
parameter is omitted</p>
</td>
</tr>
<tr><td><code>encryption</code><br/>
<i>string</i>
</td>
<td>
   <p>Encryption method required to S3 API</p>
</td>
</tr>
<tr><td><code>backupId</code><br/>
<i>string</i>
</td>
<td>
   <p>The ID of the Barman backup</p>
</td>
</tr>
<tr><td><code>backupName</code><br/>
<i>string</i>
</td>
<td>
   <p>The Name of the Barman backup</p>
</td>
</tr>
<tr><td><code>phase</code><br/>
<a href="#postgresql-cnpg-io-v1-BackupPhase"><i>BackupPhase</i></a>
</td>
<td>
   <p>The last backup status</p>
</td>
</tr>
<tr><td><code>startedAt</code><br/>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#time-v1-meta"><i>meta/v1.Time</i></a>
</td>
<td>
   <p>When the backup was started</p>
</td>
</tr>
<tr><td><code>stoppedAt</code><br/>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#time-v1-meta"><i>meta/v1.Time</i></a>
</td>
<td>
   <p>When the backup was terminated</p>
</td>
</tr>
<tr><td><code>beginWal</code><br/>
<i>string</i>
</td>
<td>
   <p>The starting WAL</p>
</td>
</tr>
<tr><td><code>endWal</code><br/>
<i>string</i>
</td>
<td>
   <p>The ending WAL</p>
</td>
</tr>
<tr><td><code>beginLSN</code><br/>
<i>string</i>
</td>
<td>
   <p>The starting xlog</p>
</td>
</tr>
<tr><td><code>endLSN</code><br/>
<i>string</i>
</td>
<td>
   <p>The ending xlog</p>
</td>
</tr>
<tr><td><code>error</code><br/>
<i>string</i>
</td>
<td>
   <p>The detected error</p>
</td>
</tr>
<tr><td><code>commandOutput</code><br/>
<i>string</i>
</td>
<td>
   <p>Unused. Retained for compatibility with old versions.</p>
</td>
</tr>
<tr><td><code>commandError</code><br/>
<i>string</i>
</td>
<td>
   <p>The backup command output in case of error</p>
</td>
</tr>
<tr><td><code>backupLabelFile</code><br/>
<i>[]byte</i>
</td>
<td>
   <p>Backup label file content as returned by Postgres in case of online (hot) backups</p>
</td>
</tr>
<tr><td><code>tablespaceMapFile</code><br/>
<i>[]byte</i>
</td>
<td>
   <p>Tablespace map file content as returned by Postgres in case of online (hot) backups</p>
</td>
</tr>
<tr><td><code>instanceID</code><br/>
<a href="#postgresql-cnpg-io-v1-InstanceID"><i>InstanceID</i></a>
</td>
<td>
   <p>Information to identify the instance where the backup has been taken from</p>
</td>
</tr>
<tr><td><code>snapshotBackupStatus</code><br/>
<a href="#postgresql-cnpg-io-v1-BackupSnapshotStatus"><i>BackupSnapshotStatus</i></a>
</td>
<td>
   <p>Status of the volumeSnapshot backup</p>
</td>
</tr>
<tr><td><code>method</code><br/>
<a href="#postgresql-cnpg-io-v1-BackupMethod"><i>BackupMethod</i></a>
</td>
<td>
   <p>The backup method being used</p>
</td>
</tr>
<tr><td><code>online</code> <B>[Required]</B><br/>
<i>bool</i>
</td>
<td>
   <p>Whether the backup was online/hot (<code>true</code>) or offline/cold (<code>false</code>)</p>
</td>
</tr>
</tbody>
</table>

## BackupTarget     {#postgresql-cnpg-io-v1-BackupTarget}

(Alias of `string`)

**Appears in:**

- [BackupConfiguration](#postgresql-cnpg-io-v1-BackupConfiguration)

- [BackupSpec](#postgresql-cnpg-io-v1-BackupSpec)

- [ScheduledBackupSpec](#postgresql-cnpg-io-v1-ScheduledBackupSpec)


<p>BackupTarget describes the preferred targets for a backup</p>




## BarmanCredentials     {#postgresql-cnpg-io-v1-BarmanCredentials}


**Appears in:**

- [BackupStatus](#postgresql-cnpg-io-v1-BackupStatus)

- [BarmanObjectStoreConfiguration](#postgresql-cnpg-io-v1-BarmanObjectStoreConfiguration)


<p>BarmanCredentials an object containing the potential credentials for each cloud provider</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>googleCredentials</code><br/>
<a href="#postgresql-cnpg-io-v1-GoogleCredentials"><i>GoogleCredentials</i></a>
</td>
<td>
   <p>The credentials to use to upload data to Google Cloud Storage</p>
</td>
</tr>
<tr><td><code>s3Credentials</code><br/>
<a href="#postgresql-cnpg-io-v1-S3Credentials"><i>S3Credentials</i></a>
</td>
<td>
   <p>The credentials to use to upload data to S3</p>
</td>
</tr>
<tr><td><code>azureCredentials</code><br/>
<a href="#postgresql-cnpg-io-v1-AzureCredentials"><i>AzureCredentials</i></a>
</td>
<td>
   <p>The credentials to use to upload data to Azure Blob Storage</p>
</td>
</tr>
</tbody>
</table>

## BarmanObjectStoreConfiguration     {#postgresql-cnpg-io-v1-BarmanObjectStoreConfiguration}


**Appears in:**

- [BackupConfiguration](#postgresql-cnpg-io-v1-BackupConfiguration)

- [ExternalCluster](#postgresql-cnpg-io-v1-ExternalCluster)


<p>BarmanObjectStoreConfiguration contains the backup configuration
using Barman against an S3-compatible object storage</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>BarmanCredentials</code><br/>
<a href="#postgresql-cnpg-io-v1-BarmanCredentials"><i>BarmanCredentials</i></a>
</td>
<td>(Members of <code>BarmanCredentials</code> are embedded into this type.)
   <p>The potential credentials for each cloud provider</p>
</td>
</tr>
<tr><td><code>endpointURL</code><br/>
<i>string</i>
</td>
<td>
   <p>Endpoint to be used to upload data to the cloud,
overriding the automatic endpoint discovery</p>
</td>
</tr>
<tr><td><code>endpointCA</code><br/>
<a href="#postgresql-cnpg-io-v1-SecretKeySelector"><i>SecretKeySelector</i></a>
</td>
<td>
   <p>EndpointCA store the CA bundle of the barman endpoint.
Useful when using self-signed certificates to avoid
errors with certificate issuer and barman-cloud-wal-archive</p>
</td>
</tr>
<tr><td><code>destinationPath</code> <B>[Required]</B><br/>
<i>string</i>
</td>
<td>
   <p>The path where to store the backup (i.e. s3://bucket/path/to/folder)
this path, with different destination folders, will be used for WALs
and for data</p>
</td>
</tr>
<tr><td><code>serverName</code><br/>
<i>string</i>
</td>
<td>
   <p>The server name on S3, the cluster name is used if this
parameter is omitted</p>
</td>
</tr>
<tr><td><code>wal</code><br/>
<a href="#postgresql-cnpg-io-v1-WalBackupConfiguration"><i>WalBackupConfiguration</i></a>
</td>
<td>
   <p>The configuration for the backup of the WAL stream.
When not defined, WAL files will be stored uncompressed and may be
unencrypted in the object store, according to the bucket default policy.</p>
</td>
</tr>
<tr><td><code>data</code><br/>
<a href="#postgresql-cnpg-io-v1-DataBackupConfiguration"><i>DataBackupConfiguration</i></a>
</td>
<td>
   <p>The configuration to be used to backup the data files
When not defined, base backups files will be stored uncompressed and may
be unencrypted in the object store, according to the bucket default
policy.</p>
</td>
</tr>
<tr><td><code>tags</code><br/>
<i>map[string]string</i>
</td>
<td>
   <p>Tags is a list of key value pairs that will be passed to the
Barman --tags option.</p>
</td>
</tr>
<tr><td><code>historyTags</code><br/>
<i>map[string]string</i>
</td>
<td>
   <p>HistoryTags is a list of key value pairs that will be passed to the
Barman --history-tags option.</p>
</td>
</tr>
</tbody>
</table>

## BootstrapConfiguration     {#postgresql-cnpg-io-v1-BootstrapConfiguration}


**Appears in:**

- [ClusterSpec](#postgresql-cnpg-io-v1-ClusterSpec)


<p>BootstrapConfiguration contains information about how to create the PostgreSQL
cluster. Only a single bootstrap method can be defined among the supported
ones. <code>initdb</code> will be used as the bootstrap method if left
unspecified. Refer to the Bootstrap page of the documentation for more
information.</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>initdb</code><br/>
<a href="#postgresql-cnpg-io-v1-BootstrapInitDB"><i>BootstrapInitDB</i></a>
</td>
<td>
   <p>Bootstrap the cluster via initdb</p>
</td>
</tr>
<tr><td><code>recovery</code><br/>
<a href="#postgresql-cnpg-io-v1-BootstrapRecovery"><i>BootstrapRecovery</i></a>
</td>
<td>
   <p>Bootstrap the cluster from a backup</p>
</td>
</tr>
<tr><td><code>pg_basebackup</code><br/>
<a href="#postgresql-cnpg-io-v1-BootstrapPgBaseBackup"><i>BootstrapPgBaseBackup</i></a>
</td>
<td>
   <p>Bootstrap the cluster taking a physical backup of another compatible
PostgreSQL instance</p>
</td>
</tr>
</tbody>
</table>

## BootstrapInitDB     {#postgresql-cnpg-io-v1-BootstrapInitDB}


**Appears in:**

- [BootstrapConfiguration](#postgresql-cnpg-io-v1-BootstrapConfiguration)


<p>BootstrapInitDB is the configuration of the bootstrap process when
initdb is used
Refer to the Bootstrap page of the documentation for more information.</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>database</code><br/>
<i>string</i>
</td>
<td>
   <p>Name of the database used by the application. Default: <code>app</code>.</p>
</td>
</tr>
<tr><td><code>owner</code><br/>
<i>string</i>
</td>
<td>
   <p>Name of the owner of the database in the instance to be used
by applications. Defaults to the value of the <code>database</code> key.</p>
</td>
</tr>
<tr><td><code>secret</code><br/>
<a href="#postgresql-cnpg-io-v1-LocalObjectReference"><i>LocalObjectReference</i></a>
</td>
<td>
   <p>Name of the secret containing the initial credentials for the
owner of the user database. If empty a new secret will be
created from scratch</p>
</td>
</tr>
<tr><td><code>options</code><br/>
<i>[]string</i>
</td>
<td>
   <p>The list of options that must be passed to initdb when creating the cluster.
Deprecated: This could lead to inconsistent configurations,
please use the explicit provided parameters instead.
If defined, explicit values will be ignored.</p>
</td>
</tr>
<tr><td><code>dataChecksums</code><br/>
<i>bool</i>
</td>
<td>
   <p>Whether the <code>-k</code> option should be passed to initdb,
enabling checksums on data pages (default: <code>false</code>)</p>
</td>
</tr>
<tr><td><code>encoding</code><br/>
<i>string</i>
</td>
<td>
   <p>The value to be passed as option <code>--encoding</code> for initdb (default:<code>UTF8</code>)</p>
</td>
</tr>
<tr><td><code>localeCollate</code><br/>
<i>string</i>
</td>
<td>
   <p>The value to be passed as option <code>--lc-collate</code> for initdb (default:<code>C</code>)</p>
</td>
</tr>
<tr><td><code>localeCType</code><br/>
<i>string</i>
</td>
<td>
   <p>The value to be passed as option <code>--lc-ctype</code> for initdb (default:<code>C</code>)</p>
</td>
</tr>
<tr><td><code>walSegmentSize</code><br/>
<i>int</i>
</td>
<td>
   <p>The value in megabytes (1 to 1024) to be passed to the <code>--wal-segsize</code>
option for initdb (default: empty, resulting in PostgreSQL default: 16MB)</p>
</td>
</tr>
<tr><td><code>postInitSQL</code><br/>
<i>[]string</i>
</td>
<td>
   <p>List of SQL queries to be executed as a superuser immediately
after the cluster has been created - to be used with extreme care
(by default empty)</p>
</td>
</tr>
<tr><td><code>postInitApplicationSQL</code><br/>
<i>[]string</i>
</td>
<td>
   <p>List of SQL queries to be executed as a superuser in the application
database right after is created - to be used with extreme care
(by default empty)</p>
</td>
</tr>
<tr><td><code>postInitTemplateSQL</code><br/>
<i>[]string</i>
</td>
<td>
   <p>List of SQL queries to be executed as a superuser in the <code>template1</code>
after the cluster has been created - to be used with extreme care
(by default empty)</p>
</td>
</tr>
<tr><td><code>import</code><br/>
<a href="#postgresql-cnpg-io-v1-Import"><i>Import</i></a>
</td>
<td>
   <p>Bootstraps the new cluster by importing data from an existing PostgreSQL
instance using logical backup (<code>pg_dump</code> and <code>pg_restore</code>)</p>
</td>
</tr>
<tr><td><code>postInitApplicationSQLRefs</code><br/>
<a href="#postgresql-cnpg-io-v1-PostInitApplicationSQLRefs"><i>PostInitApplicationSQLRefs</i></a>
</td>
<td>
   <p>PostInitApplicationSQLRefs points references to ConfigMaps or Secrets which
contain SQL files, the general implementation order to these references is
from all Secrets to all ConfigMaps, and inside Secrets or ConfigMaps,
the implementation order is same as the order of each array
(by default empty)</p>
</td>
</tr>
</tbody>
</table>

## BootstrapPgBaseBackup     {#postgresql-cnpg-io-v1-BootstrapPgBaseBackup}


**Appears in:**

- [BootstrapConfiguration](#postgresql-cnpg-io-v1-BootstrapConfiguration)


<p>BootstrapPgBaseBackup contains the configuration required to take
a physical backup of an existing PostgreSQL cluster</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>source</code> <B>[Required]</B><br/>
<i>string</i>
</td>
<td>
   <p>The name of the server of which we need to take a physical backup</p>
</td>
</tr>
<tr><td><code>database</code><br/>
<i>string</i>
</td>
<td>
   <p>Name of the database used by the application. Default: <code>app</code>.</p>
</td>
</tr>
<tr><td><code>owner</code><br/>
<i>string</i>
</td>
<td>
   <p>Name of the owner of the database in the instance to be used
by applications. Defaults to the value of the <code>database</code> key.</p>
</td>
</tr>
<tr><td><code>secret</code><br/>
<a href="#postgresql-cnpg-io-v1-LocalObjectReference"><i>LocalObjectReference</i></a>
</td>
<td>
   <p>Name of the secret containing the initial credentials for the
owner of the user database. If empty a new secret will be
created from scratch</p>
</td>
</tr>
</tbody>
</table>

## BootstrapRecovery     {#postgresql-cnpg-io-v1-BootstrapRecovery}


**Appears in:**

- [BootstrapConfiguration](#postgresql-cnpg-io-v1-BootstrapConfiguration)


<p>BootstrapRecovery contains the configuration required to restore
from an existing cluster using 3 methodologies: external cluster,
volume snapshots or backup objects. Full recovery and Point-In-Time
Recovery are supported.
The method can be also be used to create clusters in continuous recovery
(replica clusters), also supporting cascading replication when <code>instances</code> &gt;</p>
<ol>
<li>Once the cluster exits recovery, the password for the superuser
will be changed through the provided secret.
Refer to the Bootstrap page of the documentation for more information.</li>
</ol>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>backup</code><br/>
<a href="#postgresql-cnpg-io-v1-BackupSource"><i>BackupSource</i></a>
</td>
<td>
   <p>The backup object containing the physical base backup from which to
initiate the recovery procedure.
Mutually exclusive with <code>source</code> and <code>volumeSnapshots</code>.</p>
</td>
</tr>
<tr><td><code>source</code><br/>
<i>string</i>
</td>
<td>
   <p>The external cluster whose backup we will restore. This is also
used as the name of the folder under which the backup is stored,
so it must be set to the name of the source cluster
Mutually exclusive with <code>backup</code>.</p>
</td>
</tr>
<tr><td><code>volumeSnapshots</code><br/>
<a href="#postgresql-cnpg-io-v1-DataSource"><i>DataSource</i></a>
</td>
<td>
   <p>The static PVC data source(s) from which to initiate the
recovery procedure. Currently supporting <code>VolumeSnapshot</code>
and <code>PersistentVolumeClaim</code> resources that map an existing
PVC group, compatible with CloudNativePG, and taken with
a cold backup copy on a fenced Postgres instance (limitation
which will be removed in the future when online backup
will be implemented).
Mutually exclusive with <code>backup</code>.</p>
</td>
</tr>
<tr><td><code>recoveryTarget</code><br/>
<a href="#postgresql-cnpg-io-v1-RecoveryTarget"><i>RecoveryTarget</i></a>
</td>
<td>
   <p>By default, the recovery process applies all the available
WAL files in the archive (full recovery). However, you can also
end the recovery as soon as a consistent state is reached or
recover to a point-in-time (PITR) by specifying a <code>RecoveryTarget</code> object,
as expected by PostgreSQL (i.e., timestamp, transaction Id, LSN, ...).
More info: https://www.postgresql.org/docs/current/runtime-config-wal.html#RUNTIME-CONFIG-WAL-RECOVERY-TARGET</p>
</td>
</tr>
<tr><td><code>database</code><br/>
<i>string</i>
</td>
<td>
   <p>Name of the database used by the application. Default: <code>app</code>.</p>
</td>
</tr>
<tr><td><code>owner</code><br/>
<i>string</i>
</td>
<td>
   <p>Name of the owner of the database in the instance to be used
by applications. Defaults to the value of the <code>database</code> key.</p>
</td>
</tr>
<tr><td><code>secret</code><br/>
<a href="#postgresql-cnpg-io-v1-LocalObjectReference"><i>LocalObjectReference</i></a>
</td>
<td>
   <p>Name of the secret containing the initial credentials for the
owner of the user database. If empty a new secret will be
created from scratch</p>
</td>
</tr>
</tbody>
</table>

## CatalogImage     {#postgresql-cnpg-io-v1-CatalogImage}


**Appears in:**

- [ImageCatalogSpec](#postgresql-cnpg-io-v1-ImageCatalogSpec)


<p>CatalogImage defines the image and major version</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>image</code> <B>[Required]</B><br/>
<i>string</i>
</td>
<td>
   <p>The image reference</p>
</td>
</tr>
<tr><td><code>major</code> <B>[Required]</B><br/>
<i>int</i>
</td>
<td>
   <p>The PostgreSQL major version of the image. Must be unique within the catalog.</p>
</td>
</tr>
</tbody>
</table>

## CertificatesConfiguration     {#postgresql-cnpg-io-v1-CertificatesConfiguration}


**Appears in:**

- [CertificatesStatus](#postgresql-cnpg-io-v1-CertificatesStatus)

- [ClusterSpec](#postgresql-cnpg-io-v1-ClusterSpec)


<p>CertificatesConfiguration contains the needed configurations to handle server certificates.</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>serverCASecret</code><br/>
<i>string</i>
</td>
<td>
   <p>The secret containing the Server CA certificate. If not defined, a new secret will be created
with a self-signed CA and will be used to generate the TLS certificate ServerTLSSecret.<!-- raw HTML omitted -->
<!-- raw HTML omitted -->
Contains:<!-- raw HTML omitted -->
<!-- raw HTML omitted --></p>
<ul>
<li><code>ca.crt</code>: CA that should be used to validate the server certificate,
used as <code>sslrootcert</code> in client connection strings.<!-- raw HTML omitted --></li>
<li><code>ca.key</code>: key used to generate Server SSL certs, if ServerTLSSecret is provided,
this can be omitted.<!-- raw HTML omitted --></li>
</ul>
</td>
</tr>
<tr><td><code>serverTLSSecret</code><br/>
<i>string</i>
</td>
<td>
   <p>The secret of type kubernetes.io/tls containing the server TLS certificate and key that will be set as
<code>ssl_cert_file</code> and <code>ssl_key_file</code> so that clients can connect to postgres securely.
If not defined, ServerCASecret must provide also <code>ca.key</code> and a new secret will be
created using the provided CA.</p>
</td>
</tr>
<tr><td><code>replicationTLSSecret</code><br/>
<i>string</i>
</td>
<td>
   <p>The secret of type kubernetes.io/tls containing the client certificate to authenticate as
the <code>streaming_replica</code> user.
If not defined, ClientCASecret must provide also <code>ca.key</code>, and a new secret will be
created using the provided CA.</p>
</td>
</tr>
<tr><td><code>clientCASecret</code><br/>
<i>string</i>
</td>
<td>
   <p>The secret containing the Client CA certificate. If not defined, a new secret will be created
with a self-signed CA and will be used to generate all the client certificates.<!-- raw HTML omitted -->
<!-- raw HTML omitted -->
Contains:<!-- raw HTML omitted -->
<!-- raw HTML omitted --></p>
<ul>
<li><code>ca.crt</code>: CA that should be used to validate the client certificates,
used as <code>ssl_ca_file</code> of all the instances.<!-- raw HTML omitted --></li>
<li><code>ca.key</code>: key used to generate client certificates, if ReplicationTLSSecret is provided,
this can be omitted.<!-- raw HTML omitted --></li>
</ul>
</td>
</tr>
<tr><td><code>serverAltDNSNames</code><br/>
<i>[]string</i>
</td>
<td>
   <p>The list of the server alternative DNS names to be added to the generated server TLS certificates, when required.</p>
</td>
</tr>
</tbody>
</table>

## CertificatesStatus     {#postgresql-cnpg-io-v1-CertificatesStatus}


**Appears in:**

- [ClusterStatus](#postgresql-cnpg-io-v1-ClusterStatus)


<p>CertificatesStatus contains configuration certificates and related expiration dates.</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>CertificatesConfiguration</code><br/>
<a href="#postgresql-cnpg-io-v1-CertificatesConfiguration"><i>CertificatesConfiguration</i></a>
</td>
<td>(Members of <code>CertificatesConfiguration</code> are embedded into this type.)
   <p>Needed configurations to handle server certificates, initialized with default values, if needed.</p>
</td>
</tr>
<tr><td><code>expirations</code><br/>
<i>map[string]string</i>
</td>
<td>
   <p>Expiration dates for all certificates.</p>
</td>
</tr>
</tbody>
</table>

## ClusterSpec     {#postgresql-cnpg-io-v1-ClusterSpec}


**Appears in:**

- [Cluster](#postgresql-cnpg-io-v1-Cluster)


<p>ClusterSpec defines the desired state of Cluster</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>description</code><br/>
<i>string</i>
</td>
<td>
   <p>Description of this PostgreSQL cluster</p>
</td>
</tr>
<tr><td><code>inheritedMetadata</code><br/>
<a href="#postgresql-cnpg-io-v1-EmbeddedObjectMetadata"><i>EmbeddedObjectMetadata</i></a>
</td>
<td>
   <p>Metadata that will be inherited by all objects related to the Cluster</p>
</td>
</tr>
<tr><td><code>imageName</code><br/>
<i>string</i>
</td>
<td>
   <p>Name of the container image, supporting both tags (<code>&lt;image&gt;:&lt;tag&gt;</code>)
and digests for deterministic and repeatable deployments
(<code>&lt;image&gt;:&lt;tag&gt;@sha256:&lt;digestValue&gt;</code>)</p>
</td>
</tr>
<tr><td><code>imageCatalogRef</code><br/>
<a href="#postgresql-cnpg-io-v1-ImageCatalogRef"><i>ImageCatalogRef</i></a>
</td>
<td>
   <p>Defines the major PostgreSQL version we want to use within an ImageCatalog</p>
</td>
</tr>
<tr><td><code>imagePullPolicy</code><br/>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#pullpolicy-v1-core"><i>core/v1.PullPolicy</i></a>
</td>
<td>
   <p>Image pull policy.
One of <code>Always</code>, <code>Never</code> or <code>IfNotPresent</code>.
If not defined, it defaults to <code>IfNotPresent</code>.
Cannot be updated.
More info: https://kubernetes.io/docs/concepts/containers/images#updating-images</p>
</td>
</tr>
<tr><td><code>schedulerName</code><br/>
<i>string</i>
</td>
<td>
   <p>If specified, the pod will be dispatched by specified Kubernetes
scheduler. If not specified, the pod will be dispatched by the default
scheduler. More info:
https://kubernetes.io/docs/concepts/scheduling-eviction/kube-scheduler/</p>
</td>
</tr>
<tr><td><code>postgresUID</code><br/>
<i>int64</i>
</td>
<td>
   <p>The UID of the <code>postgres</code> user inside the image, defaults to <code>26</code></p>
</td>
</tr>
<tr><td><code>postgresGID</code><br/>
<i>int64</i>
</td>
<td>
   <p>The GID of the <code>postgres</code> user inside the image, defaults to <code>26</code></p>
</td>
</tr>
<tr><td><code>instances</code> <B>[Required]</B><br/>
<i>int</i>
</td>
<td>
   <p>Number of instances required in the cluster</p>
</td>
</tr>
<tr><td><code>minSyncReplicas</code><br/>
<i>int</i>
</td>
<td>
   <p>Minimum number of instances required in synchronous replication with the
primary. Undefined or 0 allow writes to complete when no standby is
available.</p>
</td>
</tr>
<tr><td><code>maxSyncReplicas</code><br/>
<i>int</i>
</td>
<td>
   <p>The target value for the synchronous replication quorum, that can be
decreased if the number of ready standbys is lower than this.
Undefined or 0 disable synchronous replication.</p>
</td>
</tr>
<tr><td><code>postgresql</code><br/>
<a href="#postgresql-cnpg-io-v1-PostgresConfiguration"><i>PostgresConfiguration</i></a>
</td>
<td>
   <p>Configuration of the PostgreSQL server</p>
</td>
</tr>
<tr><td><code>replicationSlots</code><br/>
<a href="#postgresql-cnpg-io-v1-ReplicationSlotsConfiguration"><i>ReplicationSlotsConfiguration</i></a>
</td>
<td>
   <p>Replication slots management configuration</p>
</td>
</tr>
<tr><td><code>bootstrap</code><br/>
<a href="#postgresql-cnpg-io-v1-BootstrapConfiguration"><i>BootstrapConfiguration</i></a>
</td>
<td>
   <p>Instructions to bootstrap this cluster</p>
</td>
</tr>
<tr><td><code>replica</code><br/>
<a href="#postgresql-cnpg-io-v1-ReplicaClusterConfiguration"><i>ReplicaClusterConfiguration</i></a>
</td>
<td>
   <p>Replica cluster configuration</p>
</td>
</tr>
<tr><td><code>superuserSecret</code><br/>
<a href="#postgresql-cnpg-io-v1-LocalObjectReference"><i>LocalObjectReference</i></a>
</td>
<td>
   <p>The secret containing the superuser password. If not defined a new
secret will be created with a randomly generated password</p>
</td>
</tr>
<tr><td><code>enableSuperuserAccess</code><br/>
<i>bool</i>
</td>
<td>
   <p>When this option is enabled, the operator will use the <code>SuperuserSecret</code>
to update the <code>postgres</code> user password (if the secret is
not present, the operator will automatically create one). When this
option is disabled, the operator will ignore the <code>SuperuserSecret</code> content, delete
it when automatically created, and then blank the password of the <code>postgres</code>
user by setting it to <code>NULL</code>. Disabled by default.</p>
</td>
</tr>
<tr><td><code>certificates</code><br/>
<a href="#postgresql-cnpg-io-v1-CertificatesConfiguration"><i>CertificatesConfiguration</i></a>
</td>
<td>
   <p>The configuration for the CA and related certificates</p>
</td>
</tr>
<tr><td><code>imagePullSecrets</code><br/>
<a href="#postgresql-cnpg-io-v1-LocalObjectReference"><i>[]LocalObjectReference</i></a>
</td>
<td>
   <p>The list of pull secrets to be used to pull the images</p>
</td>
</tr>
<tr><td><code>storage</code><br/>
<a href="#postgresql-cnpg-io-v1-StorageConfiguration"><i>StorageConfiguration</i></a>
</td>
<td>
   <p>Configuration of the storage of the instances</p>
</td>
</tr>
<tr><td><code>serviceAccountTemplate</code><br/>
<a href="#postgresql-cnpg-io-v1-ServiceAccountTemplate"><i>ServiceAccountTemplate</i></a>
</td>
<td>
   <p>Configure the generation of the service account</p>
</td>
</tr>
<tr><td><code>walStorage</code><br/>
<a href="#postgresql-cnpg-io-v1-StorageConfiguration"><i>StorageConfiguration</i></a>
</td>
<td>
   <p>Configuration of the storage for PostgreSQL WAL (Write-Ahead Log)</p>
</td>
</tr>
<tr><td><code>ephemeralVolumeSource</code><br/>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#ephemeralvolumesource-v1-core"><i>core/v1.EphemeralVolumeSource</i></a>
</td>
<td>
   <p>EphemeralVolumeSource allows the user to configure the source of ephemeral volumes.</p>
</td>
</tr>
<tr><td><code>startDelay</code><br/>
<i>int32</i>
</td>
<td>
   <p>The time in seconds that is allowed for a PostgreSQL instance to
successfully start up (default 3600).
The startup probe failure threshold is derived from this value using the formula:
ceiling(startDelay / 10).</p>
</td>
</tr>
<tr><td><code>stopDelay</code><br/>
<i>int32</i>
</td>
<td>
   <p>The time in seconds that is allowed for a PostgreSQL instance to
gracefully shutdown (default 1800)</p>
</td>
</tr>
<tr><td><code>smartShutdownTimeout</code><br/>
<i>int32</i>
</td>
<td>
   <p>The time in seconds that controls the window of time reserved for the smart shutdown of Postgres to complete.
Make sure you reserve enough time for the operator to request a fast shutdown of Postgres
(that is: <code>stopDelay</code> - <code>smartShutdownTimeout</code>).</p>
</td>
</tr>
<tr><td><code>switchoverDelay</code><br/>
<i>int32</i>
</td>
<td>
   <p>The time in seconds that is allowed for a primary PostgreSQL instance
to gracefully shutdown during a switchover.
Default value is 3600 seconds (1 hour).</p>
</td>
</tr>
<tr><td><code>failoverDelay</code><br/>
<i>int32</i>
</td>
<td>
   <p>The amount of time (in seconds) to wait before triggering a failover
after the primary PostgreSQL instance in the cluster was detected
to be unhealthy</p>
</td>
</tr>
<tr><td><code>affinity</code><br/>
<a href="#postgresql-cnpg-io-v1-AffinityConfiguration"><i>AffinityConfiguration</i></a>
</td>
<td>
   <p>Affinity/Anti-affinity rules for Pods</p>
</td>
</tr>
<tr><td><code>topologySpreadConstraints</code><br/>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#topologyspreadconstraint-v1-core"><i>[]core/v1.TopologySpreadConstraint</i></a>
</td>
<td>
   <p>TopologySpreadConstraints specifies how to spread matching pods among the given topology.
More info:
https://kubernetes.io/docs/concepts/scheduling-eviction/topology-spread-constraints/</p>
</td>
</tr>
<tr><td><code>resources</code><br/>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#resourcerequirements-v1-core"><i>core/v1.ResourceRequirements</i></a>
</td>
<td>
   <p>Resources requirements of every generated Pod. Please refer to
https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/
for more information.</p>
</td>
</tr>
<tr><td><code>ephemeralVolumesSizeLimit</code> <B>[Required]</B><br/>
<a href="#postgresql-cnpg-io-v1-EphemeralVolumesSizeLimitConfiguration"><i>EphemeralVolumesSizeLimitConfiguration</i></a>
</td>
<td>
   <p>EphemeralVolumesSizeLimit allows the user to set the limits for the ephemeral
volumes</p>
</td>
</tr>
<tr><td><code>priorityClassName</code><br/>
<i>string</i>
</td>
<td>
   <p>Name of the priority class which will be used in every generated Pod, if the PriorityClass
specified does not exist, the pod will not be able to schedule.  Please refer to
https://kubernetes.io/docs/concepts/scheduling-eviction/pod-priority-preemption/#priorityclass
for more information</p>
</td>
</tr>
<tr><td><code>primaryUpdateStrategy</code><br/>
<a href="#postgresql-cnpg-io-v1-PrimaryUpdateStrategy"><i>PrimaryUpdateStrategy</i></a>
</td>
<td>
   <p>Deployment strategy to follow to upgrade the primary server during a rolling
update procedure, after all replicas have been successfully updated:
it can be automated (<code>unsupervised</code> - default) or manual (<code>supervised</code>)</p>
</td>
</tr>
<tr><td><code>primaryUpdateMethod</code><br/>
<a href="#postgresql-cnpg-io-v1-PrimaryUpdateMethod"><i>PrimaryUpdateMethod</i></a>
</td>
<td>
   <p>Method to follow to upgrade the primary server during a rolling
update procedure, after all replicas have been successfully updated:
it can be with a switchover (<code>switchover</code>) or in-place (<code>restart</code> - default)</p>
</td>
</tr>
<tr><td><code>backup</code><br/>
<a href="#postgresql-cnpg-io-v1-BackupConfiguration"><i>BackupConfiguration</i></a>
</td>
<td>
   <p>The configuration to be used for backups</p>
</td>
</tr>
<tr><td><code>nodeMaintenanceWindow</code><br/>
<a href="#postgresql-cnpg-io-v1-NodeMaintenanceWindow"><i>NodeMaintenanceWindow</i></a>
</td>
<td>
   <p>Define a maintenance window for the Kubernetes nodes</p>
</td>
</tr>
<tr><td><code>monitoring</code><br/>
<a href="#postgresql-cnpg-io-v1-MonitoringConfiguration"><i>MonitoringConfiguration</i></a>
</td>
<td>
   <p>The configuration of the monitoring infrastructure of this cluster</p>
</td>
</tr>
<tr><td><code>externalClusters</code><br/>
<a href="#postgresql-cnpg-io-v1-ExternalCluster"><i>[]ExternalCluster</i></a>
</td>
<td>
   <p>The list of external clusters which are used in the configuration</p>
</td>
</tr>
<tr><td><code>logLevel</code><br/>
<i>string</i>
</td>
<td>
   <p>The instances' log level, one of the following values: error, warning, info (default), debug, trace</p>
</td>
</tr>
<tr><td><code>projectedVolumeTemplate</code><br/>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#projectedvolumesource-v1-core"><i>core/v1.ProjectedVolumeSource</i></a>
</td>
<td>
   <p>Template to be used to define projected volumes, projected volumes will be mounted
under <code>/projected</code> base folder</p>
</td>
</tr>
<tr><td><code>env</code><br/>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#envvar-v1-core"><i>[]core/v1.EnvVar</i></a>
</td>
<td>
   <p>Env follows the Env format to pass environment variables
to the pods created in the cluster</p>
</td>
</tr>
<tr><td><code>envFrom</code><br/>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#envfromsource-v1-core"><i>[]core/v1.EnvFromSource</i></a>
</td>
<td>
   <p>EnvFrom follows the EnvFrom format to pass environment variables
sources to the pods to be used by Env</p>
</td>
</tr>
<tr><td><code>managed</code><br/>
<a href="#postgresql-cnpg-io-v1-ManagedConfiguration"><i>ManagedConfiguration</i></a>
</td>
<td>
   <p>The configuration that is used by the portions of PostgreSQL that are managed by the instance manager</p>
</td>
</tr>
<tr><td><code>seccompProfile</code><br/>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#seccompprofile-v1-core"><i>core/v1.SeccompProfile</i></a>
</td>
<td>
   <p>The SeccompProfile applied to every Pod and Container.
Defaults to: <code>RuntimeDefault</code></p>
</td>
</tr>
<tr><td><code>tablespaces</code><br/>
<a href="#postgresql-cnpg-io-v1-TablespaceConfiguration"><i>[]TablespaceConfiguration</i></a>
</td>
<td>
   <p>The tablespaces configuration</p>
</td>
</tr>
<tr><td><code>enablePDB</code><br/>
<i>bool</i>
</td>
<td>
   <p>Manage the <code>PodDisruptionBudget</code> resources within the cluster. When
configured as <code>true</code> (default setting), the pod disruption budgets
will safeguard the primary node from being terminated. Conversely,
setting it to <code>false</code> will result in the absence of any
<code>PodDisruptionBudget</code> resource, permitting the shutdown of all nodes
hosting the PostgreSQL cluster. This latter configuration is
advisable for any PostgreSQL cluster employed for
development/staging purposes.</p>
</td>
</tr>
<tr><td><code>plugins</code> <B>[Required]</B><br/>
<a href="#postgresql-cnpg-io-v1-PluginConfigurationList"><i>PluginConfigurationList</i></a>
</td>
<td>
   <p>The plugins configuration, containing
any plugin to be loaded with the corresponding configuration</p>
</td>
</tr>
</tbody>
</table>

## ClusterStatus     {#postgresql-cnpg-io-v1-ClusterStatus}


**Appears in:**

- [Cluster](#postgresql-cnpg-io-v1-Cluster)


<p>ClusterStatus defines the observed state of Cluster</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>instances</code><br/>
<i>int</i>
</td>
<td>
   <p>The total number of PVC Groups detected in the cluster. It may differ from the number of existing instance pods.</p>
</td>
</tr>
<tr><td><code>readyInstances</code><br/>
<i>int</i>
</td>
<td>
   <p>The total number of ready instances in the cluster. It is equal to the number of ready instance pods.</p>
</td>
</tr>
<tr><td><code>instancesStatus</code><br/>
<i>map[PodStatus][]string</i>
</td>
<td>
   <p>InstancesStatus indicates in which status the instances are</p>
</td>
</tr>
<tr><td><code>instancesReportedState</code><br/>
<a href="#postgresql-cnpg-io-v1-InstanceReportedState"><i>map[PodName]InstanceReportedState</i></a>
</td>
<td>
   <p>The reported state of the instances during the last reconciliation loop</p>
</td>
</tr>
<tr><td><code>managedRolesStatus</code><br/>
<a href="#postgresql-cnpg-io-v1-ManagedRoles"><i>ManagedRoles</i></a>
</td>
<td>
   <p>ManagedRolesStatus reports the state of the managed roles in the cluster</p>
</td>
</tr>
<tr><td><code>tablespacesStatus</code><br/>
<a href="#postgresql-cnpg-io-v1-TablespaceState"><i>[]TablespaceState</i></a>
</td>
<td>
   <p>TablespacesStatus reports the state of the declarative tablespaces in the cluster</p>
</td>
</tr>
<tr><td><code>timelineID</code><br/>
<i>int</i>
</td>
<td>
   <p>The timeline of the Postgres cluster</p>
</td>
</tr>
<tr><td><code>topology</code><br/>
<a href="#postgresql-cnpg-io-v1-Topology"><i>Topology</i></a>
</td>
<td>
   <p>Instances topology.</p>
</td>
</tr>
<tr><td><code>latestGeneratedNode</code><br/>
<i>int</i>
</td>
<td>
   <p>ID of the latest generated node (used to avoid node name clashing)</p>
</td>
</tr>
<tr><td><code>currentPrimary</code><br/>
<i>string</i>
</td>
<td>
   <p>Current primary instance</p>
</td>
</tr>
<tr><td><code>targetPrimary</code><br/>
<i>string</i>
</td>
<td>
   <p>Target primary instance, this is different from the previous one
during a switchover or a failover</p>
</td>
</tr>
<tr><td><code>pvcCount</code><br/>
<i>int32</i>
</td>
<td>
   <p>How many PVCs have been created by this cluster</p>
</td>
</tr>
<tr><td><code>jobCount</code><br/>
<i>int32</i>
</td>
<td>
   <p>How many Jobs have been created by this cluster</p>
</td>
</tr>
<tr><td><code>danglingPVC</code><br/>
<i>[]string</i>
</td>
<td>
   <p>List of all the PVCs created by this cluster and still available
which are not attached to a Pod</p>
</td>
</tr>
<tr><td><code>resizingPVC</code><br/>
<i>[]string</i>
</td>
<td>
   <p>List of all the PVCs that have ResizingPVC condition.</p>
</td>
</tr>
<tr><td><code>initializingPVC</code><br/>
<i>[]string</i>
</td>
<td>
   <p>List of all the PVCs that are being initialized by this cluster</p>
</td>
</tr>
<tr><td><code>healthyPVC</code><br/>
<i>[]string</i>
</td>
<td>
   <p>List of all the PVCs not dangling nor initializing</p>
</td>
</tr>
<tr><td><code>unusablePVC</code><br/>
<i>[]string</i>
</td>
<td>
   <p>List of all the PVCs that are unusable because another PVC is missing</p>
</td>
</tr>
<tr><td><code>writeService</code><br/>
<i>string</i>
</td>
<td>
   <p>Current write pod</p>
</td>
</tr>
<tr><td><code>readService</code><br/>
<i>string</i>
</td>
<td>
   <p>Current list of read pods</p>
</td>
</tr>
<tr><td><code>phase</code><br/>
<i>string</i>
</td>
<td>
   <p>Current phase of the cluster</p>
</td>
</tr>
<tr><td><code>phaseReason</code><br/>
<i>string</i>
</td>
<td>
   <p>Reason for the current phase</p>
</td>
</tr>
<tr><td><code>secretsResourceVersion</code><br/>
<a href="#postgresql-cnpg-io-v1-SecretsResourceVersion"><i>SecretsResourceVersion</i></a>
</td>
<td>
   <p>The list of resource versions of the secrets
managed by the operator. Every change here is done in the
interest of the instance manager, which will refresh the
secret data</p>
</td>
</tr>
<tr><td><code>configMapResourceVersion</code><br/>
<a href="#postgresql-cnpg-io-v1-ConfigMapResourceVersion"><i>ConfigMapResourceVersion</i></a>
</td>
<td>
   <p>The list of resource versions of the configmaps,
managed by the operator. Every change here is done in the
interest of the instance manager, which will refresh the
configmap data</p>
</td>
</tr>
<tr><td><code>certificates</code><br/>
<a href="#postgresql-cnpg-io-v1-CertificatesStatus"><i>CertificatesStatus</i></a>
</td>
<td>
   <p>The configuration for the CA and related certificates, initialized with defaults.</p>
</td>
</tr>
<tr><td><code>firstRecoverabilityPoint</code><br/>
<i>string</i>
</td>
<td>
   <p>The first recoverability point, stored as a date in RFC3339 format.
This field is calculated from the content of FirstRecoverabilityPointByMethod</p>
</td>
</tr>
<tr><td><code>firstRecoverabilityPointByMethod</code><br/>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#time-v1-meta"><i>map[BackupMethod]meta/v1.Time</i></a>
</td>
<td>
   <p>The first recoverability point, stored as a date in RFC3339 format, per backup method type</p>
</td>
</tr>
<tr><td><code>lastSuccessfulBackup</code><br/>
<i>string</i>
</td>
<td>
   <p>Last successful backup, stored as a date in RFC3339 format
This field is calculated from the content of LastSuccessfulBackupByMethod</p>
</td>
</tr>
<tr><td><code>lastSuccessfulBackupByMethod</code><br/>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#time-v1-meta"><i>map[BackupMethod]meta/v1.Time</i></a>
</td>
<td>
   <p>Last successful backup, stored as a date in RFC3339 format, per backup method type</p>
</td>
</tr>
<tr><td><code>lastFailedBackup</code><br/>
<i>string</i>
</td>
<td>
   <p>Stored as a date in RFC3339 format</p>
</td>
</tr>
<tr><td><code>cloudNativePGCommitHash</code><br/>
<i>string</i>
</td>
<td>
   <p>The commit hash number of which this operator running</p>
</td>
</tr>
<tr><td><code>currentPrimaryTimestamp</code><br/>
<i>string</i>
</td>
<td>
   <p>The timestamp when the last actual promotion to primary has occurred</p>
</td>
</tr>
<tr><td><code>currentPrimaryFailingSinceTimestamp</code><br/>
<i>string</i>
</td>
<td>
   <p>The timestamp when the primary was detected to be unhealthy
This field is reported when <code>.spec.failoverDelay</code> is populated or during online upgrades</p>
</td>
</tr>
<tr><td><code>targetPrimaryTimestamp</code><br/>
<i>string</i>
</td>
<td>
   <p>The timestamp when the last request for a new primary has occurred</p>
</td>
</tr>
<tr><td><code>poolerIntegrations</code><br/>
<a href="#postgresql-cnpg-io-v1-PoolerIntegrations"><i>PoolerIntegrations</i></a>
</td>
<td>
   <p>The integration needed by poolers referencing the cluster</p>
</td>
</tr>
<tr><td><code>cloudNativePGOperatorHash</code><br/>
<i>string</i>
</td>
<td>
   <p>The hash of the binary of the operator</p>
</td>
</tr>
<tr><td><code>availableArchitectures</code><br/>
<a href="#postgresql-cnpg-io-v1-AvailableArchitecture"><i>[]AvailableArchitecture</i></a>
</td>
<td>
   <p>AvailableArchitectures reports the available architectures of a cluster</p>
</td>
</tr>
<tr><td><code>conditions</code><br/>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#condition-v1-meta"><i>[]meta/v1.Condition</i></a>
</td>
<td>
   <p>Conditions for cluster object</p>
</td>
</tr>
<tr><td><code>instanceNames</code><br/>
<i>[]string</i>
</td>
<td>
   <p>List of instance names in the cluster</p>
</td>
</tr>
<tr><td><code>onlineUpdateEnabled</code><br/>
<i>bool</i>
</td>
<td>
   <p>OnlineUpdateEnabled shows if the online upgrade is enabled inside the cluster</p>
</td>
</tr>
<tr><td><code>azurePVCUpdateEnabled</code><br/>
<i>bool</i>
</td>
<td>
   <p>AzurePVCUpdateEnabled shows if the PVC online upgrade is enabled for this cluster</p>
</td>
</tr>
<tr><td><code>image</code><br/>
<i>string</i>
</td>
<td>
   <p>Image contains the image name used by the pods</p>
</td>
</tr>
<tr><td><code>pluginStatus</code> <B>[Required]</B><br/>
<a href="#postgresql-cnpg-io-v1-PluginStatus"><i>[]PluginStatus</i></a>
</td>
<td>
   <p>PluginStatus is the status of the loaded plugins</p>
</td>
</tr>
<tr><td><code>switchReplicaClusterStatus</code><br/>
<a href="#postgresql-cnpg-io-v1-SwitchReplicaClusterStatus"><i>SwitchReplicaClusterStatus</i></a>
</td>
<td>
   <p>SwitchReplicaClusterStatus is the status of the switch to replica cluster</p>
</td>
</tr>
</tbody>
</table>

## CompressionType     {#postgresql-cnpg-io-v1-CompressionType}

(Alias of `string`)

**Appears in:**

- [DataBackupConfiguration](#postgresql-cnpg-io-v1-DataBackupConfiguration)

- [WalBackupConfiguration](#postgresql-cnpg-io-v1-WalBackupConfiguration)


<p>CompressionType encapsulates the available types of compression</p>




## ConfigMapKeySelector     {#postgresql-cnpg-io-v1-ConfigMapKeySelector}


**Appears in:**

- [MonitoringConfiguration](#postgresql-cnpg-io-v1-MonitoringConfiguration)

- [PostInitApplicationSQLRefs](#postgresql-cnpg-io-v1-PostInitApplicationSQLRefs)


<p>ConfigMapKeySelector contains enough information to let you locate
the key of a ConfigMap</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>LocalObjectReference</code><br/>
<a href="#postgresql-cnpg-io-v1-LocalObjectReference"><i>LocalObjectReference</i></a>
</td>
<td>(Members of <code>LocalObjectReference</code> are embedded into this type.)
   <p>The name of the secret in the pod's namespace to select from.</p>
</td>
</tr>
<tr><td><code>key</code> <B>[Required]</B><br/>
<i>string</i>
</td>
<td>
   <p>The key to select</p>
</td>
</tr>
</tbody>
</table>

## ConfigMapResourceVersion     {#postgresql-cnpg-io-v1-ConfigMapResourceVersion}


**Appears in:**

- [ClusterStatus](#postgresql-cnpg-io-v1-ClusterStatus)


<p>ConfigMapResourceVersion is the resource versions of the secrets
managed by the operator</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>metrics</code><br/>
<i>map[string]string</i>
</td>
<td>
   <p>A map with the versions of all the config maps used to pass metrics.
Map keys are the config map names, map values are the versions</p>
</td>
</tr>
</tbody>
</table>

## DataBackupConfiguration     {#postgresql-cnpg-io-v1-DataBackupConfiguration}


**Appears in:**

- [BarmanObjectStoreConfiguration](#postgresql-cnpg-io-v1-BarmanObjectStoreConfiguration)


<p>DataBackupConfiguration is the configuration of the backup of
the data directory</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>compression</code><br/>
<a href="#postgresql-cnpg-io-v1-CompressionType"><i>CompressionType</i></a>
</td>
<td>
   <p>Compress a backup file (a tar file per tablespace) while streaming it
to the object store. Available options are empty string (no
compression, default), <code>gzip</code>, <code>bzip2</code> or <code>snappy</code>.</p>
</td>
</tr>
<tr><td><code>encryption</code><br/>
<a href="#postgresql-cnpg-io-v1-EncryptionType"><i>EncryptionType</i></a>
</td>
<td>
   <p>Whenever to force the encryption of files (if the bucket is
not already configured for that).
Allowed options are empty string (use the bucket policy, default),
<code>AES256</code> and <code>aws:kms</code></p>
</td>
</tr>
<tr><td><code>jobs</code><br/>
<i>int32</i>
</td>
<td>
   <p>The number of parallel jobs to be used to upload the backup, defaults
to 2</p>
</td>
</tr>
<tr><td><code>immediateCheckpoint</code><br/>
<i>bool</i>
</td>
<td>
   <p>Control whether the I/O workload for the backup initial checkpoint will
be limited, according to the <code>checkpoint_completion_target</code> setting on
the PostgreSQL server. If set to true, an immediate checkpoint will be
used, meaning PostgreSQL will complete the checkpoint as soon as
possible. <code>false</code> by default.</p>
</td>
</tr>
<tr><td><code>additionalCommandArgs</code> <B>[Required]</B><br/>
<i>[]string</i>
</td>
<td>
   <p>AdditionalCommandArgs represents additional arguments that can be appended
to the 'barman-cloud-backup' command-line invocation. These arguments
provide flexibility to customize the backup process further according to
specific requirements or configurations.</p>
<p>Example:
In a scenario where specialized backup options are required, such as setting
a specific timeout or defining custom behavior, users can use this field
to specify additional command arguments.</p>
<p>Note:
It's essential to ensure that the provided arguments are valid and supported
by the 'barman-cloud-backup' command, to avoid potential errors or unintended
behavior during execution.</p>
</td>
</tr>
</tbody>
</table>

## DataSource     {#postgresql-cnpg-io-v1-DataSource}


**Appears in:**

- [BootstrapRecovery](#postgresql-cnpg-io-v1-BootstrapRecovery)


<p>DataSource contains the configuration required to bootstrap a
PostgreSQL cluster from an existing storage</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>storage</code> <B>[Required]</B><br/>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#typedlocalobjectreference-v1-core"><i>core/v1.TypedLocalObjectReference</i></a>
</td>
<td>
   <p>Configuration of the storage of the instances</p>
</td>
</tr>
<tr><td><code>walStorage</code><br/>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#typedlocalobjectreference-v1-core"><i>core/v1.TypedLocalObjectReference</i></a>
</td>
<td>
   <p>Configuration of the storage for PostgreSQL WAL (Write-Ahead Log)</p>
</td>
</tr>
<tr><td><code>tablespaceStorage</code><br/>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#typedlocalobjectreference-v1-core"><i>map[string]core/v1.TypedLocalObjectReference</i></a>
</td>
<td>
   <p>Configuration of the storage for PostgreSQL tablespaces</p>
</td>
</tr>
</tbody>
</table>

## DatabaseRoleRef     {#postgresql-cnpg-io-v1-DatabaseRoleRef}


**Appears in:**

- [TablespaceConfiguration](#postgresql-cnpg-io-v1-TablespaceConfiguration)


<p>DatabaseRoleRef is a reference an a role available inside PostgreSQL</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>name</code><br/>
<i>string</i>
</td>
<td>
   <span class="text-muted">No description provided.</span></td>
</tr>
</tbody>
</table>

## EmbeddedObjectMetadata     {#postgresql-cnpg-io-v1-EmbeddedObjectMetadata}


**Appears in:**

- [ClusterSpec](#postgresql-cnpg-io-v1-ClusterSpec)


<p>EmbeddedObjectMetadata contains metadata to be inherited by all resources related to a Cluster</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>labels</code><br/>
<i>map[string]string</i>
</td>
<td>
   <span class="text-muted">No description provided.</span></td>
</tr>
<tr><td><code>annotations</code><br/>
<i>map[string]string</i>
</td>
<td>
   <span class="text-muted">No description provided.</span></td>
</tr>
</tbody>
</table>

## EncryptionType     {#postgresql-cnpg-io-v1-EncryptionType}

(Alias of `string`)

**Appears in:**

- [DataBackupConfiguration](#postgresql-cnpg-io-v1-DataBackupConfiguration)

- [WalBackupConfiguration](#postgresql-cnpg-io-v1-WalBackupConfiguration)


<p>EncryptionType encapsulated the available types of encryption</p>




## EnsureOption     {#postgresql-cnpg-io-v1-EnsureOption}

(Alias of `string`)

**Appears in:**

- [RoleConfiguration](#postgresql-cnpg-io-v1-RoleConfiguration)


<p>EnsureOption represents whether we should enforce the presence or absence of
a Role in a PostgreSQL instance</p>




## EphemeralVolumesSizeLimitConfiguration     {#postgresql-cnpg-io-v1-EphemeralVolumesSizeLimitConfiguration}


**Appears in:**

- [ClusterSpec](#postgresql-cnpg-io-v1-ClusterSpec)


<p>EphemeralVolumesSizeLimitConfiguration contains the configuration of the ephemeral
storage</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>shm</code> <B>[Required]</B><br/>
<a href="https://pkg.go.dev/k8s.io/apimachinery/pkg/api/resource#Quantity"><i>k8s.io/apimachinery/pkg/api/resource.Quantity</i></a>
</td>
<td>
   <p>Shm is the size limit of the shared memory volume</p>
</td>
</tr>
<tr><td><code>temporaryData</code> <B>[Required]</B><br/>
<a href="https://pkg.go.dev/k8s.io/apimachinery/pkg/api/resource#Quantity"><i>k8s.io/apimachinery/pkg/api/resource.Quantity</i></a>
</td>
<td>
   <p>TemporaryData is the size limit of the temporary data volume</p>
</td>
</tr>
</tbody>
</table>

## ExternalCluster     {#postgresql-cnpg-io-v1-ExternalCluster}


**Appears in:**

- [ClusterSpec](#postgresql-cnpg-io-v1-ClusterSpec)


<p>ExternalCluster represents the connection parameters to an
external cluster which is used in the other sections of the configuration</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>name</code> <B>[Required]</B><br/>
<i>string</i>
</td>
<td>
   <p>The server name, required</p>
</td>
</tr>
<tr><td><code>connectionParameters</code><br/>
<i>map[string]string</i>
</td>
<td>
   <p>The list of connection parameters, such as dbname, host, username, etc</p>
</td>
</tr>
<tr><td><code>sslCert</code><br/>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#secretkeyselector-v1-core"><i>core/v1.SecretKeySelector</i></a>
</td>
<td>
   <p>The reference to an SSL certificate to be used to connect to this
instance</p>
</td>
</tr>
<tr><td><code>sslKey</code><br/>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#secretkeyselector-v1-core"><i>core/v1.SecretKeySelector</i></a>
</td>
<td>
   <p>The reference to an SSL private key to be used to connect to this
instance</p>
</td>
</tr>
<tr><td><code>sslRootCert</code><br/>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#secretkeyselector-v1-core"><i>core/v1.SecretKeySelector</i></a>
</td>
<td>
   <p>The reference to an SSL CA public key to be used to connect to this
instance</p>
</td>
</tr>
<tr><td><code>password</code><br/>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#secretkeyselector-v1-core"><i>core/v1.SecretKeySelector</i></a>
</td>
<td>
   <p>The reference to the password to be used to connect to the server.
If a password is provided, CloudNativePG creates a PostgreSQL
passfile at <code>/controller/external/NAME/pass</code> (where &quot;NAME&quot; is the
cluster's name). This passfile is automatically referenced in the
connection string when establishing a connection to the remote
PostgreSQL server from the current PostgreSQL <code>Cluster</code>. This ensures
secure and efficient password management for external clusters.</p>
</td>
</tr>
<tr><td><code>barmanObjectStore</code><br/>
<a href="#postgresql-cnpg-io-v1-BarmanObjectStoreConfiguration"><i>BarmanObjectStoreConfiguration</i></a>
</td>
<td>
   <p>The configuration for the barman-cloud tool suite</p>
</td>
</tr>
</tbody>
</table>

## GoogleCredentials     {#postgresql-cnpg-io-v1-GoogleCredentials}


**Appears in:**

- [BarmanCredentials](#postgresql-cnpg-io-v1-BarmanCredentials)


<p>GoogleCredentials is the type for the Google Cloud Storage credentials.
This needs to be specified even if we run inside a GKE environment.</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>applicationCredentials</code><br/>
<a href="#postgresql-cnpg-io-v1-SecretKeySelector"><i>SecretKeySelector</i></a>
</td>
<td>
   <p>The secret containing the Google Cloud Storage JSON file with the credentials</p>
</td>
</tr>
<tr><td><code>gkeEnvironment</code><br/>
<i>bool</i>
</td>
<td>
   <p>If set to true, will presume that it's running inside a GKE environment,
default to false.</p>
</td>
</tr>
</tbody>
</table>

## ImageCatalogRef     {#postgresql-cnpg-io-v1-ImageCatalogRef}


**Appears in:**

- [ClusterSpec](#postgresql-cnpg-io-v1-ClusterSpec)


<p>ImageCatalogRef defines the reference to a major version in an ImageCatalog</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>TypedLocalObjectReference</code><br/>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#typedlocalobjectreference-v1-core"><i>core/v1.TypedLocalObjectReference</i></a>
</td>
<td>(Members of <code>TypedLocalObjectReference</code> are embedded into this type.)
   <span class="text-muted">No description provided.</span></td>
</tr>
<tr><td><code>major</code> <B>[Required]</B><br/>
<i>int</i>
</td>
<td>
   <p>The major version of PostgreSQL we want to use from the ImageCatalog</p>
</td>
</tr>
</tbody>
</table>

## ImageCatalogSpec     {#postgresql-cnpg-io-v1-ImageCatalogSpec}


**Appears in:**

- [ClusterImageCatalog](#postgresql-cnpg-io-v1-ClusterImageCatalog)

- [ImageCatalog](#postgresql-cnpg-io-v1-ImageCatalog)


<p>ImageCatalogSpec defines the desired ImageCatalog</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>images</code> <B>[Required]</B><br/>
<a href="#postgresql-cnpg-io-v1-CatalogImage"><i>[]CatalogImage</i></a>
</td>
<td>
   <p>List of CatalogImages available in the catalog</p>
</td>
</tr>
</tbody>
</table>

## Import     {#postgresql-cnpg-io-v1-Import}


**Appears in:**

- [BootstrapInitDB](#postgresql-cnpg-io-v1-BootstrapInitDB)


<p>Import contains the configuration to init a database from a logic snapshot of an externalCluster</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>source</code> <B>[Required]</B><br/>
<a href="#postgresql-cnpg-io-v1-ImportSource"><i>ImportSource</i></a>
</td>
<td>
   <p>The source of the import</p>
</td>
</tr>
<tr><td><code>type</code> <B>[Required]</B><br/>
<a href="#postgresql-cnpg-io-v1-SnapshotType"><i>SnapshotType</i></a>
</td>
<td>
   <p>The import type. Can be <code>microservice</code> or <code>monolith</code>.</p>
</td>
</tr>
<tr><td><code>databases</code> <B>[Required]</B><br/>
<i>[]string</i>
</td>
<td>
   <p>The databases to import</p>
</td>
</tr>
<tr><td><code>roles</code><br/>
<i>[]string</i>
</td>
<td>
   <p>The roles to import</p>
</td>
</tr>
<tr><td><code>postImportApplicationSQL</code><br/>
<i>[]string</i>
</td>
<td>
   <p>List of SQL queries to be executed as a superuser in the application
database right after is imported - to be used with extreme care
(by default empty). Only available in microservice type.</p>
</td>
</tr>
<tr><td><code>schemaOnly</code><br/>
<i>bool</i>
</td>
<td>
   <p>When set to true, only the <code>pre-data</code> and <code>post-data</code> sections of
<code>pg_restore</code> are invoked, avoiding data import. Default: <code>false</code>.</p>
</td>
</tr>
</tbody>
</table>

## ImportSource     {#postgresql-cnpg-io-v1-ImportSource}


**Appears in:**

- [Import](#postgresql-cnpg-io-v1-Import)


<p>ImportSource describes the source for the logical snapshot</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>externalCluster</code> <B>[Required]</B><br/>
<i>string</i>
</td>
<td>
   <p>The name of the externalCluster used for import</p>
</td>
</tr>
</tbody>
</table>

## InstanceID     {#postgresql-cnpg-io-v1-InstanceID}


**Appears in:**

- [BackupStatus](#postgresql-cnpg-io-v1-BackupStatus)


<p>InstanceID contains the information to identify an instance</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>podName</code><br/>
<i>string</i>
</td>
<td>
   <p>The pod name</p>
</td>
</tr>
<tr><td><code>ContainerID</code><br/>
<i>string</i>
</td>
<td>
   <p>The container ID</p>
</td>
</tr>
</tbody>
</table>

## InstanceReportedState     {#postgresql-cnpg-io-v1-InstanceReportedState}


**Appears in:**

- [ClusterStatus](#postgresql-cnpg-io-v1-ClusterStatus)


<p>InstanceReportedState describes the last reported state of an instance during a reconciliation loop</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>isPrimary</code> <B>[Required]</B><br/>
<i>bool</i>
</td>
<td>
   <p>indicates if an instance is the primary one</p>
</td>
</tr>
<tr><td><code>timeLineID</code><br/>
<i>int</i>
</td>
<td>
   <p>indicates on which TimelineId the instance is</p>
</td>
</tr>
</tbody>
</table>

## LDAPBindAsAuth     {#postgresql-cnpg-io-v1-LDAPBindAsAuth}


**Appears in:**

- [LDAPConfig](#postgresql-cnpg-io-v1-LDAPConfig)


<p>LDAPBindAsAuth provides the required fields to use the
bind authentication for LDAP</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>prefix</code><br/>
<i>string</i>
</td>
<td>
   <p>Prefix for the bind authentication option</p>
</td>
</tr>
<tr><td><code>suffix</code><br/>
<i>string</i>
</td>
<td>
   <p>Suffix for the bind authentication option</p>
</td>
</tr>
</tbody>
</table>

## LDAPBindSearchAuth     {#postgresql-cnpg-io-v1-LDAPBindSearchAuth}


**Appears in:**

- [LDAPConfig](#postgresql-cnpg-io-v1-LDAPConfig)


<p>LDAPBindSearchAuth provides the required fields to use
the bind+search LDAP authentication process</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>baseDN</code><br/>
<i>string</i>
</td>
<td>
   <p>Root DN to begin the user search</p>
</td>
</tr>
<tr><td><code>bindDN</code><br/>
<i>string</i>
</td>
<td>
   <p>DN of the user to bind to the directory</p>
</td>
</tr>
<tr><td><code>bindPassword</code><br/>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#secretkeyselector-v1-core"><i>core/v1.SecretKeySelector</i></a>
</td>
<td>
   <p>Secret with the password for the user to bind to the directory</p>
</td>
</tr>
<tr><td><code>searchAttribute</code><br/>
<i>string</i>
</td>
<td>
   <p>Attribute to match against the username</p>
</td>
</tr>
<tr><td><code>searchFilter</code><br/>
<i>string</i>
</td>
<td>
   <p>Search filter to use when doing the search+bind authentication</p>
</td>
</tr>
</tbody>
</table>

## LDAPConfig     {#postgresql-cnpg-io-v1-LDAPConfig}


**Appears in:**

- [PostgresConfiguration](#postgresql-cnpg-io-v1-PostgresConfiguration)


<p>LDAPConfig contains the parameters needed for LDAP authentication</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>server</code><br/>
<i>string</i>
</td>
<td>
   <p>LDAP hostname or IP address</p>
</td>
</tr>
<tr><td><code>port</code><br/>
<i>int</i>
</td>
<td>
   <p>LDAP server port</p>
</td>
</tr>
<tr><td><code>scheme</code><br/>
<a href="#postgresql-cnpg-io-v1-LDAPScheme"><i>LDAPScheme</i></a>
</td>
<td>
   <p>LDAP schema to be used, possible options are <code>ldap</code> and <code>ldaps</code></p>
</td>
</tr>
<tr><td><code>bindAsAuth</code><br/>
<a href="#postgresql-cnpg-io-v1-LDAPBindAsAuth"><i>LDAPBindAsAuth</i></a>
</td>
<td>
   <p>Bind as authentication configuration</p>
</td>
</tr>
<tr><td><code>bindSearchAuth</code><br/>
<a href="#postgresql-cnpg-io-v1-LDAPBindSearchAuth"><i>LDAPBindSearchAuth</i></a>
</td>
<td>
   <p>Bind+Search authentication configuration</p>
</td>
</tr>
<tr><td><code>tls</code><br/>
<i>bool</i>
</td>
<td>
   <p>Set to 'true' to enable LDAP over TLS. 'false' is default</p>
</td>
</tr>
</tbody>
</table>

## LDAPScheme     {#postgresql-cnpg-io-v1-LDAPScheme}

(Alias of `string`)

**Appears in:**

- [LDAPConfig](#postgresql-cnpg-io-v1-LDAPConfig)


<p>LDAPScheme defines the possible schemes for LDAP</p>




## LocalObjectReference     {#postgresql-cnpg-io-v1-LocalObjectReference}


**Appears in:**

- [BackupSource](#postgresql-cnpg-io-v1-BackupSource)

- [BackupSpec](#postgresql-cnpg-io-v1-BackupSpec)

- [BootstrapInitDB](#postgresql-cnpg-io-v1-BootstrapInitDB)

- [BootstrapPgBaseBackup](#postgresql-cnpg-io-v1-BootstrapPgBaseBackup)

- [BootstrapRecovery](#postgresql-cnpg-io-v1-BootstrapRecovery)

- [ClusterSpec](#postgresql-cnpg-io-v1-ClusterSpec)

- [ConfigMapKeySelector](#postgresql-cnpg-io-v1-ConfigMapKeySelector)

- [PgBouncerSpec](#postgresql-cnpg-io-v1-PgBouncerSpec)

- [PoolerSpec](#postgresql-cnpg-io-v1-PoolerSpec)

- [RoleConfiguration](#postgresql-cnpg-io-v1-RoleConfiguration)

- [ScheduledBackupSpec](#postgresql-cnpg-io-v1-ScheduledBackupSpec)

- [SecretKeySelector](#postgresql-cnpg-io-v1-SecretKeySelector)


<p>LocalObjectReference contains enough information to let you locate a
local object with a known type inside the same namespace</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>name</code> <B>[Required]</B><br/>
<i>string</i>
</td>
<td>
   <p>Name of the referent.</p>
</td>
</tr>
</tbody>
</table>

## ManagedConfiguration     {#postgresql-cnpg-io-v1-ManagedConfiguration}


**Appears in:**

- [ClusterSpec](#postgresql-cnpg-io-v1-ClusterSpec)


<p>ManagedConfiguration represents the portions of PostgreSQL that are managed
by the instance manager</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>roles</code><br/>
<a href="#postgresql-cnpg-io-v1-RoleConfiguration"><i>[]RoleConfiguration</i></a>
</td>
<td>
   <p>Database roles managed by the <code>Cluster</code></p>
</td>
</tr>
</tbody>
</table>

## ManagedRoles     {#postgresql-cnpg-io-v1-ManagedRoles}


**Appears in:**

- [ClusterStatus](#postgresql-cnpg-io-v1-ClusterStatus)


<p>ManagedRoles tracks the status of a cluster's managed roles</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>byStatus</code><br/>
<i>map[RoleStatus][]string</i>
</td>
<td>
   <p>ByStatus gives the list of roles in each state</p>
</td>
</tr>
<tr><td><code>cannotReconcile</code><br/>
<i>map[string][]string</i>
</td>
<td>
   <p>CannotReconcile lists roles that cannot be reconciled in PostgreSQL,
with an explanation of the cause</p>
</td>
</tr>
<tr><td><code>passwordStatus</code><br/>
<a href="#postgresql-cnpg-io-v1-PasswordState"><i>map[string]PasswordState</i></a>
</td>
<td>
   <p>PasswordStatus gives the last transaction id and password secret version for each managed role</p>
</td>
</tr>
</tbody>
</table>

## Metadata     {#postgresql-cnpg-io-v1-Metadata}


**Appears in:**

- [PodTemplateSpec](#postgresql-cnpg-io-v1-PodTemplateSpec)

- [ServiceAccountTemplate](#postgresql-cnpg-io-v1-ServiceAccountTemplate)

- [ServiceTemplateSpec](#postgresql-cnpg-io-v1-ServiceTemplateSpec)


<p>Metadata is a structure similar to the metav1.ObjectMeta, but still
parseable by controller-gen to create a suitable CRD for the user.
The comment of PodTemplateSpec has an explanation of why we are
not using the core data types.</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>labels</code><br/>
<i>map[string]string</i>
</td>
<td>
   <p>Map of string keys and values that can be used to organize and categorize
(scope and select) objects. May match selectors of replication controllers
and services.
More info: http://kubernetes.io/docs/user-guide/labels</p>
</td>
</tr>
<tr><td><code>annotations</code><br/>
<i>map[string]string</i>
</td>
<td>
   <p>Annotations is an unstructured key value map stored with a resource that may be
set by external tools to store and retrieve arbitrary metadata. They are not
queryable and should be preserved when modifying objects.
More info: http://kubernetes.io/docs/user-guide/annotations</p>
</td>
</tr>
</tbody>
</table>

## MonitoringConfiguration     {#postgresql-cnpg-io-v1-MonitoringConfiguration}


**Appears in:**

- [ClusterSpec](#postgresql-cnpg-io-v1-ClusterSpec)


<p>MonitoringConfiguration is the type containing all the monitoring
configuration for a certain cluster</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>disableDefaultQueries</code><br/>
<i>bool</i>
</td>
<td>
   <p>Whether the default queries should be injected.
Set it to <code>true</code> if you don't want to inject default queries into the cluster.
Default: false.</p>
</td>
</tr>
<tr><td><code>customQueriesConfigMap</code><br/>
<a href="#postgresql-cnpg-io-v1-ConfigMapKeySelector"><i>[]ConfigMapKeySelector</i></a>
</td>
<td>
   <p>The list of config maps containing the custom queries</p>
</td>
</tr>
<tr><td><code>customQueriesSecret</code><br/>
<a href="#postgresql-cnpg-io-v1-SecretKeySelector"><i>[]SecretKeySelector</i></a>
</td>
<td>
   <p>The list of secrets containing the custom queries</p>
</td>
</tr>
<tr><td><code>enablePodMonitor</code><br/>
<i>bool</i>
</td>
<td>
   <p>Enable or disable the <code>PodMonitor</code></p>
</td>
</tr>
<tr><td><code>podMonitorMetricRelabelings</code><br/>
<a href="https://pkg.go.dev/github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1#RelabelConfig"><i>[]github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1.RelabelConfig</i></a>
</td>
<td>
   <p>The list of metric relabelings for the <code>PodMonitor</code>. Applied to samples before ingestion.</p>
</td>
</tr>
<tr><td><code>podMonitorRelabelings</code><br/>
<a href="https://pkg.go.dev/github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1#RelabelConfig"><i>[]github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1.RelabelConfig</i></a>
</td>
<td>
   <p>The list of relabelings for the <code>PodMonitor</code>. Applied to samples before scraping.</p>
</td>
</tr>
</tbody>
</table>

## NodeMaintenanceWindow     {#postgresql-cnpg-io-v1-NodeMaintenanceWindow}


**Appears in:**

- [ClusterSpec](#postgresql-cnpg-io-v1-ClusterSpec)


<p>NodeMaintenanceWindow contains information that the operator
will use while upgrading the underlying node.</p>
<p>This option is only useful when the chosen storage prevents the Pods
from being freely moved across nodes.</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>reusePVC</code><br/>
<i>bool</i>
</td>
<td>
   <p>Reuse the existing PVC (wait for the node to come
up again) or not (recreate it elsewhere - when <code>instances</code> &gt;1)</p>
</td>
</tr>
<tr><td><code>inProgress</code><br/>
<i>bool</i>
</td>
<td>
   <p>Is there a node maintenance activity in progress?</p>
</td>
</tr>
</tbody>
</table>

## OnlineConfiguration     {#postgresql-cnpg-io-v1-OnlineConfiguration}


**Appears in:**

- [BackupSpec](#postgresql-cnpg-io-v1-BackupSpec)

- [ScheduledBackupSpec](#postgresql-cnpg-io-v1-ScheduledBackupSpec)

- [VolumeSnapshotConfiguration](#postgresql-cnpg-io-v1-VolumeSnapshotConfiguration)


<p>OnlineConfiguration contains the configuration parameters for the online volume snapshot</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>waitForArchive</code><br/>
<i>bool</i>
</td>
<td>
   <p>If false, the function will return immediately after the backup is completed,
without waiting for WAL to be archived.
This behavior is only useful with backup software that independently monitors WAL archiving.
Otherwise, WAL required to make the backup consistent might be missing and make the backup useless.
By default, or when this parameter is true, pg_backup_stop will wait for WAL to be archived when archiving is
enabled.
On a standby, this means that it will wait only when archive_mode = always.
If write activity on the primary is low, it may be useful to run pg_switch_wal on the primary in order to trigger
an immediate segment switch.</p>
</td>
</tr>
<tr><td><code>immediateCheckpoint</code><br/>
<i>bool</i>
</td>
<td>
   <p>Control whether the I/O workload for the backup initial checkpoint will
be limited, according to the <code>checkpoint_completion_target</code> setting on
the PostgreSQL server. If set to true, an immediate checkpoint will be
used, meaning PostgreSQL will complete the checkpoint as soon as
possible. <code>false</code> by default.</p>
</td>
</tr>
</tbody>
</table>

## PasswordState     {#postgresql-cnpg-io-v1-PasswordState}


**Appears in:**

- [ManagedRoles](#postgresql-cnpg-io-v1-ManagedRoles)


<p>PasswordState represents the state of the password of a managed RoleConfiguration</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>transactionID</code><br/>
<i>int64</i>
</td>
<td>
   <p>the last transaction ID to affect the role definition in PostgreSQL</p>
</td>
</tr>
<tr><td><code>resourceVersion</code><br/>
<i>string</i>
</td>
<td>
   <p>the resource version of the password secret</p>
</td>
</tr>
</tbody>
</table>

## PgBouncerIntegrationStatus     {#postgresql-cnpg-io-v1-PgBouncerIntegrationStatus}


**Appears in:**

- [PoolerIntegrations](#postgresql-cnpg-io-v1-PoolerIntegrations)


<p>PgBouncerIntegrationStatus encapsulates the needed integration for the pgbouncer poolers referencing the cluster</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>secrets</code><br/>
<i>[]string</i>
</td>
<td>
   <span class="text-muted">No description provided.</span></td>
</tr>
</tbody>
</table>

## PgBouncerPoolMode     {#postgresql-cnpg-io-v1-PgBouncerPoolMode}

(Alias of `string`)

**Appears in:**

- [PgBouncerSpec](#postgresql-cnpg-io-v1-PgBouncerSpec)


<p>PgBouncerPoolMode is the mode of PgBouncer</p>




## PgBouncerSecrets     {#postgresql-cnpg-io-v1-PgBouncerSecrets}


**Appears in:**

- [PoolerSecrets](#postgresql-cnpg-io-v1-PoolerSecrets)


<p>PgBouncerSecrets contains the versions of the secrets used
by pgbouncer</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>authQuery</code><br/>
<a href="#postgresql-cnpg-io-v1-SecretVersion"><i>SecretVersion</i></a>
</td>
<td>
   <p>The auth query secret version</p>
</td>
</tr>
</tbody>
</table>

## PgBouncerSpec     {#postgresql-cnpg-io-v1-PgBouncerSpec}


**Appears in:**

- [PoolerSpec](#postgresql-cnpg-io-v1-PoolerSpec)


<p>PgBouncerSpec defines how to configure PgBouncer</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>poolMode</code><br/>
<a href="#postgresql-cnpg-io-v1-PgBouncerPoolMode"><i>PgBouncerPoolMode</i></a>
</td>
<td>
   <p>The pool mode. Default: <code>session</code>.</p>
</td>
</tr>
<tr><td><code>authQuerySecret</code><br/>
<a href="#postgresql-cnpg-io-v1-LocalObjectReference"><i>LocalObjectReference</i></a>
</td>
<td>
   <p>The credentials of the user that need to be used for the authentication
query. In case it is specified, also an AuthQuery
(e.g. &quot;SELECT usename, passwd FROM pg_catalog.pg_shadow WHERE usename=$1&quot;)
has to be specified and no automatic CNPG Cluster integration will be triggered.</p>
</td>
</tr>
<tr><td><code>authQuery</code><br/>
<i>string</i>
</td>
<td>
   <p>The query that will be used to download the hash of the password
of a certain user. Default: &quot;SELECT usename, passwd FROM public.user_search($1)&quot;.
In case it is specified, also an AuthQuerySecret has to be specified and
no automatic CNPG Cluster integration will be triggered.</p>
</td>
</tr>
<tr><td><code>parameters</code><br/>
<i>map[string]string</i>
</td>
<td>
   <p>Additional parameters to be passed to PgBouncer - please check
the CNPG documentation for a list of options you can configure</p>
</td>
</tr>
<tr><td><code>pg_hba</code><br/>
<i>[]string</i>
</td>
<td>
   <p>PostgreSQL Host Based Authentication rules (lines to be appended
to the pg_hba.conf file)</p>
</td>
</tr>
<tr><td><code>paused</code><br/>
<i>bool</i>
</td>
<td>
   <p>When set to <code>true</code>, PgBouncer will disconnect from the PostgreSQL
server, first waiting for all queries to complete, and pause all new
client connections until this value is set to <code>false</code> (default). Internally,
the operator calls PgBouncer's <code>PAUSE</code> and <code>RESUME</code> commands.</p>
</td>
</tr>
</tbody>
</table>

## PluginStatus     {#postgresql-cnpg-io-v1-PluginStatus}


**Appears in:**

- [ClusterStatus](#postgresql-cnpg-io-v1-ClusterStatus)


<p>PluginStatus is the status of a loaded plugin</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>name</code> <B>[Required]</B><br/>
<i>string</i>
</td>
<td>
   <p>Name is the name of the plugin</p>
</td>
</tr>
<tr><td><code>version</code> <B>[Required]</B><br/>
<i>string</i>
</td>
<td>
   <p>Version is the version of the plugin loaded by the
latest reconciliation loop</p>
</td>
</tr>
<tr><td><code>capabilities</code> <B>[Required]</B><br/>
<i>[]string</i>
</td>
<td>
   <p>Capabilities are the list of capabilities of the
plugin</p>
</td>
</tr>
<tr><td><code>operatorCapabilities</code> <B>[Required]</B><br/>
<i>[]string</i>
</td>
<td>
   <p>OperatorCapabilities are the list of capabilities of the
plugin regarding the reconciler</p>
</td>
</tr>
<tr><td><code>walCapabilities</code> <B>[Required]</B><br/>
<i>[]string</i>
</td>
<td>
   <p>WALCapabilities are the list of capabilities of the
plugin regarding the WAL management</p>
</td>
</tr>
<tr><td><code>backupCapabilities</code> <B>[Required]</B><br/>
<i>[]string</i>
</td>
<td>
   <p>BackupCapabilities are the list of capabilities of the
plugin regarding the Backup management</p>
</td>
</tr>
</tbody>
</table>

## PodTemplateSpec     {#postgresql-cnpg-io-v1-PodTemplateSpec}


**Appears in:**

- [PoolerSpec](#postgresql-cnpg-io-v1-PoolerSpec)


<p>PodTemplateSpec is a structure allowing the user to set
a template for Pod generation.</p>
<p>Unfortunately we can't use the corev1.PodTemplateSpec
type because the generated CRD won't have the field for the
metadata section.</p>
<p>References:
https://github.com/kubernetes-sigs/controller-tools/issues/385
https://github.com/kubernetes-sigs/controller-tools/issues/448
https://github.com/prometheus-operator/prometheus-operator/issues/3041</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>metadata</code><br/>
<a href="#postgresql-cnpg-io-v1-Metadata"><i>Metadata</i></a>
</td>
<td>
   <p>Standard object's metadata.
More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata</p>
</td>
</tr>
<tr><td><code>spec</code><br/>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#podspec-v1-core"><i>core/v1.PodSpec</i></a>
</td>
<td>
   <p>Specification of the desired behavior of the pod.
More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status</p>
</td>
</tr>
</tbody>
</table>

## PodTopologyLabels     {#postgresql-cnpg-io-v1-PodTopologyLabels}

(Alias of `map[string]string`)

**Appears in:**

- [Topology](#postgresql-cnpg-io-v1-Topology)


<p>PodTopologyLabels represent the topology of a Pod. map[labelName]labelValue</p>




## PoolerIntegrations     {#postgresql-cnpg-io-v1-PoolerIntegrations}


**Appears in:**

- [ClusterStatus](#postgresql-cnpg-io-v1-ClusterStatus)


<p>PoolerIntegrations encapsulates the needed integration for the poolers referencing the cluster</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>pgBouncerIntegration</code><br/>
<a href="#postgresql-cnpg-io-v1-PgBouncerIntegrationStatus"><i>PgBouncerIntegrationStatus</i></a>
</td>
<td>
   <span class="text-muted">No description provided.</span></td>
</tr>
</tbody>
</table>

## PoolerMonitoringConfiguration     {#postgresql-cnpg-io-v1-PoolerMonitoringConfiguration}


**Appears in:**

- [PoolerSpec](#postgresql-cnpg-io-v1-PoolerSpec)


<p>PoolerMonitoringConfiguration is the type containing all the monitoring
configuration for a certain Pooler.</p>
<p>Mirrors the Cluster's MonitoringConfiguration but without the custom queries
part for now.</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>enablePodMonitor</code><br/>
<i>bool</i>
</td>
<td>
   <p>Enable or disable the <code>PodMonitor</code></p>
</td>
</tr>
<tr><td><code>podMonitorMetricRelabelings</code><br/>
<a href="https://pkg.go.dev/github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1#RelabelConfig"><i>[]github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1.RelabelConfig</i></a>
</td>
<td>
   <p>The list of metric relabelings for the <code>PodMonitor</code>. Applied to samples before ingestion.</p>
</td>
</tr>
<tr><td><code>podMonitorRelabelings</code><br/>
<a href="https://pkg.go.dev/github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1#RelabelConfig"><i>[]github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1.RelabelConfig</i></a>
</td>
<td>
   <p>The list of relabelings for the <code>PodMonitor</code>. Applied to samples before scraping.</p>
</td>
</tr>
</tbody>
</table>

## PoolerSecrets     {#postgresql-cnpg-io-v1-PoolerSecrets}


**Appears in:**

- [PoolerStatus](#postgresql-cnpg-io-v1-PoolerStatus)


<p>PoolerSecrets contains the versions of all the secrets used</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>serverTLS</code><br/>
<a href="#postgresql-cnpg-io-v1-SecretVersion"><i>SecretVersion</i></a>
</td>
<td>
   <p>The server TLS secret version</p>
</td>
</tr>
<tr><td><code>serverCA</code><br/>
<a href="#postgresql-cnpg-io-v1-SecretVersion"><i>SecretVersion</i></a>
</td>
<td>
   <p>The server CA secret version</p>
</td>
</tr>
<tr><td><code>clientCA</code><br/>
<a href="#postgresql-cnpg-io-v1-SecretVersion"><i>SecretVersion</i></a>
</td>
<td>
   <p>The client CA secret version</p>
</td>
</tr>
<tr><td><code>pgBouncerSecrets</code><br/>
<a href="#postgresql-cnpg-io-v1-PgBouncerSecrets"><i>PgBouncerSecrets</i></a>
</td>
<td>
   <p>The version of the secrets used by PgBouncer</p>
</td>
</tr>
</tbody>
</table>

## PoolerSpec     {#postgresql-cnpg-io-v1-PoolerSpec}


**Appears in:**

- [Pooler](#postgresql-cnpg-io-v1-Pooler)


<p>PoolerSpec defines the desired state of Pooler</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>cluster</code> <B>[Required]</B><br/>
<a href="#postgresql-cnpg-io-v1-LocalObjectReference"><i>LocalObjectReference</i></a>
</td>
<td>
   <p>This is the cluster reference on which the Pooler will work.
Pooler name should never match with any cluster name within the same namespace.</p>
</td>
</tr>
<tr><td><code>type</code><br/>
<a href="#postgresql-cnpg-io-v1-PoolerType"><i>PoolerType</i></a>
</td>
<td>
   <p>Type of service to forward traffic to. Default: <code>rw</code>.</p>
</td>
</tr>
<tr><td><code>instances</code><br/>
<i>int32</i>
</td>
<td>
   <p>The number of replicas we want. Default: 1.</p>
</td>
</tr>
<tr><td><code>template</code><br/>
<a href="#postgresql-cnpg-io-v1-PodTemplateSpec"><i>PodTemplateSpec</i></a>
</td>
<td>
   <p>The template of the Pod to be created</p>
</td>
</tr>
<tr><td><code>pgbouncer</code> <B>[Required]</B><br/>
<a href="#postgresql-cnpg-io-v1-PgBouncerSpec"><i>PgBouncerSpec</i></a>
</td>
<td>
   <p>The PgBouncer configuration</p>
</td>
</tr>
<tr><td><code>deploymentStrategy</code><br/>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#deploymentstrategy-v1-apps"><i>apps/v1.DeploymentStrategy</i></a>
</td>
<td>
   <p>The deployment strategy to use for pgbouncer to replace existing pods with new ones</p>
</td>
</tr>
<tr><td><code>monitoring</code><br/>
<a href="#postgresql-cnpg-io-v1-PoolerMonitoringConfiguration"><i>PoolerMonitoringConfiguration</i></a>
</td>
<td>
   <p>The configuration of the monitoring infrastructure of this pooler.</p>
</td>
</tr>
<tr><td><code>serviceTemplate</code><br/>
<a href="#postgresql-cnpg-io-v1-ServiceTemplateSpec"><i>ServiceTemplateSpec</i></a>
</td>
<td>
   <p>Template for the Service to be created</p>
</td>
</tr>
</tbody>
</table>

## PoolerStatus     {#postgresql-cnpg-io-v1-PoolerStatus}


**Appears in:**

- [Pooler](#postgresql-cnpg-io-v1-Pooler)


<p>PoolerStatus defines the observed state of Pooler</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>secrets</code><br/>
<a href="#postgresql-cnpg-io-v1-PoolerSecrets"><i>PoolerSecrets</i></a>
</td>
<td>
   <p>The resource version of the config object</p>
</td>
</tr>
<tr><td><code>instances</code><br/>
<i>int32</i>
</td>
<td>
   <p>The number of pods trying to be scheduled</p>
</td>
</tr>
</tbody>
</table>

## PoolerType     {#postgresql-cnpg-io-v1-PoolerType}

(Alias of `string`)

**Appears in:**

- [PoolerSpec](#postgresql-cnpg-io-v1-PoolerSpec)


<p>PoolerType is the type of the connection pool, meaning the service
we are targeting. Allowed values are <code>rw</code> and <code>ro</code>.</p>




## PostInitApplicationSQLRefs     {#postgresql-cnpg-io-v1-PostInitApplicationSQLRefs}


**Appears in:**

- [BootstrapInitDB](#postgresql-cnpg-io-v1-BootstrapInitDB)


<p>PostInitApplicationSQLRefs points references to ConfigMaps or Secrets which
contain SQL files, the general implementation order to these references is
from all Secrets to all ConfigMaps, and inside Secrets or ConfigMaps,
the implementation order is same as the order of each array</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>secretRefs</code><br/>
<a href="#postgresql-cnpg-io-v1-SecretKeySelector"><i>[]SecretKeySelector</i></a>
</td>
<td>
   <p>SecretRefs holds a list of references to Secrets</p>
</td>
</tr>
<tr><td><code>configMapRefs</code><br/>
<a href="#postgresql-cnpg-io-v1-ConfigMapKeySelector"><i>[]ConfigMapKeySelector</i></a>
</td>
<td>
   <p>ConfigMapRefs holds a list of references to ConfigMaps</p>
</td>
</tr>
</tbody>
</table>

## PostgresConfiguration     {#postgresql-cnpg-io-v1-PostgresConfiguration}


**Appears in:**

- [ClusterSpec](#postgresql-cnpg-io-v1-ClusterSpec)


<p>PostgresConfiguration defines the PostgreSQL configuration</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>parameters</code><br/>
<i>map[string]string</i>
</td>
<td>
   <p>PostgreSQL configuration options (postgresql.conf)</p>
</td>
</tr>
<tr><td><code>pg_hba</code><br/>
<i>[]string</i>
</td>
<td>
   <p>PostgreSQL Host Based Authentication rules (lines to be appended
to the pg_hba.conf file)</p>
</td>
</tr>
<tr><td><code>pg_ident</code><br/>
<i>[]string</i>
</td>
<td>
   <p>PostgreSQL User Name Maps rules (lines to be appended
to the pg_ident.conf file)</p>
</td>
</tr>
<tr><td><code>syncReplicaElectionConstraint</code><br/>
<a href="#postgresql-cnpg-io-v1-SyncReplicaElectionConstraints"><i>SyncReplicaElectionConstraints</i></a>
</td>
<td>
   <p>Requirements to be met by sync replicas. This will affect how the &quot;synchronous_standby_names&quot; parameter will be
set up.</p>
</td>
</tr>
<tr><td><code>shared_preload_libraries</code><br/>
<i>[]string</i>
</td>
<td>
   <p>Lists of shared preload libraries to add to the default ones</p>
</td>
</tr>
<tr><td><code>ldap</code><br/>
<a href="#postgresql-cnpg-io-v1-LDAPConfig"><i>LDAPConfig</i></a>
</td>
<td>
   <p>Options to specify LDAP configuration</p>
</td>
</tr>
<tr><td><code>promotionTimeout</code><br/>
<i>int32</i>
</td>
<td>
   <p>Specifies the maximum number of seconds to wait when promoting an instance to primary.
Default value is 40000000, greater than one year in seconds,
big enough to simulate an infinite timeout</p>
</td>
</tr>
<tr><td><code>enableAlterSystem</code><br/>
<i>bool</i>
</td>
<td>
   <p>If this parameter is true, the user will be able to invoke <code>ALTER SYSTEM</code>
on this CloudNativePG Cluster.
This should only be used for debugging and troubleshooting.
Defaults to false.</p>
</td>
</tr>
</tbody>
</table>

## PrimaryUpdateMethod     {#postgresql-cnpg-io-v1-PrimaryUpdateMethod}

(Alias of `string`)

**Appears in:**

- [ClusterSpec](#postgresql-cnpg-io-v1-ClusterSpec)


<p>PrimaryUpdateMethod contains the method to use when upgrading
the primary server of the cluster as part of rolling updates</p>




## PrimaryUpdateStrategy     {#postgresql-cnpg-io-v1-PrimaryUpdateStrategy}

(Alias of `string`)

**Appears in:**

- [ClusterSpec](#postgresql-cnpg-io-v1-ClusterSpec)


<p>PrimaryUpdateStrategy contains the strategy to follow when upgrading
the primary server of the cluster as part of rolling updates</p>




## RecoveryTarget     {#postgresql-cnpg-io-v1-RecoveryTarget}


**Appears in:**

- [BootstrapRecovery](#postgresql-cnpg-io-v1-BootstrapRecovery)


<p>RecoveryTarget allows to configure the moment where the recovery process
will stop. All the target options except TargetTLI are mutually exclusive.</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>backupID</code><br/>
<i>string</i>
</td>
<td>
   <p>The ID of the backup from which to start the recovery process.
If empty (default) the operator will automatically detect the backup
based on targetTime or targetLSN if specified. Otherwise use the
latest available backup in chronological order.</p>
</td>
</tr>
<tr><td><code>targetTLI</code><br/>
<i>string</i>
</td>
<td>
   <p>The target timeline (&quot;latest&quot; or a positive integer)</p>
</td>
</tr>
<tr><td><code>targetXID</code><br/>
<i>string</i>
</td>
<td>
   <p>The target transaction ID</p>
</td>
</tr>
<tr><td><code>targetName</code><br/>
<i>string</i>
</td>
<td>
   <p>The target name (to be previously created
with <code>pg_create_restore_point</code>)</p>
</td>
</tr>
<tr><td><code>targetLSN</code><br/>
<i>string</i>
</td>
<td>
   <p>The target LSN (Log Sequence Number)</p>
</td>
</tr>
<tr><td><code>targetTime</code><br/>
<i>string</i>
</td>
<td>
   <p>The target time as a timestamp in the RFC3339 standard</p>
</td>
</tr>
<tr><td><code>targetImmediate</code><br/>
<i>bool</i>
</td>
<td>
   <p>End recovery as soon as a consistent state is reached</p>
</td>
</tr>
<tr><td><code>exclusive</code><br/>
<i>bool</i>
</td>
<td>
   <p>Set the target to be exclusive. If omitted, defaults to false, so that
in Postgres, <code>recovery_target_inclusive</code> will be true</p>
</td>
</tr>
</tbody>
</table>

## ReplicaClusterConfiguration     {#postgresql-cnpg-io-v1-ReplicaClusterConfiguration}


**Appears in:**

- [ClusterSpec](#postgresql-cnpg-io-v1-ClusterSpec)


<p>ReplicaClusterConfiguration encapsulates the configuration of a replica
cluster</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>source</code> <B>[Required]</B><br/>
<i>string</i>
</td>
<td>
   <p>The name of the external cluster which is the replication origin</p>
</td>
</tr>
<tr><td><code>enabled</code> <B>[Required]</B><br/>
<i>bool</i>
</td>
<td>
   <p>If replica mode is enabled, this cluster will be a replica of an
existing cluster. Replica cluster can be created from a recovery
object store or via streaming through pg_basebackup.
Refer to the Replica clusters page of the documentation for more information.</p>
</td>
</tr>
</tbody>
</table>

## ReplicationSlotsConfiguration     {#postgresql-cnpg-io-v1-ReplicationSlotsConfiguration}


**Appears in:**

- [ClusterSpec](#postgresql-cnpg-io-v1-ClusterSpec)


<p>ReplicationSlotsConfiguration encapsulates the configuration
of replication slots</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>highAvailability</code><br/>
<a href="#postgresql-cnpg-io-v1-ReplicationSlotsHAConfiguration"><i>ReplicationSlotsHAConfiguration</i></a>
</td>
<td>
   <p>Replication slots for high availability configuration</p>
</td>
</tr>
<tr><td><code>updateInterval</code><br/>
<i>int</i>
</td>
<td>
   <p>Standby will update the status of the local replication slots
every <code>updateInterval</code> seconds (default 30).</p>
</td>
</tr>
<tr><td><code>synchronizeReplicas</code><br/>
<a href="#postgresql-cnpg-io-v1-SynchronizeReplicasConfiguration"><i>SynchronizeReplicasConfiguration</i></a>
</td>
<td>
   <p>Configures the synchronization of the user defined physical replication slots</p>
</td>
</tr>
</tbody>
</table>

## ReplicationSlotsHAConfiguration     {#postgresql-cnpg-io-v1-ReplicationSlotsHAConfiguration}


**Appears in:**

- [ReplicationSlotsConfiguration](#postgresql-cnpg-io-v1-ReplicationSlotsConfiguration)


<p>ReplicationSlotsHAConfiguration encapsulates the configuration
of the replication slots that are automatically managed by
the operator to control the streaming replication connections
with the standby instances for high availability (HA) purposes.
Replication slots are a PostgreSQL feature that makes sure
that PostgreSQL automatically keeps WAL files in the primary
when a streaming client (in this specific case a replica that
is part of the HA cluster) gets disconnected.</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>enabled</code><br/>
<i>bool</i>
</td>
<td>
   <p>If enabled (default), the operator will automatically manage replication slots
on the primary instance and use them in streaming replication
connections with all the standby instances that are part of the HA
cluster. If disabled, the operator will not take advantage
of replication slots in streaming connections with the replicas.
This feature also controls replication slots in replica cluster,
from the designated primary to its cascading replicas.</p>
</td>
</tr>
<tr><td><code>slotPrefix</code><br/>
<i>string</i>
</td>
<td>
   <p>Prefix for replication slots managed by the operator for HA.
It may only contain lower case letters, numbers, and the underscore character.
This can only be set at creation time. By default set to <code>_cnpg_</code>.</p>
</td>
</tr>
</tbody>
</table>

## RoleConfiguration     {#postgresql-cnpg-io-v1-RoleConfiguration}


**Appears in:**

- [ManagedConfiguration](#postgresql-cnpg-io-v1-ManagedConfiguration)


<p>RoleConfiguration is the representation, in Kubernetes, of a PostgreSQL role
with the additional field Ensure specifying whether to ensure the presence or
absence of the role in the database</p>
<p>The defaults of the CREATE ROLE command are applied
Reference: https://www.postgresql.org/docs/current/sql-createrole.html</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>name</code> <B>[Required]</B><br/>
<i>string</i>
</td>
<td>
   <p>Name of the role</p>
</td>
</tr>
<tr><td><code>comment</code><br/>
<i>string</i>
</td>
<td>
   <p>Description of the role</p>
</td>
</tr>
<tr><td><code>ensure</code><br/>
<a href="#postgresql-cnpg-io-v1-EnsureOption"><i>EnsureOption</i></a>
</td>
<td>
   <p>Ensure the role is <code>present</code> or <code>absent</code> - defaults to &quot;present&quot;</p>
</td>
</tr>
<tr><td><code>passwordSecret</code><br/>
<a href="#postgresql-cnpg-io-v1-LocalObjectReference"><i>LocalObjectReference</i></a>
</td>
<td>
   <p>Secret containing the password of the role (if present)
If null, the password will be ignored unless DisablePassword is set</p>
</td>
</tr>
<tr><td><code>connectionLimit</code><br/>
<i>int64</i>
</td>
<td>
   <p>If the role can log in, this specifies how many concurrent
connections the role can make. <code>-1</code> (the default) means no limit.</p>
</td>
</tr>
<tr><td><code>validUntil</code><br/>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#time-v1-meta"><i>meta/v1.Time</i></a>
</td>
<td>
   <p>Date and time after which the role's password is no longer valid.
When omitted, the password will never expire (default).</p>
</td>
</tr>
<tr><td><code>inRoles</code><br/>
<i>[]string</i>
</td>
<td>
   <p>List of one or more existing roles to which this role will be
immediately added as a new member. Default empty.</p>
</td>
</tr>
<tr><td><code>inherit</code><br/>
<i>bool</i>
</td>
<td>
   <p>Whether a role &quot;inherits&quot; the privileges of roles it is a member of.
Defaults is <code>true</code>.</p>
</td>
</tr>
<tr><td><code>disablePassword</code><br/>
<i>bool</i>
</td>
<td>
   <p>DisablePassword indicates that a role's password should be set to NULL in Postgres</p>
</td>
</tr>
<tr><td><code>superuser</code><br/>
<i>bool</i>
</td>
<td>
   <p>Whether the role is a <code>superuser</code> who can override all access
restrictions within the database - superuser status is dangerous and
should be used only when really needed. You must yourself be a
superuser to create a new superuser. Defaults is <code>false</code>.</p>
</td>
</tr>
<tr><td><code>createdb</code><br/>
<i>bool</i>
</td>
<td>
   <p>When set to <code>true</code>, the role being defined will be allowed to create
new databases. Specifying <code>false</code> (default) will deny a role the
ability to create databases.</p>
</td>
</tr>
<tr><td><code>createrole</code><br/>
<i>bool</i>
</td>
<td>
   <p>Whether the role will be permitted to create, alter, drop, comment
on, change the security label for, and grant or revoke membership in
other roles. Default is <code>false</code>.</p>
</td>
</tr>
<tr><td><code>login</code><br/>
<i>bool</i>
</td>
<td>
   <p>Whether the role is allowed to log in. A role having the <code>login</code>
attribute can be thought of as a user. Roles without this attribute
are useful for managing database privileges, but are not users in
the usual sense of the word. Default is <code>false</code>.</p>
</td>
</tr>
<tr><td><code>replication</code><br/>
<i>bool</i>
</td>
<td>
   <p>Whether a role is a replication role. A role must have this
attribute (or be a superuser) in order to be able to connect to the
server in replication mode (physical or logical replication) and in
order to be able to create or drop replication slots. A role having
the <code>replication</code> attribute is a very highly privileged role, and
should only be used on roles actually used for replication. Default
is <code>false</code>.</p>
</td>
</tr>
<tr><td><code>bypassrls</code><br/>
<i>bool</i>
</td>
<td>
   <p>Whether a role bypasses every row-level security (RLS) policy.
Default is <code>false</code>.</p>
</td>
</tr>
</tbody>
</table>

## S3Credentials     {#postgresql-cnpg-io-v1-S3Credentials}


**Appears in:**

- [BarmanCredentials](#postgresql-cnpg-io-v1-BarmanCredentials)


<p>S3Credentials is the type for the credentials to be used to upload
files to S3. It can be provided in two alternative ways:</p>
<ul>
<li>
<p>explicitly passing accessKeyId and secretAccessKey</p>
</li>
<li>
<p>inheriting the role from the pod environment by setting inheritFromIAMRole to true</p>
</li>
</ul>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>accessKeyId</code><br/>
<a href="#postgresql-cnpg-io-v1-SecretKeySelector"><i>SecretKeySelector</i></a>
</td>
<td>
   <p>The reference to the access key id</p>
</td>
</tr>
<tr><td><code>secretAccessKey</code><br/>
<a href="#postgresql-cnpg-io-v1-SecretKeySelector"><i>SecretKeySelector</i></a>
</td>
<td>
   <p>The reference to the secret access key</p>
</td>
</tr>
<tr><td><code>region</code><br/>
<a href="#postgresql-cnpg-io-v1-SecretKeySelector"><i>SecretKeySelector</i></a>
</td>
<td>
   <p>The reference to the secret containing the region name</p>
</td>
</tr>
<tr><td><code>sessionToken</code><br/>
<a href="#postgresql-cnpg-io-v1-SecretKeySelector"><i>SecretKeySelector</i></a>
</td>
<td>
   <p>The references to the session key</p>
</td>
</tr>
<tr><td><code>inheritFromIAMRole</code><br/>
<i>bool</i>
</td>
<td>
   <p>Use the role based authentication without providing explicitly the keys.</p>
</td>
</tr>
</tbody>
</table>

## ScheduledBackupSpec     {#postgresql-cnpg-io-v1-ScheduledBackupSpec}


**Appears in:**

- [ScheduledBackup](#postgresql-cnpg-io-v1-ScheduledBackup)


<p>ScheduledBackupSpec defines the desired state of ScheduledBackup</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>suspend</code><br/>
<i>bool</i>
</td>
<td>
   <p>If this backup is suspended or not</p>
</td>
</tr>
<tr><td><code>immediate</code><br/>
<i>bool</i>
</td>
<td>
   <p>If the first backup has to be immediately start after creation or not</p>
</td>
</tr>
<tr><td><code>schedule</code> <B>[Required]</B><br/>
<i>string</i>
</td>
<td>
   <p>The schedule does not follow the same format used in Kubernetes CronJobs
as it includes an additional seconds specifier,
see https://pkg.go.dev/github.com/robfig/cron#hdr-CRON_Expression_Format</p>
</td>
</tr>
<tr><td><code>cluster</code> <B>[Required]</B><br/>
<a href="#postgresql-cnpg-io-v1-LocalObjectReference"><i>LocalObjectReference</i></a>
</td>
<td>
   <p>The cluster to backup</p>
</td>
</tr>
<tr><td><code>backupOwnerReference</code><br/>
<i>string</i>
</td>
<td>
   <p>Indicates which ownerReference should be put inside the created backup resources.<!-- raw HTML omitted --></p>
<ul>
<li>none: no owner reference for created backup objects (same behavior as before the field was introduced)<!-- raw HTML omitted --></li>
<li>self: sets the Scheduled backup object as owner of the backup<!-- raw HTML omitted --></li>
<li>cluster: set the cluster as owner of the backup<!-- raw HTML omitted --></li>
</ul>
</td>
</tr>
<tr><td><code>target</code><br/>
<a href="#postgresql-cnpg-io-v1-BackupTarget"><i>BackupTarget</i></a>
</td>
<td>
   <p>The policy to decide which instance should perform this backup. If empty,
it defaults to <code>cluster.spec.backup.target</code>.
Available options are empty string, <code>primary</code> and <code>prefer-standby</code>.
<code>primary</code> to have backups run always on primary instances,
<code>prefer-standby</code> to have backups run preferably on the most updated
standby, if available.</p>
</td>
</tr>
<tr><td><code>method</code><br/>
<a href="#postgresql-cnpg-io-v1-BackupMethod"><i>BackupMethod</i></a>
</td>
<td>
   <p>The backup method to be used, possible options are <code>barmanObjectStore</code>
and <code>volumeSnapshot</code>. Defaults to: <code>barmanObjectStore</code>.</p>
</td>
</tr>
<tr><td><code>pluginConfiguration</code><br/>
<a href="#postgresql-cnpg-io-v1-BackupPluginConfiguration"><i>BackupPluginConfiguration</i></a>
</td>
<td>
   <p>Configuration parameters passed to the plugin managing this backup</p>
</td>
</tr>
<tr><td><code>online</code><br/>
<i>bool</i>
</td>
<td>
   <p>Whether the default type of backup with volume snapshots is
online/hot (<code>true</code>, default) or offline/cold (<code>false</code>)
Overrides the default setting specified in the cluster field '.spec.backup.volumeSnapshot.online'</p>
</td>
</tr>
<tr><td><code>onlineConfiguration</code><br/>
<a href="#postgresql-cnpg-io-v1-OnlineConfiguration"><i>OnlineConfiguration</i></a>
</td>
<td>
   <p>Configuration parameters to control the online/hot backup with volume snapshots
Overrides the default settings specified in the cluster '.backup.volumeSnapshot.onlineConfiguration' stanza</p>
</td>
</tr>
</tbody>
</table>

## ScheduledBackupStatus     {#postgresql-cnpg-io-v1-ScheduledBackupStatus}


**Appears in:**

- [ScheduledBackup](#postgresql-cnpg-io-v1-ScheduledBackup)


<p>ScheduledBackupStatus defines the observed state of ScheduledBackup</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>lastCheckTime</code><br/>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#time-v1-meta"><i>meta/v1.Time</i></a>
</td>
<td>
   <p>The latest time the schedule</p>
</td>
</tr>
<tr><td><code>lastScheduleTime</code><br/>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#time-v1-meta"><i>meta/v1.Time</i></a>
</td>
<td>
   <p>Information when was the last time that backup was successfully scheduled.</p>
</td>
</tr>
<tr><td><code>nextScheduleTime</code><br/>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#time-v1-meta"><i>meta/v1.Time</i></a>
</td>
<td>
   <p>Next time we will run a backup</p>
</td>
</tr>
</tbody>
</table>

## SecretKeySelector     {#postgresql-cnpg-io-v1-SecretKeySelector}


**Appears in:**

- [AzureCredentials](#postgresql-cnpg-io-v1-AzureCredentials)

- [BackupSource](#postgresql-cnpg-io-v1-BackupSource)

- [BackupStatus](#postgresql-cnpg-io-v1-BackupStatus)

- [BarmanObjectStoreConfiguration](#postgresql-cnpg-io-v1-BarmanObjectStoreConfiguration)

- [GoogleCredentials](#postgresql-cnpg-io-v1-GoogleCredentials)

- [MonitoringConfiguration](#postgresql-cnpg-io-v1-MonitoringConfiguration)

- [PostInitApplicationSQLRefs](#postgresql-cnpg-io-v1-PostInitApplicationSQLRefs)

- [S3Credentials](#postgresql-cnpg-io-v1-S3Credentials)


<p>SecretKeySelector contains enough information to let you locate
the key of a Secret</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>LocalObjectReference</code><br/>
<a href="#postgresql-cnpg-io-v1-LocalObjectReference"><i>LocalObjectReference</i></a>
</td>
<td>(Members of <code>LocalObjectReference</code> are embedded into this type.)
   <p>The name of the secret in the pod's namespace to select from.</p>
</td>
</tr>
<tr><td><code>key</code> <B>[Required]</B><br/>
<i>string</i>
</td>
<td>
   <p>The key to select</p>
</td>
</tr>
</tbody>
</table>

## SecretVersion     {#postgresql-cnpg-io-v1-SecretVersion}


**Appears in:**

- [PgBouncerSecrets](#postgresql-cnpg-io-v1-PgBouncerSecrets)

- [PoolerSecrets](#postgresql-cnpg-io-v1-PoolerSecrets)


<p>SecretVersion contains a secret name and its ResourceVersion</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>name</code><br/>
<i>string</i>
</td>
<td>
   <p>The name of the secret</p>
</td>
</tr>
<tr><td><code>version</code><br/>
<i>string</i>
</td>
<td>
   <p>The ResourceVersion of the secret</p>
</td>
</tr>
</tbody>
</table>

## SecretsResourceVersion     {#postgresql-cnpg-io-v1-SecretsResourceVersion}


**Appears in:**

- [ClusterStatus](#postgresql-cnpg-io-v1-ClusterStatus)


<p>SecretsResourceVersion is the resource versions of the secrets
managed by the operator</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>superuserSecretVersion</code><br/>
<i>string</i>
</td>
<td>
   <p>The resource version of the &quot;postgres&quot; user secret</p>
</td>
</tr>
<tr><td><code>replicationSecretVersion</code><br/>
<i>string</i>
</td>
<td>
   <p>The resource version of the &quot;streaming_replica&quot; user secret</p>
</td>
</tr>
<tr><td><code>applicationSecretVersion</code><br/>
<i>string</i>
</td>
<td>
   <p>The resource version of the &quot;app&quot; user secret</p>
</td>
</tr>
<tr><td><code>managedRoleSecretVersion</code><br/>
<i>map[string]string</i>
</td>
<td>
   <p>The resource versions of the managed roles secrets</p>
</td>
</tr>
<tr><td><code>caSecretVersion</code><br/>
<i>string</i>
</td>
<td>
   <p>Unused. Retained for compatibility with old versions.</p>
</td>
</tr>
<tr><td><code>clientCaSecretVersion</code><br/>
<i>string</i>
</td>
<td>
   <p>The resource version of the PostgreSQL client-side CA secret version</p>
</td>
</tr>
<tr><td><code>serverCaSecretVersion</code><br/>
<i>string</i>
</td>
<td>
   <p>The resource version of the PostgreSQL server-side CA secret version</p>
</td>
</tr>
<tr><td><code>serverSecretVersion</code><br/>
<i>string</i>
</td>
<td>
   <p>The resource version of the PostgreSQL server-side secret version</p>
</td>
</tr>
<tr><td><code>barmanEndpointCA</code><br/>
<i>string</i>
</td>
<td>
   <p>The resource version of the Barman Endpoint CA if provided</p>
</td>
</tr>
<tr><td><code>externalClusterSecretVersion</code><br/>
<i>map[string]string</i>
</td>
<td>
   <p>The resource versions of the external cluster secrets</p>
</td>
</tr>
<tr><td><code>metrics</code><br/>
<i>map[string]string</i>
</td>
<td>
   <p>A map with the versions of all the secrets used to pass metrics.
Map keys are the secret names, map values are the versions</p>
</td>
</tr>
</tbody>
</table>

## ServiceAccountTemplate     {#postgresql-cnpg-io-v1-ServiceAccountTemplate}


**Appears in:**

- [ClusterSpec](#postgresql-cnpg-io-v1-ClusterSpec)


<p>ServiceAccountTemplate contains the template needed to generate the service accounts</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>metadata</code> <B>[Required]</B><br/>
<a href="#postgresql-cnpg-io-v1-Metadata"><i>Metadata</i></a>
</td>
<td>
   <p>Metadata are the metadata to be used for the generated
service account</p>
</td>
</tr>
</tbody>
</table>

## ServiceTemplateSpec     {#postgresql-cnpg-io-v1-ServiceTemplateSpec}


**Appears in:**

- [PoolerSpec](#postgresql-cnpg-io-v1-PoolerSpec)


<p>ServiceTemplateSpec is a structure allowing the user to set
a template for Service generation.</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>metadata</code><br/>
<a href="#postgresql-cnpg-io-v1-Metadata"><i>Metadata</i></a>
</td>
<td>
   <p>Standard object's metadata.
More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata</p>
</td>
</tr>
<tr><td><code>spec</code><br/>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#servicespec-v1-core"><i>core/v1.ServiceSpec</i></a>
</td>
<td>
   <p>Specification of the desired behavior of the service.
More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status</p>
</td>
</tr>
</tbody>
</table>

## SnapshotOwnerReference     {#postgresql-cnpg-io-v1-SnapshotOwnerReference}

(Alias of `string`)

**Appears in:**

- [VolumeSnapshotConfiguration](#postgresql-cnpg-io-v1-VolumeSnapshotConfiguration)


<p>SnapshotOwnerReference defines the reference type for the owner of the snapshot.
This specifies which owner the processed resources should relate to.</p>




## SnapshotType     {#postgresql-cnpg-io-v1-SnapshotType}

(Alias of `string`)

**Appears in:**

- [Import](#postgresql-cnpg-io-v1-Import)


<p>SnapshotType is a type of allowed import</p>




## StorageConfiguration     {#postgresql-cnpg-io-v1-StorageConfiguration}


**Appears in:**

- [ClusterSpec](#postgresql-cnpg-io-v1-ClusterSpec)

- [TablespaceConfiguration](#postgresql-cnpg-io-v1-TablespaceConfiguration)


<p>StorageConfiguration is the configuration used to create and reconcile PVCs,
usable for WAL volumes, PGDATA volumes, or tablespaces</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>storageClass</code><br/>
<i>string</i>
</td>
<td>
   <p>StorageClass to use for PVCs. Applied after
evaluating the PVC template, if available.
If not specified, the generated PVCs will use the
default storage class</p>
</td>
</tr>
<tr><td><code>size</code><br/>
<i>string</i>
</td>
<td>
   <p>Size of the storage. Required if not already specified in the PVC template.
Changes to this field are automatically reapplied to the created PVCs.
Size cannot be decreased.</p>
</td>
</tr>
<tr><td><code>resizeInUseVolumes</code><br/>
<i>bool</i>
</td>
<td>
   <p>Resize existent PVCs, defaults to true</p>
</td>
</tr>
<tr><td><code>pvcTemplate</code><br/>
<a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#persistentvolumeclaimspec-v1-core"><i>core/v1.PersistentVolumeClaimSpec</i></a>
</td>
<td>
   <p>Template to be used to generate the Persistent Volume Claim</p>
</td>
</tr>
</tbody>
</table>

## SwitchReplicaClusterStatus     {#postgresql-cnpg-io-v1-SwitchReplicaClusterStatus}


**Appears in:**

- [ClusterStatus](#postgresql-cnpg-io-v1-ClusterStatus)


<p>SwitchReplicaClusterStatus contains all the statuses regarding the switch of a cluster to a replica cluster</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>inProgress</code><br/>
<i>bool</i>
</td>
<td>
   <p>InProgress indicates if there is an ongoing procedure of switching a cluster to a replica cluster.</p>
</td>
</tr>
</tbody>
</table>

## SyncReplicaElectionConstraints     {#postgresql-cnpg-io-v1-SyncReplicaElectionConstraints}


**Appears in:**

- [PostgresConfiguration](#postgresql-cnpg-io-v1-PostgresConfiguration)


<p>SyncReplicaElectionConstraints contains the constraints for sync replicas election.</p>
<p>For anti-affinity parameters two instances are considered in the same location
if all the labels values match.</p>
<p>In future synchronous replica election restriction by name will be supported.</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>nodeLabelsAntiAffinity</code><br/>
<i>[]string</i>
</td>
<td>
   <p>A list of node labels values to extract and compare to evaluate if the pods reside in the same topology or not</p>
</td>
</tr>
<tr><td><code>enabled</code> <B>[Required]</B><br/>
<i>bool</i>
</td>
<td>
   <p>This flag enables the constraints for sync replicas</p>
</td>
</tr>
</tbody>
</table>

## SynchronizeReplicasConfiguration     {#postgresql-cnpg-io-v1-SynchronizeReplicasConfiguration}


**Appears in:**

- [ReplicationSlotsConfiguration](#postgresql-cnpg-io-v1-ReplicationSlotsConfiguration)


<p>SynchronizeReplicasConfiguration contains the configuration for the synchronization of user defined
physical replication slots</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>enabled</code> <B>[Required]</B><br/>
<i>bool</i>
</td>
<td>
   <p>When set to true, every replication slot that is on the primary is synchronized on each standby</p>
</td>
</tr>
<tr><td><code>excludePatterns</code><br/>
<i>[]string</i>
</td>
<td>
   <p>List of regular expression patterns to match the names of replication slots to be excluded (by default empty)</p>
</td>
</tr>
<tr><td><code>-</code> <B>[Required]</B><br/>
<a href="#postgresql-cnpg-io-v1-synchronizeReplicasCache"><i>synchronizeReplicasCache</i></a>
</td>
<td>
   <span class="text-muted">No description provided.</span></td>
</tr>
</tbody>
</table>

## TablespaceConfiguration     {#postgresql-cnpg-io-v1-TablespaceConfiguration}


**Appears in:**

- [ClusterSpec](#postgresql-cnpg-io-v1-ClusterSpec)


<p>TablespaceConfiguration is the configuration of a tablespace, and includes
the storage specification for the tablespace</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>name</code> <B>[Required]</B><br/>
<i>string</i>
</td>
<td>
   <p>The name of the tablespace</p>
</td>
</tr>
<tr><td><code>storage</code> <B>[Required]</B><br/>
<a href="#postgresql-cnpg-io-v1-StorageConfiguration"><i>StorageConfiguration</i></a>
</td>
<td>
   <p>The storage configuration for the tablespace</p>
</td>
</tr>
<tr><td><code>owner</code><br/>
<a href="#postgresql-cnpg-io-v1-DatabaseRoleRef"><i>DatabaseRoleRef</i></a>
</td>
<td>
   <p>Owner is the PostgreSQL user owning the tablespace</p>
</td>
</tr>
<tr><td><code>temporary</code><br/>
<i>bool</i>
</td>
<td>
   <p>When set to true, the tablespace will be added as a <code>temp_tablespaces</code>
entry in PostgreSQL, and will be available to automatically house temp
database objects, or other temporary files. Please refer to PostgreSQL
documentation for more information on the <code>temp_tablespaces</code> GUC.</p>
</td>
</tr>
</tbody>
</table>

## TablespaceState     {#postgresql-cnpg-io-v1-TablespaceState}


**Appears in:**

- [ClusterStatus](#postgresql-cnpg-io-v1-ClusterStatus)


<p>TablespaceState represents the state of a tablespace in a cluster</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>name</code> <B>[Required]</B><br/>
<i>string</i>
</td>
<td>
   <p>Name is the name of the tablespace</p>
</td>
</tr>
<tr><td><code>owner</code><br/>
<i>string</i>
</td>
<td>
   <p>Owner is the PostgreSQL user owning the tablespace</p>
</td>
</tr>
<tr><td><code>state</code> <B>[Required]</B><br/>
<a href="#postgresql-cnpg-io-v1-TablespaceStatus"><i>TablespaceStatus</i></a>
</td>
<td>
   <p>State is the latest reconciliation state</p>
</td>
</tr>
<tr><td><code>error</code><br/>
<i>string</i>
</td>
<td>
   <p>Error is the reconciliation error, if any</p>
</td>
</tr>
</tbody>
</table>

## TablespaceStatus     {#postgresql-cnpg-io-v1-TablespaceStatus}

(Alias of `string`)

**Appears in:**

- [TablespaceState](#postgresql-cnpg-io-v1-TablespaceState)


<p>TablespaceStatus represents the status of a tablespace in the cluster</p>




## Topology     {#postgresql-cnpg-io-v1-Topology}


**Appears in:**

- [ClusterStatus](#postgresql-cnpg-io-v1-ClusterStatus)


<p>Topology contains the cluster topology</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>instances</code><br/>
<a href="#postgresql-cnpg-io-v1-PodTopologyLabels"><i>map[PodName]PodTopologyLabels</i></a>
</td>
<td>
   <p>Instances contains the pod topology of the instances</p>
</td>
</tr>
<tr><td><code>nodesUsed</code><br/>
<i>int32</i>
</td>
<td>
   <p>NodesUsed represents the count of distinct nodes accommodating the instances.
A value of '1' suggests that all instances are hosted on a single node,
implying the absence of High Availability (HA). Ideally, this value should
be the same as the number of instances in the Postgres HA cluster, implying
shared nothing architecture on the compute side.</p>
</td>
</tr>
<tr><td><code>successfullyExtracted</code><br/>
<i>bool</i>
</td>
<td>
   <p>SuccessfullyExtracted indicates if the topology data was extract. It is useful to enact fallback behaviors
in synchronous replica election in case of failures</p>
</td>
</tr>
</tbody>
</table>

## VolumeSnapshotConfiguration     {#postgresql-cnpg-io-v1-VolumeSnapshotConfiguration}


**Appears in:**

- [BackupConfiguration](#postgresql-cnpg-io-v1-BackupConfiguration)


<p>VolumeSnapshotConfiguration represents the configuration for the execution of snapshot backups.</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>labels</code><br/>
<i>map[string]string</i>
</td>
<td>
   <p>Labels are key-value pairs that will be added to .metadata.labels snapshot resources.</p>
</td>
</tr>
<tr><td><code>annotations</code><br/>
<i>map[string]string</i>
</td>
<td>
   <p>Annotations key-value pairs that will be added to .metadata.annotations snapshot resources.</p>
</td>
</tr>
<tr><td><code>className</code><br/>
<i>string</i>
</td>
<td>
   <p>ClassName specifies the Snapshot Class to be used for PG_DATA PersistentVolumeClaim.
It is the default class for the other types if no specific class is present</p>
</td>
</tr>
<tr><td><code>walClassName</code><br/>
<i>string</i>
</td>
<td>
   <p>WalClassName specifies the Snapshot Class to be used for the PG_WAL PersistentVolumeClaim.</p>
</td>
</tr>
<tr><td><code>tablespaceClassName</code><br/>
<i>map[string]string</i>
</td>
<td>
   <p>TablespaceClassName specifies the Snapshot Class to be used for the tablespaces.
defaults to the PGDATA Snapshot Class, if set</p>
</td>
</tr>
<tr><td><code>snapshotOwnerReference</code><br/>
<a href="#postgresql-cnpg-io-v1-SnapshotOwnerReference"><i>SnapshotOwnerReference</i></a>
</td>
<td>
   <p>SnapshotOwnerReference indicates the type of owner reference the snapshot should have</p>
</td>
</tr>
<tr><td><code>online</code><br/>
<i>bool</i>
</td>
<td>
   <p>Whether the default type of backup with volume snapshots is
online/hot (<code>true</code>, default) or offline/cold (<code>false</code>)</p>
</td>
</tr>
<tr><td><code>onlineConfiguration</code><br/>
<a href="#postgresql-cnpg-io-v1-OnlineConfiguration"><i>OnlineConfiguration</i></a>
</td>
<td>
   <p>Configuration parameters to control the online/hot backup with volume snapshots</p>
</td>
</tr>
</tbody>
</table>

## WalBackupConfiguration     {#postgresql-cnpg-io-v1-WalBackupConfiguration}


**Appears in:**

- [BarmanObjectStoreConfiguration](#postgresql-cnpg-io-v1-BarmanObjectStoreConfiguration)


<p>WalBackupConfiguration is the configuration of the backup of the
WAL stream</p>


<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
<tr><td><code>compression</code><br/>
<a href="#postgresql-cnpg-io-v1-CompressionType"><i>CompressionType</i></a>
</td>
<td>
   <p>Compress a WAL file before sending it to the object store. Available
options are empty string (no compression, default), <code>gzip</code>, <code>bzip2</code> or <code>snappy</code>.</p>
</td>
</tr>
<tr><td><code>encryption</code><br/>
<a href="#postgresql-cnpg-io-v1-EncryptionType"><i>EncryptionType</i></a>
</td>
<td>
   <p>Whenever to force the encryption of files (if the bucket is
not already configured for that).
Allowed options are empty string (use the bucket policy, default),
<code>AES256</code> and <code>aws:kms</code></p>
</td>
</tr>
<tr><td><code>maxParallel</code><br/>
<i>int</i>
</td>
<td>
   <p>Number of WAL files to be either archived in parallel (when the
PostgreSQL instance is archiving to a backup object store) or
restored in parallel (when a PostgreSQL standby is fetching WAL
files from a recovery object store). If not specified, WAL files
will be processed one at a time. It accepts a positive integer as a
value - with 1 being the minimum accepted value.</p>
</td>
</tr>
</tbody>
</table>