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
	"encoding/json"
	"fmt"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/cloudnative-pg/machinery/pkg/stringset"
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

	psqlVersion, err := r.GetPostgresqlMajorVersion()
	if err == nil {
		// The validation error will be already raised by the
		// validateImageName function
		info := postgres.ConfigurationInfo{
			Settings:                      postgres.CnpgConfigurationSettings,
			MajorVersion:                  psqlVersion,
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

	if r.Spec.PostgresConfiguration.Synchronous != nil &&
		r.Spec.PostgresConfiguration.Synchronous.DataDurability == "" {
		r.Spec.PostgresConfiguration.Synchronous.DataDurability = DataDurabilityLevelRequired
	}

	r.setDefaultPlugins(configuration.Current)
	r.setProbes()
}

func (r *Cluster) setDefaultPlugins(config *configuration.Data) {
	// Add the list of pre-defined plugins
	foundPlugins := stringset.New()
	for _, plugin := range r.Spec.Plugins {
		foundPlugins.Put(plugin.Name)
	}

	for _, pluginName := range config.GetIncludePlugins() {
		if !foundPlugins.Has(pluginName) {
			r.Spec.Plugins = append(r.Spec.Plugins, PluginConfiguration{
				Name:    pluginName,
				Enabled: ptr.To(true),
			})
		}
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
		// Set the default only if not executing a monolithic import
		if r.Spec.Bootstrap.InitDB.Import == nil ||
			r.Spec.Bootstrap.InitDB.Import.Type != MonolithSnapshotType {
			r.Spec.Bootstrap.InitDB.Database = DefaultApplicationDatabaseName
		}
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

const (
	// defaultRequestTimeout is the default value of the request timeout
	defaultRequestTimeout = 1000

	// defaultConnectionTimeout is the default value of the connection timeout
	defaultConnectionTimeout = 1000
)

func (r *Cluster) setProbes() {
	if r.Spec.Probes == nil {
		r.Spec.Probes = &ProbesConfiguration{}
	}

	if r.Spec.Probes.Liveness == nil {
		r.Spec.Probes.Liveness = &LivenessProbe{}
	}

	// we don't override the isolation check if it is already set
	if r.Spec.Probes.Liveness.IsolationCheck != nil {
		return
	}

	// STEP 1: check if the alpha annotation is present, in that case convert it to spec
	r.tryConvertAlphaLivenessPinger()

	if r.Spec.Probes.Liveness.IsolationCheck != nil {
		return
	}

	// STEP 2: set defaults.
	r.Spec.Probes.Liveness.IsolationCheck = &IsolationCheckConfiguration{
		Enabled:           ptr.To(true),
		RequestTimeout:    defaultRequestTimeout,
		ConnectionTimeout: defaultConnectionTimeout,
	}
}

func (r *Cluster) tryConvertAlphaLivenessPinger() {
	if _, ok := r.Annotations[utils.LivenessPingerAnnotationName]; !ok {
		return
	}
	v, err := NewLivenessPingerConfigFromAnnotations(r.Annotations)
	if err != nil || v == nil {
		// the error will be raised by the validation webhook
		return
	}

	r.Spec.Probes.Liveness.IsolationCheck = &IsolationCheckConfiguration{
		Enabled:           v.Enabled,
		RequestTimeout:    v.RequestTimeout,
		ConnectionTimeout: v.ConnectionTimeout,
	}
}

// NewLivenessPingerConfigFromAnnotations creates a new pinger configuration from the annotations
// in the cluster definition
func NewLivenessPingerConfigFromAnnotations(
	annotations map[string]string,
) (*IsolationCheckConfiguration, error) {
	v, ok := annotations[utils.LivenessPingerAnnotationName]
	if !ok {
		return nil, nil
	}

	var cfg IsolationCheckConfiguration
	if err := json.Unmarshal([]byte(v), &cfg); err != nil {
		return nil, fmt.Errorf("while unmarshalling pinger config: %w", err)
	}

	if cfg.Enabled == nil {
		return nil, fmt.Errorf("pinger config is missing the enabled field")
	}

	if cfg.RequestTimeout == 0 {
		cfg.RequestTimeout = defaultRequestTimeout
	}
	if cfg.ConnectionTimeout == 0 {
		cfg.ConnectionTimeout = defaultConnectionTimeout
	}

	return &cfg, nil
}
