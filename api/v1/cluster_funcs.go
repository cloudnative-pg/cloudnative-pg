/*
Copyright © contributors to CloudNativePG, established as
CloudNativePG a Series of LF Projects, LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
*/

package v1

import (
	"context"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/cloudnative-pg/cnpg-i/pkg/identity"
	"github.com/cloudnative-pg/machinery/pkg/image/reference"
	"github.com/cloudnative-pg/machinery/pkg/log"
	pgTime "github.com/cloudnative-pg/machinery/pkg/postgres/time"
	"github.com/cloudnative-pg/machinery/pkg/postgres/version"
	"github.com/cloudnative-pg/machinery/pkg/stringset"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/system"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	contextutils "github.com/cloudnative-pg/cloudnative-pg/pkg/utils/context"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/versions"
)

// GetOnline tells whether this volume snapshot configuration allows
// online backups
func (configuration *VolumeSnapshotConfiguration) GetOnline() bool {
	if configuration.Online == nil {
		return true
	}

	return *configuration.Online
}

// GetWaitForArchive tells whether to wait for archive or not
func (o OnlineConfiguration) GetWaitForArchive() bool {
	if o.WaitForArchive == nil {
		return true
	}

	return *o.WaitForArchive
}

// GetImmediateCheckpoint tells whether to execute an immediate checkpoint
func (o OnlineConfiguration) GetImmediateCheckpoint() bool {
	if o.ImmediateCheckpoint == nil {
		return false
	}

	return *o.ImmediateCheckpoint
}

// GetPluginConfigurationEnabledPluginNames gets the name of the plugins that are involved
// in the reconciliation of this cluster
func GetPluginConfigurationEnabledPluginNames(pluginList []PluginConfiguration) (result []string) {
	pluginNames := make([]string, 0, len(pluginList))
	for _, pluginDeclaration := range pluginList {
		if pluginDeclaration.IsEnabled() {
			pluginNames = append(pluginNames, pluginDeclaration.Name)
		}
	}
	return pluginNames
}

// GetInstanceEnabledPluginNames gets the name of the plugins that are available to the instance container
func (cluster *Cluster) GetInstanceEnabledPluginNames() (result []string) {
	var instance []string
	for _, pluginStatus := range cluster.Status.PluginStatus {
		if slices.Contains(pluginStatus.Capabilities,
			identity.PluginCapability_Service_TYPE_INSTANCE_SIDECAR_INJECTION.String()) {
			instance = append(instance, pluginStatus.Name)
		}
	}

	enabled := GetPluginConfigurationEnabledPluginNames(cluster.Spec.Plugins)

	var instanceEnabled []string
	for _, pluginName := range instance {
		if slices.Contains(enabled, pluginName) {
			instanceEnabled = append(instanceEnabled, pluginName)
		}
	}

	return instanceEnabled
}

// GetJobEnabledPluginNames gets the name of the plugins that are available to the job container
func (cluster *Cluster) GetJobEnabledPluginNames() (result []string) {
	var instance []string
	for _, pluginStatus := range cluster.Status.PluginStatus {
		if slices.Contains(pluginStatus.Capabilities,
			identity.PluginCapability_Service_TYPE_INSTANCE_JOB_SIDECAR_INJECTION.String()) {
			instance = append(instance, pluginStatus.Name)
		}
	}

	enabled := GetPluginConfigurationEnabledPluginNames(cluster.Spec.Plugins)

	var instanceEnabled []string
	for _, pluginName := range instance {
		if slices.Contains(enabled, pluginName) {
			instanceEnabled = append(instanceEnabled, pluginName)
		}
	}

	return instanceEnabled
}

// GetExternalClustersEnabledPluginNames gets the name of the plugins that are
// involved in the reconciliation of this external cluster list. This
// list is usually composed by the plugins that need to be active to
// recover data from the external clusters.
func GetExternalClustersEnabledPluginNames(externalClusters []ExternalCluster) (result []string) {
	pluginNames := make([]string, 0, len(externalClusters))
	for _, externalCluster := range externalClusters {
		if externalCluster.PluginConfiguration != nil {
			pluginNames = append(pluginNames, externalCluster.PluginConfiguration.Name)
		}
	}
	return pluginNames
}

// GetShmLimit gets the `/dev/shm` memory size limit
func (e *EphemeralVolumesSizeLimitConfiguration) GetShmLimit() *resource.Quantity {
	if e == nil {
		return nil
	}

	return e.Shm
}

// GetTemporaryDataLimit gets the temporary storage size limit
func (e *EphemeralVolumesSizeLimitConfiguration) GetTemporaryDataLimit() *resource.Quantity {
	if e == nil {
		return nil
	}

	return e.TemporaryData
}

// MergeMetadata adds the passed custom annotations and labels in the service account.
func (st *ServiceAccountTemplate) MergeMetadata(sa *corev1.ServiceAccount) {
	if st == nil {
		return
	}
	if sa.Labels == nil {
		sa.Labels = map[string]string{}
	}
	if sa.Annotations == nil {
		sa.Annotations = map[string]string{}
	}

	maps.Copy(sa.Labels, st.Metadata.Labels)
	maps.Copy(sa.Annotations, st.Metadata.Annotations)
}

// MatchesTopology checks if the two topologies have
// the same label values (labels are specified in SyncReplicaElectionConstraints.NodeLabelsAntiAffinity)
func (topologyLabels PodTopologyLabels) MatchesTopology(instanceTopology PodTopologyLabels) bool {
	for mainLabelName, mainLabelValue := range topologyLabels {
		if mainLabelValue != instanceTopology[mainLabelName] {
			return false
		}
	}
	return true
}

// GetAvailableArchitecture returns an AvailableArchitecture given it's name. It returns nil if it's not found.
func (status *ClusterStatus) GetAvailableArchitecture(archName string) *AvailableArchitecture {
	for _, architecture := range status.AvailableArchitectures {
		if architecture.GoArch == archName {
			return &architecture
		}
	}
	return nil
}

type regexErrors struct {
	errs []error
}

func (r regexErrors) Error() string {
	if len(r.errs) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("failed to compile regex patterns: ")
	for _, err := range r.errs {
		sb.WriteString(err.Error())
		sb.WriteString("; ")
	}
	return sb.String()
}

func (r *SynchronizeReplicasConfiguration) compileRegex() ([]regexp.Regexp, error) {
	if r == nil {
		return nil, nil
	}

	var (
		compiledPatterns = make([]regexp.Regexp, len(r.ExcludePatterns))
		compileErrors    []error
	)

	for idx, pattern := range r.ExcludePatterns {
		re, err := regexp.Compile(pattern)
		if err != nil {
			compileErrors = append(compileErrors, err)
			continue
		}
		compiledPatterns[idx] = *re
	}

	if len(compileErrors) > 0 {
		return nil, regexErrors{errs: compileErrors}
	}

	return compiledPatterns, nil
}

// GetEnabled returns false if synchronized replication slots are disabled, defaults to true
func (r *SynchronizeReplicasConfiguration) GetEnabled() bool {
	if r != nil && r.Enabled != nil {
		return *r.Enabled
	}
	return true
}

