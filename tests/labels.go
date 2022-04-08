/*
Copyright 2019-2022 The CloudNativePG Contributors

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

// Package tests contains the test infrastructure of the Cloud Native PostgreSQL operator
package tests

// List of the labels we use for labeling test specs
// See https://github.com/onsi/ginkgo/v2/blob/ver2/docs/MIGRATING_TO_V2.md#label-decoration
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

	// LabelBackupRestore is a label for only selecting backup and restore tests
	LabelBackupRestore = "backup-restore"
)
