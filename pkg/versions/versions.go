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

// Package versions contains the version of the CloudNativePG operator and the software
// that is used by it
package versions

const (
	// Version is the version of the operator
	Version = "1.28.1"

	// DefaultImageName is the default image used by the operator to create pods
	DefaultImageName = "ghcr.io/cloudnative-pg/postgresql:18.1-system-trixie"

	// DefaultOperatorImageName is the default operator image used by the controller in the pods running PostgreSQL
	DefaultOperatorImageName = "ghcr.io/cloudnative-pg/cloudnative-pg:1.28.1"

	// DefaultPgbouncerImage is the name of the pgbouncer image used by default
	DefaultPgbouncerImage = "ghcr.io/cloudnative-pg/pgbouncer:1.25.1"
)

// BuildInfo is a struct containing all the info about the build
type BuildInfo struct {
	Version, Commit, Date string
}

var (
	// buildVersion injected during the build
	buildVersion = "1.28.1"

	// buildCommit injected during the build
	buildCommit = "none"

	// buildDate injected during the build
	buildDate = "unknown"

	// Info contains the build info
	Info = BuildInfo{
		Version: buildVersion,
		Commit:  buildCommit,
		Date:    buildDate,
	}
)
