/*
Copyright The CloudNativePG Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1

import (
	"github.com/cloudnative-pg/machinery/pkg/log"
	"k8s.io/utils/ptr"

	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

const (
	// DefaultMonitoringKey is the key that should be used in the default metrics configmap to store the queries
	DefaultMonitoringKey = "queries"
	// DefaultMonitoringConfigMapName is the name of the target configmap with the default monitoring queries,
	// if configured
	DefaultMonitoringConfigMapName = "cnpg-default-monitoring"
	// DefaultMonitoringSecretName is the name of the target secret with the default monitoring queries,
	// if configured
	DefaultMonitoringSecretName = DefaultMonitoringConfigMapName
	// DefaultApplicationDatabaseName is the name of application database if not specified
	DefaultApplicationDatabaseName = "app"
	// DefaultApplicationUserName is the name of application database owner if not specified
	DefaultApplicationUserName = DefaultApplicationDatabaseName
)

// Default apply the defaults to undefined values in a Cluster preserving the user settings
func (r *Cluster) Default() {
	r.setDefaults(true)
}

// SetDefaults apply the defaults to undefined values in a Cluster
func (r *Cluster) SetDefaults() {
	r.setDefaults(false)
}

func (r *Cluster) setDefaults(preserveUserSettings bool) {
	// Defaulting the image name if not specified
	if r.Spec.ImageName == "" && r.Spec.ImageCatalogRef == nil {
		r.Spec.ImageName = configuration.Current.PostgresImageName
	}

	// Defaulting the bootstrap method if not specified
	if r.Spec.Bootstrap == nil {
		r.Spec.Bootstrap = &BootstrapConfiguration{}
	}

	// Defaulting initDB if no other bootstrap method was passed
	switch {
	case r.Spec.Bootstrap.Recovery != nil:
		r.defaultRecovery()
	case r.Spec.Bootstrap.PgBaseBackup != nil:
		r.defaultPgBaseBackup()
	default:
		r.defaultInitDB()
	}

	// Defaulting the pod anti-affinity type if podAntiAffinity
	if (r.Spec.Affinity.EnablePodAntiAffinity == nil || *r.Spec.Affinity.EnablePodAntiAffinity) &&
		r.Spec.Affinity.PodAntiAffinityType == "" {
		r.Spec.Affinity.PodAntiAffinityType = PodAntiAffinityTypePreferred
	}

	if r.Spec.Backup != nil && r.Spec.Backup.Target == "" {
		r.Spec.Backup.Target = DefaultBackupTarget
	}

	psqlVersion, err := r.GetPostgresqlVersion()
	if err == nil {
		// The validation error will be already raised by the
		// validateImageName function
		info := postgres.ConfigurationInfo{
			Settings:                      postgres.CnpgConfigurationSettings,
			Version:                       psqlVersion,
			UserSettings:                  r.Spec.PostgresConfiguration.Parameters,
			IsReplicaCluster:              r.IsReplica(),
			PreserveFixedSettingsFromUser: preserveUserSettings,
			IsWalArchivingDisabled:        utils.IsWalArchivingDisabled(&r.ObjectMeta),
			IsAlterSystemEnabled:          r.Spec.PostgresConfiguration.EnableAlterSystem,
		}
		sanitizedParameters := postgres.CreatePostgresqlConfiguration(info).GetConfigurationParameters()
		r.Spec.PostgresConfiguration.Parameters = sanitizedParameters
	}

	if r.Spec.LogLevel == "" {
		r.Spec.LogLevel = log.InfoLevelString
	}

	// we inject the defaultMonitoringQueries if the MonitoringQueriesConfigmap parameter is not empty
	// and defaultQueries not disabled on cluster crd
	if !r.Spec.Monitoring.AreDefaultQueriesDisabled() {
		r.defaultMonitoringQueries(configuration.Current)
	}

	// If the ReplicationSlots or HighAvailability stanzas are nil, we create them and enable slots
	if r.Spec.ReplicationSlots == nil {
		r.Spec.ReplicationSlots = &ReplicationSlotsConfiguration{}
	}
	if r.Spec.ReplicationSlots.HighAvailability == nil {
		r.Spec.ReplicationSlots.HighAvailability = &ReplicationSlotsHAConfiguration{
			Enabled:    ptr.To(true),
			SlotPrefix: "_cnpg_",
		}
	}
	if r.Spec.ReplicationSlots.SynchronizeReplicas == nil {
		r.Spec.ReplicationSlots.SynchronizeReplicas = &SynchronizeReplicasConfiguration{
			Enabled: ptr.To(true),
		}
	}

	if len(r.Spec.Tablespaces) > 0 {
		r.defaultTablespaces()
	}
}

// defaultTablespaces adds the tablespace owner where the
// user didn't specify it
func (r *Cluster) defaultTablespaces() {
	defaultOwner := r.GetApplicationDatabaseOwner()
	if len(defaultOwner) == 0 {
		defaultOwner = "postgres"
	}

	for name, tablespaceConfiguration := range r.Spec.Tablespaces {
		if len(tablespaceConfiguration.Owner.Name) == 0 {
			tablespaceConfiguration.Owner.Name = defaultOwner
		}
		r.Spec.Tablespaces[name] = tablespaceConfiguration
	}
}

// defaultMonitoringQueries adds the default monitoring queries configMap
// if not already present in CustomQueriesConfigMap
func (r *Cluster) defaultMonitoringQueries(config *configuration.Data) {
	if r.Spec.Monitoring == nil {
		r.Spec.Monitoring = &MonitoringConfiguration{}
	}

	if config.MonitoringQueriesConfigmap != "" {
		var defaultConfigMapQueriesAlreadyPresent bool
		// We check if the default queries are already inserted in the monitoring configuration
		for _, monitoringConfigMap := range r.Spec.Monitoring.CustomQueriesConfigMap {
			if monitoringConfigMap.Name == DefaultMonitoringConfigMapName {
				defaultConfigMapQueriesAlreadyPresent = true
				break
			}
		}

		// If the default queries are already present there is no need to re-add them.
		// Please note that in this case that the default configMap could overwrite user existing queries
		// depending on the order. This is an accepted behavior because the user willingly defined the order of his array
		if !defaultConfigMapQueriesAlreadyPresent {
			r.Spec.Monitoring.CustomQueriesConfigMap = append([]ConfigMapKeySelector{
				{
					LocalObjectReference: LocalObjectReference{Name: DefaultMonitoringConfigMapName},
					Key:                  DefaultMonitoringKey,
				},
			}, r.Spec.Monitoring.CustomQueriesConfigMap...)
		}
	}

	if config.MonitoringQueriesSecret != "" {
		var defaultSecretQueriesAlreadyPresent bool
		// we check if the default queries are already inserted in the monitoring configuration
		for _, monitoringSecret := range r.Spec.Monitoring.CustomQueriesSecret {
			if monitoringSecret.Name == DefaultMonitoringSecretName {
				defaultSecretQueriesAlreadyPresent = true
				break
			}
		}

		if !defaultSecretQueriesAlreadyPresent {
			r.Spec.Monitoring.CustomQueriesSecret = append([]SecretKeySelector{
				{
					LocalObjectReference: LocalObjectReference{Name: DefaultMonitoringSecretName},
					Key:                  DefaultMonitoringKey,
				},
			}, r.Spec.Monitoring.CustomQueriesSecret...)
		}
	}
}

// defaultInitDB enriches the initDB with defaults if not all the required arguments were passed
func (r *Cluster) defaultInitDB() {
	if r.Spec.Bootstrap.InitDB == nil {
		r.Spec.Bootstrap.InitDB = &BootstrapInitDB{
			Database: DefaultApplicationDatabaseName,
			Owner:    DefaultApplicationUserName,
		}
	}

	if r.Spec.Bootstrap.InitDB.Database == "" {
		r.Spec.Bootstrap.InitDB.Database = DefaultApplicationDatabaseName
	}
	if r.Spec.Bootstrap.InitDB.Owner == "" {
		r.Spec.Bootstrap.InitDB.Owner = r.Spec.Bootstrap.InitDB.Database
	}
	if r.Spec.Bootstrap.InitDB.Encoding == "" {
		r.Spec.Bootstrap.InitDB.Encoding = "UTF8"
	}
	if r.Spec.Bootstrap.InitDB.LocaleCollate == "" {
		r.Spec.Bootstrap.InitDB.LocaleCollate = "C"
	}
	if r.Spec.Bootstrap.InitDB.LocaleCType == "" {
		r.Spec.Bootstrap.InitDB.LocaleCType = "C"
	}
}

// defaultRecovery enriches the recovery with defaults if not all the required arguments were passed
func (r *Cluster) defaultRecovery() {
	if r.Spec.Bootstrap.Recovery.Database == "" {
		r.Spec.Bootstrap.Recovery.Database = DefaultApplicationDatabaseName
	}
	if r.Spec.Bootstrap.Recovery.Owner == "" {
		r.Spec.Bootstrap.Recovery.Owner = r.Spec.Bootstrap.Recovery.Database
	}
}

// defaultPgBaseBackup enriches the pg_basebackup with defaults if not all the required arguments were passed
func (r *Cluster) defaultPgBaseBackup() {
	if r.Spec.Bootstrap.PgBaseBackup.Database == "" {
		r.Spec.Bootstrap.PgBaseBackup.Database = DefaultApplicationDatabaseName
	}
	if r.Spec.Bootstrap.PgBaseBackup.Owner == "" {
		r.Spec.Bootstrap.PgBaseBackup.Owner = r.Spec.Bootstrap.PgBaseBackup.Database
	}
}