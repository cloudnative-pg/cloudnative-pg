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
	// LabelBackupRestore is a label for only selecting backup and restore tests
	LabelBackupRestore = "backup-restore"

	// LabelBasic is a label for  selecting basic test
	LabelBasic = "basic"

	// LabelClusterMetadata is a label for selecting cluster-metadata test
	LabelClusterMetadata = "cluster-metadata"

	// LabelDeclarativePubSub  is a label for selecting the declarative publication / subscription test
	LabelDeclarativePubSub = "declarative-pub-sub"

	// LabelDisruptive is the string for labelling disruptive tests
	LabelDisruptive = "disruptive"

	// LabelImportingDatabases is a label for selecting importing-databases test
	LabelImportingDatabases = "importing-databases"

	// LabelMaintenance is a label for selecting maintenance test
	LabelMaintenance = "maintenance"

	// LabelNoOpenshift is the string for labelling tests that don't run on Openshift
	LabelNoOpenshift = "no-openshift"

	// LabelObservability is a label for selecting observability test
	LabelObservability = "observability"

	// LabelOperator is a label for only selecting operator tests
	LabelOperator = "operator"

	// LabelPerformance is the string for labelling performance tests
	LabelPerformance = "performance"

	// LabelPlugin is a label for selecting plugin test
	LabelPlugin = "plugin"

	// LabelPodScheduling is a label for selecting pod-scheduling test
	LabelPodScheduling = "pod-scheduling"

	// LabelPostgresConfiguration is a label for selecting postgres-configuration test
	LabelPostgresConfiguration = "postgres-configuration"

	// LabelRecovery is a label for selecting recovery test
	LabelRecovery = "recovery"

	// LabelReplication is a label for selecting replication test
	LabelReplication = "replication"

	// LabelSecurity is a label for selecting security test
	LabelSecurity = "security"

	// LabelSelfHealing is a label for selecting self-healing test
	LabelSelfHealing = "self-healing"

	// LabelServiceConnectivity is a label for selecting service connections test
	LabelServiceConnectivity = "service-connectivity"

	// LabelSmoke is a label for selecting  smoke test
	LabelSmoke = "smoke"

	// LabelSnapshot is a label for selecting snapshot tests
	LabelSnapshot = "snapshot"

	// LabelStorage is a label for selecting storage test
	LabelStorage = "storage"

	// LabelTablespaces is a lable for selectin the tablespaces tests
	LabelTablespaces = "tablespaces"

	// LabelUpgrade is the string for labelling upgrade tests
	LabelUpgrade = "upgrade"
)