// ValidateRegex returns all the errors that happened during the regex compilation
func (r *SynchronizeReplicasConfiguration) ValidateRegex() error {
	_, err := r.compileRegex()
	return err
}

// IsExcludedByUser returns if a replication slot should not be reconciled on the replicas
func (r *SynchronizeReplicasConfiguration) IsExcludedByUser(slotName string) (bool, error) {
	if r == nil {
		return false, nil
	}

	compiledPatterns, err := r.compileRegex()
	// this is an unexpected issue, validation should happen at webhook level
	if err != nil {
		return false, err
	}

	for _, re := range compiledPatterns {
		if re.MatchString(slotName) {
			return true, nil
		}
	}

	return false, nil
}

// GetEnabled returns false if replication slots are disabled, default is true
func (r *ReplicationSlotsConfiguration) GetEnabled() bool {
	return r.SynchronizeReplicas.GetEnabled() || r.HighAvailability.GetEnabled()
}

// GetUpdateInterval returns the update interval, defaulting to DefaultReplicationSlotsUpdateInterval if empty
func (r *ReplicationSlotsConfiguration) GetUpdateInterval() time.Duration {
	if r == nil || r.UpdateInterval <= 0 {
		return DefaultReplicationSlotsUpdateInterval
	}
	return time.Duration(r.UpdateInterval) * time.Second
}

// GetSlotPrefix returns the HA slot prefix, defaulting to DefaultReplicationSlotsHASlotPrefix if empty
func (r *ReplicationSlotsHAConfiguration) GetSlotPrefix() string {
	if r == nil || r.SlotPrefix == "" {
		return DefaultReplicationSlotsHASlotPrefix
	}
	return r.SlotPrefix
}

// GetSlotNameFromInstanceName returns the slot name, given the instance name.
// It returns an empty string if High Availability Replication Slots are disabled
func (r *ReplicationSlotsHAConfiguration) GetSlotNameFromInstanceName(instanceName string) string {
	if r == nil || !r.GetEnabled() {
		return ""
	}

	slotName := fmt.Sprintf(
		"%s%s",
		r.GetSlotPrefix(),
		instanceName,
	)
	sanitizedName := slotNameNegativeRegex.ReplaceAllString(strings.ToLower(slotName), "_")

	return sanitizedName
}

// GetEnabled returns false if replication slots are disabled, default is true
func (r *ReplicationSlotsHAConfiguration) GetEnabled() bool {
	if r != nil && r.Enabled != nil {
		return *r.Enabled
	}
	return true
}

// GetSynchronizeLogicalDecoding returns true if logical slot synchronization is configured.
// This requires both HighAvailability slots to be enabled and SynchronizeLogicalDecoding
// to be set to true. When true on PostgreSQL 17+, the cluster is configured for native
// slot synchronization from primary to standbys.
func (r *ReplicationSlotsHAConfiguration) GetSynchronizeLogicalDecoding() bool {
	if r == nil {
		return false
	}
	return r.GetEnabled() && r.SynchronizeLogicalDecoding
}

// ToPostgreSQLConfigurationKeyword returns the contained value as a valid PostgreSQL parameter to be injected
// in the 'synchronous_standby_names' field
func (s SynchronousReplicaConfigurationMethod) ToPostgreSQLConfigurationKeyword() string {
	return strings.ToUpper(string(s))
}

func (c *CertificatesConfiguration) getServerAltDNSNames() []string {
	if c == nil {
		return nil
	}

	return c.ServerAltDNSNames
}

// HasElements returns true if it contains any Reference
func (s *SQLRefs) HasElements() bool {
	if s == nil {
		return false
	}

	return len(s.ConfigMapRefs) != 0 ||
		len(s.SecretRefs) != 0
}

// GetBackupID gets the backup ID
func (target *RecoveryTarget) GetBackupID() string {
	return target.BackupID
}

// GetTargetTime gets the target time
func (target *RecoveryTarget) GetTargetTime() string {
	return target.TargetTime
}

// GetTargetLSN gets the target LSN
func (target *RecoveryTarget) GetTargetLSN() string {
	return target.TargetLSN
}

// GetTargetTLI gets the target timeline
func (target *RecoveryTarget) GetTargetTLI() string {
	return target.TargetTLI
}

// GetSizeOrNil returns the requests storage size
func (s *StorageConfiguration) GetSizeOrNil() *resource.Quantity {
	if s == nil {
		return nil
	}

	if s.Size != "" {
		quantity, err := resource.ParseQuantity(s.Size)
		if err != nil {
			return nil
		}

		return &quantity
	}

	if s.PersistentVolumeClaimTemplate != nil {
		return s.PersistentVolumeClaimTemplate.Resources.Requests.Storage()
	}

	return nil
}

// AreDefaultQueriesDisabled checks whether default monitoring queries should be disabled
func (m *MonitoringConfiguration) AreDefaultQueriesDisabled() bool {
	return m != nil && m.DisableDefaultQueries != nil && *m.DisableDefaultQueries
}

// GetServerName returns the server name, defaulting to the name of the external cluster or using the one specified
// in the BarmanObjectStore
func (in ExternalCluster) GetServerName() string {
	if in.BarmanObjectStore != nil && in.BarmanObjectStore.ServerName != "" {
		return in.BarmanObjectStore.ServerName
	}
	return in.Name
}

// IsEnabled returns true when this plugin is enabled
func (config *PluginConfiguration) IsEnabled() bool {
	if config.Enabled == nil {
		return true
	}
	return *config.Enabled
}

// GetRoleSecretsName gets the name of the secret which is used to store the role's password
func (roleConfiguration *RoleConfiguration) GetRoleSecretsName() string {
	if roleConfiguration.PasswordSecret != nil {
		return roleConfiguration.PasswordSecret.Name
	}
	return ""
}

// GetRoleInherit return the inherit attribute of a roleConfiguration
func (roleConfiguration *RoleConfiguration) GetRoleInherit() bool {
	if roleConfiguration.Inherit != nil {
		return *roleConfiguration.Inherit
	}
	return true
}

// SetManagedRoleSecretVersion Add or update or delete the resource version of the managed role secret
func (secretResourceVersion *SecretsResourceVersion) SetManagedRoleSecretVersion(secret string, version *string) {
	if secretResourceVersion.ManagedRoleSecretVersions == nil {
		secretResourceVersion.ManagedRoleSecretVersions = make(map[string]string)
	}
	if version == nil {
		delete(secretResourceVersion.ManagedRoleSecretVersions, secret)
	} else {
		secretResourceVersion.ManagedRoleSecretVersions[secret] = *version
	}
}

// SetExternalClusterSecretVersion Add or update or delete the resource version of the secret used in external clusters
func (secretResourceVersion *SecretsResourceVersion) SetExternalClusterSecretVersion(
	secretName string,
	version *string,
) {
	if secretResourceVersion.ExternalClusterSecretVersions == nil {
		secretResourceVersion.ExternalClusterSecretVersions = make(map[string]string)
	}

	if version == nil {
		delete(secretResourceVersion.ExternalClusterSecretVersions, secretName)
		return
	}

	secretResourceVersion.ExternalClusterSecretVersions[secretName] = *version
}

// SetInContext records the cluster in the given context
func (cluster *Cluster) SetInContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, contextutils.ContextKeyCluster, cluster)
}

