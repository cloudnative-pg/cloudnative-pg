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

// Package tests contains the test infrastructure of the CloudNativePG operator
package tests

// List of the labels we use for labeling test specs
// See https://github.com/onsi/ginkgo/blob/c70867a9661d9eb6eeb706dd7580bf510a99f35b/docs/MIGRATING_TO_V2.md
const (
	// LabelDisruptive is the string for labelling disruptive tests
	LabelDisruptive = "disruptive"

	// LabelPerformance is the string for labelling performance tests
	LabelPerformance = "performance"

	// LabelUpgrade is the string for labelling upgrade tests
	LabelUpgrade = "upgrade"

	// LabelNoOpenshift is the string for labelling tests that don't run on Openshift
	LabelNoOpenshift = "no-openshift"

	// LabelIgnoreFails is the string for labelling tests that should not be considered when failing.
	LabelIgnoreFails = "ignore-fails"

	// LabelSmoke is a label for selecting  smoke test
	LabelSmoke = "smoke"

	// LabelBasic is a label for  selecting basic test
	LabelBasic = "basic"

	// LabelServiceConnectivity is a label for selecting service connections test
	LabelServiceConnectivity = "service-connectivity"

	// LabelSelfHealing is a label for selecting self-healing test
	LabelSelfHealing = "self-healing"

	// LabelBackupRestore is a label for only selecting backup and restore tests
	LabelBackupRestore = "backup-restore"

	// LabelSnapshot is a label for selecting snapshot tests
	LabelSnapshot = "snapshot"

	// LabelOperator is a label for only selecting operator tests
	LabelOperator = "operator"

	// LabelObservability is a label for selecting observability test
	LabelObservability = "observability"

	// LabelReplication is a label for selecting replication test
	LabelReplication = "replication"

	// LabelPlugin is a label for selecting plugin test
	LabelPlugin = "plugin"

	// LabelPostgresConfiguration is a label for selecting postgres-configuration test
	LabelPostgresConfiguration = "postgres-configuration"

	// LabelPodScheduling is a label for selecting pod-scheduling test
	LabelPodScheduling = "pod-scheduling"

	// LabelClusterMetadata is a label for selecting cluster-metadata test
	LabelClusterMetadata = "cluster-metadata"

	// LabelRecovery is a label for selecting cluster-metadata test
	LabelRecovery = "recovery"

	// LabelImportingDatabases is a label for selecting importing-databases test
	LabelImportingDatabases = "importing-databases"

	// LabelStorage is a label for selecting storage test
	LabelStorage = "storage"

	// LabelSecurity is a label for selecting security test
	LabelSecurity = "security"

	// LabelMaintenance is a label for selecting importing-databases test
	LabelMaintenance = "maintenance"
)
