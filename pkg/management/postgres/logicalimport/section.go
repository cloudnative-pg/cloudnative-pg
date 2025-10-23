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

// section is a possible value of the --section flag
type section string

const (
	// sectionPreData represents the pre-data section
	sectionPreData section = "pre-data"

	// sectionData represents the data section
	sectionData section = "data"

	// sectionPostData represents the post-data section
	sectionPostData section = "post-data"
)

// sectionOptions contain the options to be used when invoking the --section flag
type sectionOptions struct {
	// fallbackOptions the default values to use for when no specific option is available
	fallbackOptions []string

	// specificOptions contains specific options for a given section
	specificOptions map[section][]string
}

// forSection gets the options that should be used for the
// option with the specified name
func (opts *sectionOptions) forSection(name section) []string {
	result, ok := opts.specificOptions[name]
	if ok && len(result) > 0 {
		return result
	}

	return opts.fallbackOptions
}

// buildPgRestoreSectionOptions creates the pg_restore options for a database import
func buildPgRestoreSectionOptions(importBootstrap *apiv1.Import) sectionOptions {
	result := sectionOptions{
		fallbackOptions: importBootstrap.PgRestoreExtraOptions,
		specificOptions: make(map[section][]string),
	}

	result.specificOptions[sectionPreData] = importBootstrap.PgRestorePredataOptions
	result.specificOptions[sectionData] = importBootstrap.PgRestoreDataOptions
	result.specificOptions[sectionPostData] = importBootstrap.PgRestorePostdataOptions

	return result
}