// GetPostgresqlMajorVersion gets the PostgreSQL image major version detecting it from the
// image name or from the ImageCatalogRef.
func (cluster *Cluster) GetPostgresqlMajorVersion() (int, error) {
	if cluster.Spec.ImageCatalogRef != nil {
		return cluster.Spec.ImageCatalogRef.Major, nil
	}

	if cluster.Spec.ImageName != "" {
		imgVersion, err := version.FromTag(reference.New(cluster.Spec.ImageName).Tag)
		if err != nil {
			return 0, fmt.Errorf("cannot parse image name %q: %w", cluster.Spec.ImageName, err)
		}
		return int(imgVersion.Major()), nil //nolint:gosec
	}

	// Fallback for unit tests where a cluster is created without status or defaults
	imgVersion, err := version.FromTag(reference.New(configuration.Current.PostgresImageName).Tag)
	if err != nil {
		return 0, fmt.Errorf("cannot parse default image name %q: %w", configuration.Current.PostgresImageName, err)
	}
	return int(imgVersion.Major()), nil //nolint:gosec
}

// GetImagePullSecret get the name of the pull secret to use
// to download the PostgreSQL image
func (cluster *Cluster) GetImagePullSecret() string {
	return cluster.Name + ClusterSecretSuffix
}

// GetSuperuserSecretName get the secret name of the PostgreSQL superuser
func (cluster *Cluster) GetSuperuserSecretName() string {
	if cluster.Spec.SuperuserSecret != nil &&
		cluster.Spec.SuperuserSecret.Name != "" {
		return cluster.Spec.SuperuserSecret.Name
	}

	return fmt.Sprintf("%v%v", cluster.Name, SuperUserSecretSuffix)
}

// GetEnableLDAPAuth return true if bind or bind+search method are
// configured in the cluster configuration
func (cluster *Cluster) GetEnableLDAPAuth() bool {
	if cluster.Spec.PostgresConfiguration.LDAP != nil &&
		(cluster.Spec.PostgresConfiguration.LDAP.BindAsAuth != nil ||
			cluster.Spec.PostgresConfiguration.LDAP.BindSearchAuth != nil) {
		return true
	}
	return false
}

// GetLDAPSecretName gets the secret name containing the LDAP password
func (cluster *Cluster) GetLDAPSecretName() string {
	if cluster.Spec.PostgresConfiguration.LDAP != nil &&
		cluster.Spec.PostgresConfiguration.LDAP.BindSearchAuth != nil &&
		cluster.Spec.PostgresConfiguration.LDAP.BindSearchAuth.BindPassword != nil {
		return cluster.Spec.PostgresConfiguration.LDAP.BindSearchAuth.BindPassword.Name
	}
	return ""
}

// ContainsManagedRolesConfiguration returns true iff there are managed roles configured
func (cluster *Cluster) ContainsManagedRolesConfiguration() bool {
	return cluster.Spec.Managed != nil && len(cluster.Spec.Managed.Roles) > 0
}

// GetExternalClusterSecrets returns the secrets used by external Clusters
func (cluster *Cluster) GetExternalClusterSecrets() *stringset.Data {
	secrets := stringset.New()

	if cluster.Spec.ExternalClusters != nil {
		for _, externalCluster := range cluster.Spec.ExternalClusters {
			if externalCluster.Password != nil {
				secrets.Put(externalCluster.Password.Name)
			}
			if externalCluster.SSLKey != nil {
				secrets.Put(externalCluster.SSLKey.Name)
			}
			if externalCluster.SSLCert != nil {
				secrets.Put(externalCluster.SSLCert.Name)
			}
			if externalCluster.SSLRootCert != nil {
				secrets.Put(externalCluster.SSLRootCert.Name)
			}
		}
	}
	return secrets
}

// UsesSecretInManagedRoles checks if the given secret name is used in a managed role
func (cluster *Cluster) UsesSecretInManagedRoles(secretName string) bool {
	if !cluster.ContainsManagedRolesConfiguration() {
		return false
	}
	for _, role := range cluster.Spec.Managed.Roles {
		if role.PasswordSecret != nil && role.PasswordSecret.Name == secretName {
			return true
		}
	}
	return false
}

// GetApplicationSecretName get the name of the application secret for any bootstrap type
func (cluster *Cluster) GetApplicationSecretName() string {
	bootstrap := cluster.Spec.Bootstrap
	if bootstrap == nil {
		return fmt.Sprintf("%v%v", cluster.Name, ApplicationUserSecretSuffix)
	}
	recovery := bootstrap.Recovery
	if recovery != nil && recovery.Secret != nil && recovery.Secret.Name != "" {
		return recovery.Secret.Name
	}

	pgBaseBackup := bootstrap.PgBaseBackup
	if pgBaseBackup != nil && pgBaseBackup.Secret != nil && pgBaseBackup.Secret.Name != "" {
		return pgBaseBackup.Secret.Name
	}

	initDB := bootstrap.InitDB
	if initDB != nil && initDB.Secret != nil && initDB.Secret.Name != "" {
		return initDB.Secret.Name
	}

	return fmt.Sprintf("%v%v", cluster.Name, ApplicationUserSecretSuffix)
}

// GetApplicationDatabaseName get the name of the application database for a specific bootstrap
func (cluster *Cluster) GetApplicationDatabaseName() string {
	bootstrap := cluster.Spec.Bootstrap
	if bootstrap == nil {
		return ""
	}

	if bootstrap.Recovery != nil && bootstrap.Recovery.Database != "" {
		return bootstrap.Recovery.Database
	}

	if bootstrap.PgBaseBackup != nil && bootstrap.PgBaseBackup.Database != "" {
		return bootstrap.PgBaseBackup.Database
	}

	if bootstrap.InitDB != nil && bootstrap.InitDB.Database != "" {
		return bootstrap.InitDB.Database
	}

	return ""
}

// GetApplicationDatabaseOwner get the owner user of the application database for a specific bootstrap
func (cluster *Cluster) GetApplicationDatabaseOwner() string {
	bootstrap := cluster.Spec.Bootstrap
	if bootstrap == nil {
		return ""
	}

	if bootstrap.Recovery != nil && bootstrap.Recovery.Owner != "" {
		return bootstrap.Recovery.Owner
	}

	if bootstrap.PgBaseBackup != nil && bootstrap.PgBaseBackup.Owner != "" {
		return bootstrap.PgBaseBackup.Owner
	}

	if bootstrap.InitDB != nil && bootstrap.InitDB.Owner != "" {
		return bootstrap.InitDB.Owner
	}

	return ""
}

// GetServerCASecretName get the name of the secret containing the CA
// of the cluster
func (cluster *Cluster) GetServerCASecretName() string {
	if cluster.Spec.Certificates != nil && cluster.Spec.Certificates.ServerCASecret != "" {
		return cluster.Spec.Certificates.ServerCASecret
	}
	return fmt.Sprintf("%v%v", cluster.Name, DefaultServerCaSecretSuffix)
}

// GetServerTLSSecretName get the name of the secret containing the
// certificate that is used for the PostgreSQL servers
func (cluster *Cluster) GetServerTLSSecretName() string {
	if cluster.Spec.Certificates != nil && cluster.Spec.Certificates.ServerTLSSecret != "" {
		return cluster.Spec.Certificates.ServerTLSSecret
	}
	return fmt.Sprintf("%v%v", cluster.Name, ServerSecretSuffix)
}

