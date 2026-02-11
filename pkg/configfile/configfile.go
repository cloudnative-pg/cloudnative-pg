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

// Package configfile contains primitives needed to manage a configuration file
// with the syntax of PostgreSQL
package configfile

import (
	"fmt"
	"strings"

	"github.com/cloudnative-pg/machinery/pkg/fileutils"
	"github.com/cloudnative-pg/machinery/pkg/stringset"
	"github.com/lib/pq"
)

// UpdatePostgresConfigurationFile search and replace options in a Postgres configuration file.
// If any managedOptions is passed, it will be removed unless present in the options map.
// If the configuration file doesn't exist, it will be written.
func UpdatePostgresConfigurationFile(
	fileName string,
	options map[string]string,
	managedOptions ...string,
) (changed bool, err error) {
	lines, err := fileutils.ReadFileLines(fileName)
	if err != nil {
		return false, fmt.Errorf("error while reading content of %v: %w", fileName, err)
	}

	optionsToRemove := make([]string, 0, len(managedOptions))
	for _, option := range managedOptions {
		if _, hasOption := options[option]; !hasOption {
			optionsToRemove = append(optionsToRemove, option)
		}
	}
	lines = RemoveOptionsFromConfigurationContents(lines, optionsToRemove...)

	lines, err = UpdateConfigurationContents(lines, options)
	if err != nil {
		return false, fmt.Errorf("error while updating configuration from %v: %w", fileName, err)
	}
	return fileutils.WriteLinesToFile(fileName, lines)
}

// UpdateConfigurationContents search and replace options in a configuration file whose
// content is passed
func UpdateConfigurationContents(lines []string, options map[string]string) ([]string, error) {
	foundKeys := stringset.New()
	index := 0
	for _, line := range lines {
		kv := strings.SplitN(strings.TrimSpace(line), "=", 2)
		key := strings.TrimSpace(kv[0])

		// If we find a line containing one of the option we have to manage,
		// we replace it with the provided content
		if value, has := options[key]; has {
			// We output only the first occurrence of the option,
			// discarding further occurrences
			if foundKeys.Has(key) {
				continue
			}

			foundKeys.Put(key)
			lines[index] = fmt.Sprintf("%s = %s", key, pq.QuoteLiteral(value))
			index++
			continue
		}

		lines[index] = line
		index++
	}
	lines = lines[:index]

	// Append missing options to the end of the file
	keysList := stringset.FromKeys(options).ToSortedList()
	for _, key := range keysList {
		if !foundKeys.Has(key) {
			value := options[key]
			lines = append(lines, fmt.Sprintf("%s = %s", key, pq.QuoteLiteral(value)))
		}
	}

	return lines, nil
}

// WritePostgresConfiguration replaces the content of a PostgreSQL configuration
// file with the provided options
func WritePostgresConfiguration(
	fileName string,
	options map[string]string,
) (changed bool, err error) {
	lines, err := UpdateConfigurationContents(nil, options)
	if err != nil {
		return false, fmt.Errorf("error while writing configuration to %v: %w", fileName, err)
	}
	return fileutils.WriteLinesToFile(fileName, lines)
}

// RemoveOptionsFromConfigurationContents deletes all the lines containing one of the given options
// from the provided configuration content
func RemoveOptionsFromConfigurationContents(lines []string, options ...string) []string {
	optionSet := stringset.From(options)

	index := 0
	for _, line := range lines {
		kv := strings.SplitN(strings.TrimSpace(line), "=", 2)
		key := strings.TrimSpace(kv[0])

		if optionSet.Has(key) {
			continue
		}
		lines[index] = line
		index++
	}
	lines = lines[:index]

	return lines
}

// EnsureIncludes makes sure the passed PostgreSQL configuration file has an include directive
// to every filesToInclude.
func EnsureIncludes(fileName string, filesToInclude ...string) (changed bool, err error) {
	includeLinesToAdd := make(map[string]string, len(filesToInclude))
	for _, fileToInclude := range filesToInclude {
		includeLinesToAdd[fileToInclude] = fmt.Sprintf("include '%v'", fileToInclude)
	}

	lines, err := fileutils.ReadFileLines(fileName)
	if err != nil {
		return false, fmt.Errorf("error while reading lines of %v: %w", fileName, err)
	}

	for _, line := range lines {
		trimLine := strings.TrimSpace(line)
		for targetFile, includeLine := range includeLinesToAdd {
			if trimLine == includeLine {
				delete(includeLinesToAdd, targetFile)
			}
		}
	}

	if len(includeLinesToAdd) == 0 {
		return false, nil
	}

	for _, fileToInclude := range filesToInclude {
		if includeLine, present := includeLinesToAdd[fileToInclude]; present {
			lines = append(lines,
				"",
				fmt.Sprintf("# load CloudNativePG %s configuration", fileToInclude),
				includeLine,
			)
		}
	}

	return fileutils.WriteLinesToFile(fileName, lines)
}
