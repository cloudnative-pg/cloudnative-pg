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

package logicalimport

import apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

// PgRestoreSectionName is the name of a "pg_restore" section
type PgRestoreSectionName string

const (
	// PgRestoreSectionPreData represents the pre-data section
	PgRestoreSectionPreData PgRestoreSectionName = "pre-data"

	// PgRestoreSectionData represents the data section
	PgRestoreSectionData PgRestoreSectionName = "data"

	// PgRestoreSectionPostData represents the post-data section
	PgRestoreSectionPostData PgRestoreSectionName = "post-data"
)

// pgRestoreSectionOptions contain the options to be used when invoking
// "pg_restore"
type pgRestoreSectionOptions struct {
	// commonOptions is the default value of the
	// section-specific field
	commonOptions []string

	// specificOptions maps the section name to the options
	// specific for that section
	specificOptions map[PgRestoreSectionName][]string
}

// ForSection gets the options that should be used for the
// option with the specified name
func (opts *pgRestoreSectionOptions) ForSection(name PgRestoreSectionName) []string {
	result, ok := opts.specificOptions[name]
	if ok && len(result) > 0 {
		return result
	}

	return opts.commonOptions
}

// pgRestoreOptionsForImport creates the pg_restore options for a database import
func pgRestoreOptionsForImport(importBootstrap *apiv1.Import) pgRestoreSectionOptions {
	result := pgRestoreSectionOptions{
		commonOptions:   importBootstrap.PgRestoreExtraOptions,
		specificOptions: make(map[PgRestoreSectionName][]string),
	}

	result.specificOptions[PgRestoreSectionPreData] = importBootstrap.PgRestorePredataOptions
	result.specificOptions[PgRestoreSectionData] = importBootstrap.PgRestoreDataOptions
	result.specificOptions[PgRestoreSectionPostData] = importBootstrap.PgRestorePostdataOptions

	return result
}