// GetClientCASecretName get the name of the secret containing the CA
// of the cluster
func (cluster *Cluster) GetClientCASecretName() string {
	if cluster.Spec.Certificates != nil && cluster.Spec.Certificates.ClientCASecret != "" {
		return cluster.Spec.Certificates.ClientCASecret
	}
	return fmt.Sprintf("%v%v", cluster.Name, ClientCaSecretSuffix)
}

// GetFixedInheritedAnnotations gets the annotations that should be
// inherited by all resources according to the cluster spec and the operator version
func (cluster *Cluster) GetFixedInheritedAnnotations() map[string]string {
	var meta metav1.ObjectMeta
	utils.SetOperatorVersion(&meta, versions.Version)

	if cluster.Spec.InheritedMetadata == nil || cluster.Spec.InheritedMetadata.Annotations == nil {
		return meta.Annotations
	}

	maps.Copy(meta.Annotations, cluster.Spec.InheritedMetadata.Annotations)

	return meta.Annotations
}

// GetFixedInheritedLabels gets the labels that should be
// inherited by all resources according the cluster spec
func (cluster *Cluster) GetFixedInheritedLabels() map[string]string {
	if cluster.Spec.InheritedMetadata == nil || cluster.Spec.InheritedMetadata.Labels == nil {
		return nil
	}
	return cluster.Spec.InheritedMetadata.Labels
}

// GetReplicationSecretName get the name of the secret for the replication user
func (cluster *Cluster) GetReplicationSecretName() string {
	if cluster.Spec.Certificates != nil && cluster.Spec.Certificates.ReplicationTLSSecret != "" {
		return cluster.Spec.Certificates.ReplicationTLSSecret
	}
	return fmt.Sprintf("%v%v", cluster.Name, ReplicationSecretSuffix)
}

// GetServiceAnyName return the name of the service that is used as DNS
// domain for all the nodes, even if they are not ready
func (cluster *Cluster) GetServiceAnyName() string {
	return fmt.Sprintf("%v%v", cluster.Name, ServiceAnySuffix)
}

// GetServiceReadName return the default name of the service that is used for
// read transactions (including the primary)
func (cluster *Cluster) GetServiceReadName() string {
	return fmt.Sprintf("%v%v", cluster.Name, ServiceReadSuffix)
}

// GetServiceReadOnlyName return the default name of the service that is used for
// read-only transactions (excluding the primary)
func (cluster *Cluster) GetServiceReadOnlyName() string {
	return fmt.Sprintf("%v%v", cluster.Name, ServiceReadOnlySuffix)
}

// GetServiceReadWriteName return the default name of the service that is used for
// read-write transactions
func (cluster *Cluster) GetServiceReadWriteName() string {
	return fmt.Sprintf("%v%v", cluster.Name, ServiceReadWriteSuffix)
}

// GetMaxStartDelay get the amount of time of startDelay config option
func (cluster *Cluster) GetMaxStartDelay() int32 {
	if cluster.Spec.MaxStartDelay > 0 {
		return cluster.Spec.MaxStartDelay
	}
	return DefaultStartupDelay
}

// GetMaxStopDelay get the amount of time PostgreSQL has to stop
func (cluster *Cluster) GetMaxStopDelay() int32 {
	if cluster.Spec.MaxStopDelay > 0 {
		return cluster.Spec.MaxStopDelay
	}
	return 1800
}

// GetSmartShutdownTimeout is used to ensure that smart shutdown timeout is a positive integer
func (cluster *Cluster) GetSmartShutdownTimeout() int32 {
	if cluster.Spec.SmartShutdownTimeout != nil {
		return *cluster.Spec.SmartShutdownTimeout
	}
	return 180
}

// GetRestartTimeout is used to have a timeout for operations that involve
// a restart of a PostgreSQL instance
func (cluster *Cluster) GetRestartTimeout() time.Duration {
	return time.Duration(cluster.GetMaxStopDelay()+cluster.GetMaxStartDelay()) * time.Second
}

// GetMaxSwitchoverDelay get the amount of time PostgreSQL has to stop before switchover
func (cluster *Cluster) GetMaxSwitchoverDelay() int32 {
	if cluster.Spec.MaxSwitchoverDelay > 0 {
		return cluster.Spec.MaxSwitchoverDelay
	}
	return DefaultMaxSwitchoverDelay
}

// GetPrimaryUpdateStrategy get the cluster primary update strategy,
// defaulting to unsupervised
func (cluster *Cluster) GetPrimaryUpdateStrategy() PrimaryUpdateStrategy {
	strategy := cluster.Spec.PrimaryUpdateStrategy
	if strategy == "" {
		return PrimaryUpdateStrategyUnsupervised
	}

	return strategy
}

// GetPrimaryUpdateMethod get the cluster primary update method,
// defaulting to restart
func (cluster *Cluster) GetPrimaryUpdateMethod() PrimaryUpdateMethod {
	strategy := cluster.Spec.PrimaryUpdateMethod
	if strategy == "" {
		return PrimaryUpdateMethodRestart
	}

	return strategy
}

// GetEnablePDB get the cluster EnablePDB value, defaults to true
func (cluster *Cluster) GetEnablePDB() bool {
	if cluster.Spec.EnablePDB == nil {
		return true
	}

	return *cluster.Spec.EnablePDB
}

// IsNodeMaintenanceWindowInProgress check if the upgrade mode is active or not
func (cluster *Cluster) IsNodeMaintenanceWindowInProgress() bool {
	return cluster.Spec.NodeMaintenanceWindow != nil && cluster.Spec.NodeMaintenanceWindow.InProgress
}

// GetPgCtlTimeoutForPromotion returns the timeout that should be waited for an instance to be promoted
// to primary. As default, DefaultPgCtlTimeoutForPromotion is big enough to simulate an infinite timeout
func (cluster *Cluster) GetPgCtlTimeoutForPromotion() int32 {
	timeout := cluster.Spec.PostgresConfiguration.PgCtlTimeoutForPromotion
	if timeout == 0 {
		return DefaultPgCtlTimeoutForPromotion
	}
	return timeout
}

// IsReusePVCEnabled check if in a maintenance window we should reuse PVCs
func (cluster *Cluster) IsReusePVCEnabled() bool {
	reusePVC := true
	if cluster.Spec.NodeMaintenanceWindow != nil && cluster.Spec.NodeMaintenanceWindow.ReusePVC != nil {
		reusePVC = *cluster.Spec.NodeMaintenanceWindow.ReusePVC
	}
	return reusePVC
}

// IsInstanceFenced check if in a given instance should be fenced
func (cluster *Cluster) IsInstanceFenced(instance string) bool {
	fencedInstances, err := utils.GetFencedInstances(cluster.Annotations)
	if err != nil {
		return false
	}

	if fencedInstances.Has(utils.FenceAllInstances) {
		return true
	}
	return fencedInstances.Has(instance)
}

