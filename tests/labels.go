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

// Package tests contains the test infrastructure of the CloudNativePG operator
package tests

// List of the labels we use for labeling test specs
// See https://github.com/onsi/ginkgo/blob/c70867a9661d9eb6eeb706dd7580bf510a99f35b/docs/MIGRATING_TO_V2.md
const (
	// LabelBackupRestore is a label for only selecting backup and restore tests
	LabelBackupRestore = "backup-restore"

	// LabelBasic is a label for selecting basic tests
	LabelBasic = "basic"

	// LabelClusterMetadata is a label for selecting cluster-metadata tests
	LabelClusterMetadata = "cluster-metadata"

	// LabelDeclarativeDatabases is a label for selecting the declarative databases test
	LabelDeclarativeDatabases = "declarative-databases"

	// LabelDisruptive is the string for labelling disruptive tests
	LabelDisruptive = "disruptive"

	// LabelImportingDatabases is a label for selecting the importing-databases test
	LabelImportingDatabases = "importing-databases"

	// LabelMaintenance is a label for selecting maintenance tests
	LabelMaintenance = "maintenance"

	// LabelNoOpenshift is the string for selecting tests that don't run on Openshift
	LabelNoOpenshift = "no-openshift"

	// LabelObservability is a label for selecting observability tests
	LabelObservability = "observability"

	// LabelOperator is a label for only selecting operator tests
	LabelOperator = "operator"

	// LabelPerformance is the string for labelling performance tests
	LabelPerformance = "performance"

	// LabelPlugin is a label for selecting plugin tests
	LabelPlugin = "plugin"

	// LabelPodScheduling is a label for selecting pod-scheduling test
	LabelPodScheduling = "pod-scheduling"

	// LabelPostgresConfiguration is a label for selecting postgres-configuration test
	LabelPostgresConfiguration = "postgres-configuration"

	// LabelPublicationSubscription  is a label for selecting the publication / subscription test
	LabelPublicationSubscription = "publication-subscription"

	// LabelRecovery is a label for selecting recovery tests
	LabelRecovery = "recovery"

	// LabelReplication is a label for selecting replication tests
	LabelReplication = "replication"

	// LabelSecurity is a label for selecting security tests
	LabelSecurity = "security"

	// LabelSelfHealing is a label for selecting self-healing tests
	LabelSelfHealing = "self-healing"

	// LabelServiceConnectivity is a label for selecting service connections tests
	LabelServiceConnectivity = "service-connectivity"

	// LabelSmoke is a label for selecting smoke tests
	LabelSmoke = "smoke"

	// LabelSnapshot is a label for selecting snapshot tests
	LabelSnapshot = "snapshot"

	// LabelStorage is a label for selecting storage tests
	LabelStorage = "storage"

	// LabelTablespaces is a label for selecting the tablespaces test
	LabelTablespaces = "tablespaces"

	// LabelUpgrade is a label for upgrade tests
	LabelUpgrade = "upgrade"

	// LabelPostgresMajorUpgrade is a label for Cluster major version upgrade tests
	LabelPostgresMajorUpgrade = "postgres-major-upgrade"

	// LabelImageVolumeExtensions is a label for imageVolume extensions tests
	LabelImageVolumeExtensions = "image-volume-extensions"

	// LabelNamespacedOperator is a label for namespaced deployment tests with restricted rbac
	LabelNamespacedOperator = "namespaced-operator"
)