// ShouldResizeInUseVolumes is true when we should resize PVC we already
// created
func (cluster *Cluster) ShouldResizeInUseVolumes() bool {
	if cluster.Spec.StorageConfiguration.ResizeInUseVolumes == nil {
		return true
	}

	return *cluster.Spec.StorageConfiguration.ResizeInUseVolumes
}

// ShouldCreateApplicationSecret returns true if for this cluster,
// during the bootstrap phase, we need to create a secret to store application credentials
func (cluster *Cluster) ShouldCreateApplicationSecret() bool {
	return cluster.ShouldInitDBCreateApplicationSecret() ||
		cluster.ShouldPgBaseBackupCreateApplicationSecret() ||
		cluster.ShouldRecoveryCreateApplicationSecret()
}

// ShouldInitDBCreateApplicationSecret returns true if for this cluster,
// during the bootstrap phase using initDB, we need to create an new application secret
func (cluster *Cluster) ShouldInitDBCreateApplicationSecret() bool {
	return cluster.ShouldInitDBCreateApplicationDatabase() &&
		(cluster.Spec.Bootstrap.InitDB.Secret == nil ||
			cluster.Spec.Bootstrap.InitDB.Secret.Name == "")
}

// ShouldPgBaseBackupCreateApplicationSecret returns true if for this cluster,
// during the bootstrap phase using pg_basebackup, we need to create an application secret
func (cluster *Cluster) ShouldPgBaseBackupCreateApplicationSecret() bool {
	return cluster.ShouldPgBaseBackupCreateApplicationDatabase() &&
		(cluster.Spec.Bootstrap.PgBaseBackup.Secret == nil ||
			cluster.Spec.Bootstrap.PgBaseBackup.Secret.Name == "")
}

// ShouldRecoveryCreateApplicationSecret returns true if for this cluster,
// during the bootstrap phase using recovery, we need to create an application secret
func (cluster *Cluster) ShouldRecoveryCreateApplicationSecret() bool {
	return cluster.ShouldRecoveryCreateApplicationDatabase() &&
		(cluster.Spec.Bootstrap.Recovery.Secret == nil ||
			cluster.Spec.Bootstrap.Recovery.Secret.Name == "")
}

// ShouldCreateApplicationDatabase returns true if for this cluster,
// during the bootstrap phase, we need to create an application database
func (cluster *Cluster) ShouldCreateApplicationDatabase() bool {
	return cluster.ShouldInitDBCreateApplicationDatabase() ||
		cluster.ShouldRecoveryCreateApplicationDatabase() ||
		cluster.ShouldPgBaseBackupCreateApplicationDatabase()
}

// ShouldInitDBRunPostInitApplicationSQLRefs returns true if for this cluster,
// during the bootstrap phase using initDB, we need to run post init SQL files
// for the application database from provided references.
func (cluster *Cluster) ShouldInitDBRunPostInitApplicationSQLRefs() bool {
	if cluster.Spec.Bootstrap == nil {
		return false
	}

	if cluster.Spec.Bootstrap.InitDB == nil {
		return false
	}

	return cluster.Spec.Bootstrap.InitDB.PostInitApplicationSQLRefs.HasElements()
}

// ShouldInitDBRunPostInitTemplateSQLRefs returns true if for this cluster,
// during the bootstrap phase using initDB, we need to run post init SQL files
// for the `template1` database from provided references.
func (cluster *Cluster) ShouldInitDBRunPostInitTemplateSQLRefs() bool {
	if cluster.Spec.Bootstrap == nil {
		return false
	}

	if cluster.Spec.Bootstrap.InitDB == nil {
		return false
	}

	return cluster.Spec.Bootstrap.InitDB.PostInitTemplateSQLRefs.HasElements()
}

// ShouldInitDBRunPostInitSQLRefs returns true if for this cluster,
// during the bootstrap phase using initDB, we need to run post init SQL files
// for the `postgres` database from provided references.
func (cluster *Cluster) ShouldInitDBRunPostInitSQLRefs() bool {
	if cluster.Spec.Bootstrap == nil {
		return false
	}

	if cluster.Spec.Bootstrap.InitDB == nil {
		return false
	}

	return cluster.Spec.Bootstrap.InitDB.PostInitSQLRefs.HasElements()
}

// ShouldInitDBCreateApplicationDatabase returns true if the application database needs to be created during initdb
// job
func (cluster *Cluster) ShouldInitDBCreateApplicationDatabase() bool {
	if cluster.Spec.Bootstrap == nil {
		return false
	}

	if cluster.Spec.Bootstrap.InitDB == nil {
		return false
	}

	initDBParameters := cluster.Spec.Bootstrap.InitDB
	return initDBParameters.Owner != "" && initDBParameters.Database != ""
}

// ShouldPgBaseBackupCreateApplicationDatabase returns true if the application database needs to be created during the
// pg_basebackup job
func (cluster *Cluster) ShouldPgBaseBackupCreateApplicationDatabase() bool {
	// we skip creating the application database if cluster is a replica
	if cluster.IsReplica() {
		return false
	}
	if cluster.Spec.Bootstrap == nil {
		return false
	}

	if cluster.Spec.Bootstrap.PgBaseBackup == nil {
		return false
	}

	pgBaseBackupParameters := cluster.Spec.Bootstrap.PgBaseBackup
	return pgBaseBackupParameters.Owner != "" && pgBaseBackupParameters.Database != ""
}

// ShouldRecoveryCreateApplicationDatabase returns true if the application database needs to be created during the
// recovery job
func (cluster *Cluster) ShouldRecoveryCreateApplicationDatabase() bool {
	// we skip creating the application database if cluster is a replica
	if cluster.IsReplica() {
		return false
	}

	if cluster.Spec.Bootstrap == nil {
		return false
	}

	if cluster.Spec.Bootstrap.Recovery == nil {
		return false
	}

	recoveryParameters := cluster.Spec.Bootstrap.Recovery
	return recoveryParameters.Owner != "" && recoveryParameters.Database != ""
}

// ShouldCreateProjectedVolume returns whether we should create the projected all in one volume
func (cluster *Cluster) ShouldCreateProjectedVolume() bool {
	return cluster.Spec.ProjectedVolumeTemplate != nil
}

// ShouldCreateWalArchiveVolume returns whether we should create the wal archive volume
func (cluster *Cluster) ShouldCreateWalArchiveVolume() bool {
	return cluster.Spec.WalStorage != nil
}

// ShouldPromoteFromReplicaCluster returns true if the cluster should promote
func (cluster *Cluster) ShouldPromoteFromReplicaCluster() bool {
	// If there's no replica cluster configuration there's no
	// promotion token too, so we don't need to promote.
	if cluster.Spec.ReplicaCluster == nil {
		return false
	}

	// If we don't have a promotion token, we don't need to promote
	if len(cluster.Spec.ReplicaCluster.PromotionToken) == 0 {
		return false
	}

	// If the current token was already used, there's no need to
	// promote
	if cluster.Spec.ReplicaCluster.PromotionToken == cluster.Status.LastPromotionToken {
		return false
	}
	return true
}

// ContainsTablespaces returns true if for this cluster, we need to create tablespaces
func (cluster *Cluster) ContainsTablespaces() bool {
	return len(cluster.Spec.Tablespaces) != 0
}

// GetPostgresUID returns the UID that is being used for the "postgres"
// user
func (cluster Cluster) GetPostgresUID() int64 {
	if cluster.Spec.PostgresUID == 0 {
		return DefaultPostgresUID
	}
	return cluster.Spec.PostgresUID
}

// GetPostgresGID returns the GID that is being used for the "postgres"
// user
func (cluster Cluster) GetPostgresGID() int64 {
	if cluster.Spec.PostgresGID == 0 {
		return DefaultPostgresGID
	}
	return cluster.Spec.PostgresGID
}

// ExternalCluster gets the external server with a known name, returning
// true if the server was found and false otherwise
func (cluster Cluster) ExternalCluster(name string) (ExternalCluster, bool) {
	for _, server := range cluster.Spec.ExternalClusters {
		if server.Name == name {
			return server, true
		}
	}

	return ExternalCluster{}, false
}

// IsReplica checks if this is a replica cluster or not
func (cluster Cluster) IsReplica() bool {
	// Before introducing the "primary" field, the
	// "enabled" parameter was declared as a "boolean"
	// and was not declared "omitempty".
	//
	// Legacy replica clusters will have the "replica" stanza
	// and the "enabled" field set explicitly to true.
	//
	// The following code is designed to not change the
	// previous semantics.
	r := cluster.Spec.ReplicaCluster
	if r == nil {
		return false
	}

	if r.Enabled != nil {
		return *r.Enabled
	}

	clusterName := r.Self
	if len(clusterName) == 0 {
		clusterName = cluster.Name
	}

	return clusterName != r.Primary
}

var slotNameNegativeRegex = regexp.MustCompile("[^a-z0-9_]+")

// GetSlotNameFromInstanceName returns the slot name, given the instance name.
// It returns an empty string if High Availability Replication Slots are disabled
func (cluster Cluster) GetSlotNameFromInstanceName(instanceName string) string {
	if cluster.Spec.ReplicationSlots == nil ||
		cluster.Spec.ReplicationSlots.HighAvailability == nil ||
		!cluster.Spec.ReplicationSlots.HighAvailability.GetEnabled() {
		return ""
	}

	return cluster.Spec.ReplicationSlots.HighAvailability.GetSlotNameFromInstanceName(instanceName)
}

// GetBarmanEndpointCAForReplicaCluster checks if this is a replica cluster which needs barman endpoint CA
func (cluster Cluster) GetBarmanEndpointCAForReplicaCluster() *SecretKeySelector {
	if !cluster.IsReplica() {
		return nil
	}
	sourceName := cluster.Spec.ReplicaCluster.Source
	externalCluster, found := cluster.ExternalCluster(sourceName)
	if !found || externalCluster.BarmanObjectStore == nil {
		return nil
	}
	return externalCluster.BarmanObjectStore.EndpointCA
}

// GetClusterAltDNSNames returns all the names needed to build a valid Server Certificate
func (cluster *Cluster) GetClusterAltDNSNames() []string {
	buildServiceNames := func(serviceName string, enabled bool) []string {
		if !enabled {
			return nil
		}
		return []string{
			serviceName,
			fmt.Sprintf("%v.%v", serviceName, cluster.Namespace),
			fmt.Sprintf("%v.%v.svc", serviceName, cluster.Namespace),
			fmt.Sprintf("%v.%v.svc.%s", serviceName, cluster.Namespace, configuration.Current.KubernetesClusterDomain),
		}
	}
	altDNSNames := slices.Concat(
		buildServiceNames(cluster.GetServiceReadWriteName(), cluster.IsReadWriteServiceEnabled()),
		buildServiceNames(cluster.GetServiceReadName(), cluster.IsReadServiceEnabled()),
		buildServiceNames(cluster.GetServiceReadOnlyName(), cluster.IsReadOnlyServiceEnabled()),
	)

	if cluster.Spec.Managed != nil && cluster.Spec.Managed.Services != nil {
		for _, service := range cluster.Spec.Managed.Services.Additional {
			altDNSNames = append(altDNSNames, buildServiceNames(service.ServiceTemplate.ObjectMeta.Name, true)...)
		}
	}

	return append(altDNSNames, cluster.Spec.Certificates.getServerAltDNSNames()...)
}

// UsesSecret checks whether a given secret is used by a Cluster.
//
// This function is also used to discover the set of clusters that
// should be reconciled when a certain secret changes.
func (cluster *Cluster) UsesSecret(secret string) bool {
	if _, ok := cluster.Status.SecretsResourceVersion.Metrics[secret]; ok {
		return true
	}
	certificates := cluster.Status.Certificates
	switch secret {
	case cluster.GetSuperuserSecretName(),
		cluster.GetApplicationSecretName(),
		certificates.ClientCASecret,
		certificates.ReplicationTLSSecret,
		certificates.ServerCASecret,
		certificates.ServerTLSSecret:
		return true
	}

	if cluster.UsesSecretInManagedRoles(secret) {
		return true
	}

	if cluster.Spec.Backup.IsBarmanEndpointCASet() && cluster.Spec.Backup.BarmanObjectStore.EndpointCA.Name == secret {
		return true
	}

	if endpointCA := cluster.GetBarmanEndpointCAForReplicaCluster(); endpointCA != nil && endpointCA.Name == secret {
		return true
	}

	if cluster.Status.PoolerIntegrations != nil &&
		slices.Contains(cluster.Status.PoolerIntegrations.PgBouncerIntegration.Secrets, secret) {
		return true
	}

	// watch the secrets defined in external clusters
	return cluster.GetExternalClusterSecrets().Has(secret)
}

// UsesConfigMap checks whether a given secret is used by a Cluster
func (cluster *Cluster) UsesConfigMap(config string) (ok bool) {
	if _, ok := cluster.Status.ConfigMapResourceVersion.Metrics[config]; ok {
		return true
	}
	return false
}

// IsPodMonitorEnabled checks if the PodMonitor object needs to be created
func (cluster *Cluster) IsPodMonitorEnabled() bool {
	if cluster.Spec.Monitoring != nil {
		return cluster.Spec.Monitoring.EnablePodMonitor
	}

	return false
}

// IsMetricsTLSEnabled checks if the metrics endpoint should use TLS
func (cluster *Cluster) IsMetricsTLSEnabled() bool {
	if cluster.Spec.Monitoring != nil && cluster.Spec.Monitoring.TLSConfig != nil {
		return cluster.Spec.Monitoring.TLSConfig.Enabled
	}

	return false
}

// GetMetricsQueriesTTL returns the Time To Live of the metrics computed from
// queries. Once exceeded, a scrape of the metric will trigger a rerun of the queries.
// Default value is 30 seconds
func (cluster *Cluster) GetMetricsQueriesTTL() metav1.Duration {
	if cluster.Spec.Monitoring != nil && cluster.Spec.Monitoring.MetricsQueriesTTL != nil {
		return *cluster.Spec.Monitoring.MetricsQueriesTTL
	}

	return metav1.Duration{
		Duration: 30 * time.Second,
	}
}

// GetEnableSuperuserAccess returns if the superuser access is enabled or not
func (cluster *Cluster) GetEnableSuperuserAccess() bool {
	if cluster.Spec.EnableSuperuserAccess != nil {
		return *cluster.Spec.EnableSuperuserAccess
	}

	return false
}

// LogTimestampsWithMessage prints useful information about timestamps in stdout
func (cluster *Cluster) LogTimestampsWithMessage(ctx context.Context, logMessage string) {
	currentTimestamp := pgTime.GetCurrentTimestamp()

	contextLogger := log.FromContext(ctx).WithValues(
		"phase", cluster.Status.Phase,
		"currentTimestamp", currentTimestamp,
		"targetPrimaryTimestamp", cluster.Status.TargetPrimaryTimestamp,
		"currentPrimaryTimestamp", cluster.Status.CurrentPrimaryTimestamp,
	)

	var errs []string

	// Elapsed time since the last request of promotion (TargetPrimaryTimestamp)
	if diff, err := pgTime.DifferenceBetweenTimestamps(
		currentTimestamp,
		cluster.Status.TargetPrimaryTimestamp,
	); err == nil {
		contextLogger = contextLogger.WithValues("msPassedSinceTargetPrimaryTimestamp", diff.Milliseconds())
	} else {
		errs = append(errs, err.Error())
	}

	// Elapsed time since the last promotion (CurrentPrimaryTimestamp)
	if currentPrimaryDifference, err := pgTime.DifferenceBetweenTimestamps(
		currentTimestamp,
		cluster.Status.CurrentPrimaryTimestamp,
	); err == nil {
		contextLogger = contextLogger.WithValues("msPassedSinceCurrentPrimaryTimestamp",
			currentPrimaryDifference.Milliseconds(),
		)
	} else {
		errs = append(errs, err.Error())
	}

	// Difference between the last promotion and the last request of promotion
	// When positive, it is the amount of time required in the last promotion
	// of a standby to a primary. If negative, it means we have a failover/switchover
	// in progress, and the value represents the last measured uptime of the primary.
	if currentPrimaryTargetDifference, err := pgTime.DifferenceBetweenTimestamps(
		cluster.Status.CurrentPrimaryTimestamp,
		cluster.Status.TargetPrimaryTimestamp,
	); err == nil {
		contextLogger = contextLogger.WithValues(
			"msDifferenceBetweenCurrentAndTargetPrimary",
			currentPrimaryTargetDifference.Milliseconds(),
		)
	} else {
		errs = append(errs, err.Error())
	}

	if len(errs) > 0 {
		contextLogger = contextLogger.WithValues("timestampParsingErrors", errs)
	}

	contextLogger.Info(logMessage)
}

// SetInheritedDataAndOwnership sets the cluster as owner of the passed object and then
// sets all the needed annotations and labels
func (cluster *Cluster) SetInheritedDataAndOwnership(obj *metav1.ObjectMeta) {
	cluster.SetInheritedData(obj)
	utils.SetAsOwnedBy(obj, cluster.ObjectMeta, cluster.TypeMeta)
}

// SetInheritedData sets all the needed annotations and labels
func (cluster *Cluster) SetInheritedData(obj *metav1.ObjectMeta) {
	utils.InheritAnnotations(obj, cluster.Annotations, cluster.GetFixedInheritedAnnotations(), configuration.Current)
	utils.InheritLabels(obj, cluster.Labels, cluster.GetFixedInheritedLabels(), configuration.Current)
	utils.LabelClusterName(obj, cluster.GetName())
}

// ShouldForceLegacyBackup if present takes a backup without passing the name argument even on barman version 3.3.0+.
// This is needed to test both backup system in the E2E suite
func (cluster *Cluster) ShouldForceLegacyBackup() bool {
	return cluster.Annotations[utils.LegacyBackupAnnotationName] == "true"
}

// GetSeccompProfile return the proper SeccompProfile set in the cluster for Pods and Containers
func (cluster *Cluster) GetSeccompProfile() *corev1.SeccompProfile {
	if cluster.Spec.SeccompProfile != nil {
		return cluster.Spec.SeccompProfile
	}

	return &corev1.SeccompProfile{
		Type: corev1.SeccompProfileTypeRuntimeDefault,
	}
}

// GetCoredumpFilter get the coredump filter value from the cluster annotation
func (cluster *Cluster) GetCoredumpFilter() string {
	value, ok := cluster.Annotations[utils.CoredumpFilter]
	if ok {
		return value
	}
	return system.DefaultCoredumpFilter
}

// IsInplaceRestartPhase returns true if the cluster is in a phase that handles the Inplace restart
func (cluster *Cluster) IsInplaceRestartPhase() bool {
	return cluster.Status.Phase == PhaseInplacePrimaryRestart ||
		cluster.Status.Phase == PhaseInplaceDeletePrimaryRestart
}

// GetTablespaceConfiguration returns the tablespaceConfiguration for the given name
// otherwise return nil
func (cluster *Cluster) GetTablespaceConfiguration(name string) *TablespaceConfiguration {
	for _, tbsConfig := range cluster.Spec.Tablespaces {
		if name == tbsConfig.Name {
			return &tbsConfig
		}
	}

	return nil
}

// GetServerCASecretObjectKey returns a types.NamespacedName pointing to the secret
func (cluster *Cluster) GetServerCASecretObjectKey() types.NamespacedName {
	return types.NamespacedName{Namespace: cluster.Namespace, Name: cluster.GetServerCASecretName()}
}

// IsBarmanBackupConfigured returns true if one of the possible backup destination
// is configured, false otherwise
func (backupConfiguration *BackupConfiguration) IsBarmanBackupConfigured() bool {
	return backupConfiguration != nil && backupConfiguration.BarmanObjectStore != nil &&
		backupConfiguration.BarmanObjectStore.ArePopulated()
}

// IsBarmanEndpointCASet returns true if we have a CA bundle for the endpoint
// false otherwise
func (backupConfiguration *BackupConfiguration) IsBarmanEndpointCASet() bool {
	return backupConfiguration != nil &&
		backupConfiguration.BarmanObjectStore != nil &&
		backupConfiguration.BarmanObjectStore.EndpointCA != nil &&
		backupConfiguration.BarmanObjectStore.EndpointCA.Name != "" &&
		backupConfiguration.BarmanObjectStore.EndpointCA.Key != ""
}

// UpdateBackupTimes sets the firstRecoverabilityPoint and lastSuccessfulBackup
// for the provided method, as well as the overall firstRecoverabilityPoint and
// lastSuccessfulBackup for the cluster
func (cluster *Cluster) UpdateBackupTimes(
	backupMethod BackupMethod,
	firstRecoverabilityPoint *time.Time,
	lastSuccessfulBackup *time.Time,
) {
	type comparer func(a metav1.Time, b metav1.Time) bool
	// tryGetMaxTime gets either the newest or oldest time from a set of backup times,
	// depending on the comparer argument passed to it
	tryGetMaxTime := func(m map[BackupMethod]metav1.Time, compare comparer) string {
		var maximum metav1.Time
		for _, ts := range m {
			if maximum.IsZero() || compare(ts, maximum) {
				maximum = ts
			}
		}
		result := ""
		if !maximum.IsZero() {
			result = maximum.Format(time.RFC3339)
		}

		return result
	}

	setTime := func(backupTimes map[BackupMethod]metav1.Time, value *time.Time) map[BackupMethod]metav1.Time {
		if value == nil {
			delete(backupTimes, backupMethod)
			return backupTimes
		}

		if backupTimes == nil {
			backupTimes = make(map[BackupMethod]metav1.Time)
		}

		backupTimes[backupMethod] = metav1.NewTime(*value)
		return backupTimes
	}

	cluster.Status.FirstRecoverabilityPointByMethod = setTime(cluster.Status.FirstRecoverabilityPointByMethod,
		firstRecoverabilityPoint)
	cluster.Status.FirstRecoverabilityPoint = tryGetMaxTime(
		cluster.Status.FirstRecoverabilityPointByMethod,
		// we pass a comparer to get the first among the recoverability points
		func(a metav1.Time, b metav1.Time) bool {
			return a.Before(&b)
		})

	cluster.Status.LastSuccessfulBackupByMethod = setTime(cluster.Status.LastSuccessfulBackupByMethod,
		lastSuccessfulBackup)
	cluster.Status.LastSuccessfulBackup = tryGetMaxTime(
		cluster.Status.LastSuccessfulBackupByMethod,
		// we pass a comparer to get the last among the last backup times per method
		func(a metav1.Time, b metav1.Time) bool {
			return b.Before(&a)
		})
}

// IsReadServiceEnabled checks if the read service is enabled for the cluster.
// It returns false if the read service is listed in the DisabledDefaultServices slice.
func (cluster *Cluster) IsReadServiceEnabled() bool {
	if cluster.Spec.Managed == nil || cluster.Spec.Managed.Services == nil {
		return true
	}

	return !slices.Contains(cluster.Spec.Managed.Services.DisabledDefaultServices, ServiceSelectorTypeR)
}

// IsReadWriteServiceEnabled checks if the read-write service is enabled for the cluster.
// It returns false if the read-write service is listed in the DisabledDefaultServices slice.
func (cluster *Cluster) IsReadWriteServiceEnabled() bool {
	if cluster.Spec.Managed == nil || cluster.Spec.Managed.Services == nil {
		return true
	}
	return !slices.Contains(cluster.Spec.Managed.Services.DisabledDefaultServices, ServiceSelectorTypeRW)
}

// IsReadOnlyServiceEnabled checks if the read-only service is enabled for the cluster.
// It returns false if the read-only service is listed in the DisabledDefaultServices slice.
func (cluster *Cluster) IsReadOnlyServiceEnabled() bool {
	if cluster.Spec.Managed == nil || cluster.Spec.Managed.Services == nil {
		return true
	}

	return !slices.Contains(cluster.Spec.Managed.Services.DisabledDefaultServices, ServiceSelectorTypeRO)
}

// GetRecoverySourcePlugin returns the configuration of the plugin being
// the recovery source of the cluster. If no such plugin have been configured,
// nil is returned
func (cluster *Cluster) GetRecoverySourcePlugin() *PluginConfiguration {
	if cluster.Spec.Bootstrap == nil || cluster.Spec.Bootstrap.Recovery == nil {
		return nil
	}

	recoveryConfig := cluster.Spec.Bootstrap.Recovery
	if len(recoveryConfig.Source) == 0 {
		// Plugin-based recovery is supported only with
		// An external cluster definition
		return nil
	}

	recoveryExternalCluster, found := cluster.ExternalCluster(recoveryConfig.Source)
	if !found {
		// This error should have already been detected
		// by the validating webhook.
		return nil
	}

	return recoveryExternalCluster.PluginConfiguration
}

// EnsureGVKIsPresent ensures that the GroupVersionKind (GVK) metadata is present in the Backup object.
// This is necessary because informers do not automatically include metadata inside the object.
// By setting the GVK, we ensure that components such as the plugins have enough metadata to typecheck the object.
func (cluster *Cluster) EnsureGVKIsPresent() {
	cluster.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   SchemeGroupVersion.Group,
		Version: SchemeGroupVersion.Version,
		Kind:    ClusterKind,
	})
}

// BuildPostgresOptions create the list of options that
// should be added to the PostgreSQL configuration to
// recover given a certain target
func (target *RecoveryTarget) BuildPostgresOptions() string {
	result := ""

	if target == nil {
		return result
	}

	if target.TargetTLI != "" {
		result += fmt.Sprintf(
			"recovery_target_timeline = '%v'\n",
			target.TargetTLI)
	}
	if target.TargetXID != "" {
		result += fmt.Sprintf(
			"recovery_target_xid = '%v'\n",
			target.TargetXID)
	}
	if target.TargetName != "" {
		result += fmt.Sprintf(
			"recovery_target_name = '%v'\n",
			target.TargetName)
	}
	if target.TargetLSN != "" {
		result += fmt.Sprintf(
			"recovery_target_lsn = '%v'\n",
			target.TargetLSN)
	}
	if target.TargetTime != "" {
		result += fmt.Sprintf(
			"recovery_target_time = '%v'\n",
			pgTime.ConvertToPostgresFormat(target.TargetTime))
	}
	if target.TargetImmediate != nil && *target.TargetImmediate {
		result += "recovery_target = immediate\n"
	}
	if target.Exclusive != nil && *target.Exclusive {
		result += "recovery_target_inclusive = false\n"
	} else {
		result += "recovery_target_inclusive = true\n"
	}

	return result
}

// ApplyInto applies the content of the probe configuration in a Kubernetes
// probe
func (p *Probe) ApplyInto(k8sProbe *corev1.Probe) {
	if p == nil {
		return
	}

	if p.InitialDelaySeconds != 0 {
		k8sProbe.InitialDelaySeconds = p.InitialDelaySeconds
	}
	if p.TimeoutSeconds != 0 {
		k8sProbe.TimeoutSeconds = p.TimeoutSeconds
	}
	if p.PeriodSeconds != 0 {
		k8sProbe.PeriodSeconds = p.PeriodSeconds
	}
	if p.SuccessThreshold != 0 {
		k8sProbe.SuccessThreshold = p.SuccessThreshold
	}
	if p.FailureThreshold != 0 {
		k8sProbe.FailureThreshold = p.FailureThreshold
	}
	if p.TerminationGracePeriodSeconds != nil {
		k8sProbe.TerminationGracePeriodSeconds = p.TerminationGracePeriodSeconds
	}
}

// ApplyInto applies the content of the probe configuration in a Kubernetes
// probe
func (p *ProbeWithStrategy) ApplyInto(k8sProbe *corev1.Probe) {
	if p == nil {
		return
	}

	p.Probe.ApplyInto(k8sProbe)
}

// GetEnabledWALArchivePluginName returns the name of the enabled backup plugin or an empty string
// if no backup plugin is enabled
func (cluster *Cluster) GetEnabledWALArchivePluginName() string {
	for _, plugin := range cluster.Spec.Plugins {
		if plugin.IsEnabled() && plugin.IsWALArchiver != nil && *plugin.IsWALArchiver {
			return plugin.Name
		}
	}

	return ""
}

// IsFailoverQuorumActive check if we should enable the
// quorum failover protection alpha-feature.
func (cluster *Cluster) IsFailoverQuorumActive() bool {
	if cluster.Spec.PostgresConfiguration.Synchronous == nil {
		return false
	}

	return cluster.Spec.PostgresConfiguration.Synchronous.FailoverQuorum
}
