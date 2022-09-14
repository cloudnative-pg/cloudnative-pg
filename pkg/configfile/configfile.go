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

// Package configfile contains primitives needed to manage a configuration file
// with the syntax of PostgreSQL
package configfile

import (
	"fmt"
	"math"
	"strings"

	"github.com/lib/pq"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/cnpgerrors"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/fileutils"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/stringset"
)

// UpdatePostgresConfigurationFile search and replace options in a Postgres configuration file.
// If any managedOptions is passed, it will be removed unless present in the options map.
// If the configuration file doesn't exist, it will be written.
func UpdatePostgresConfigurationFile(
	fileName string,
	options map[string]string,
	managedOptions ...string,
) (changed bool, err error) {
	rawCurrentContent, err := fileutils.ReadFile(fileName)
	if err != nil {
		return false, fmt.Errorf("error while reading content of %v: %w", fileName, err)
	}

	updatedContent := string(rawCurrentContent)

	for _, option := range managedOptions {
		if _, hasOption := options[option]; !hasOption {
			updatedContent = RemoveOptionFromConfigurationContents(updatedContent, option)
		}
	}

	updatedContent, err = UpdateConfigurationContents(updatedContent, options)
	if err != nil {
		return false, fmt.Errorf("error while updating configuration from %v: %w", fileName, err)
	}
	return fileutils.WriteStringToFile(fileName, updatedContent)
}

// UpdateConfigurationContents search and replace options in a configuration file whose
// content is passed
func UpdateConfigurationContents(content string, options map[string]string) (string, error) {
	lines := splitLines(content)
	if len(lines) >= math.MaxInt-len(options) {
		return "", fmt.Errorf("could not updateConfigurationContents: %w",
			cnpgerrors.ErrMemoryAllocation)
	}
	resultLength := len(lines) + len(options)
	// Change matching existing lines
	resultContent := make([]string, 0, resultLength)
	foundKeys := stringset.New()
	for _, line := range lines {
		// Keep empty lines and comments
		trimLine := strings.TrimSpace(line)
		if len(trimLine) == 0 || trimLine[0] == '#' {
			resultContent = append(resultContent, line)
			continue
		}

		kv := strings.SplitN(trimLine, "=", 2)
		key := strings.TrimSpace(kv[0])

		// If we find a line containing one of the option we have to manage,
		// we replace it with the provided content
		if value, ok := options[key]; ok {
			// We output only the first occurrence of the option,
			// discarding further occurrences
			if !foundKeys.Has(key) {
				foundKeys.Put(key)
				resultContent = append(resultContent, key+" = "+pq.QuoteLiteral(value))
			}
			continue
		}

		resultContent = append(resultContent, line)
	}

	// Append missing options to the end of the file
	for key, value := range options {
		if !foundKeys.Has(key) {
			resultContent = append(resultContent, key+" = "+pq.QuoteLiteral(value))
		}
	}

	return strings.Join(resultContent, "\n") + "\n", nil
}

// RemoveOptionFromConfigurationContents deletes the lines containing the given option a configuration file whose
// content is passed
func RemoveOptionFromConfigurationContents(content string, option string) string {
	resultContent := []string{}

	for _, line := range splitLines(content) {
		// Keep empty lines and comments
		trimLine := strings.TrimSpace(line)
		if len(trimLine) == 0 || trimLine[0] == '#' {
			resultContent = append(resultContent, line)
			continue
		}

		kv := strings.SplitN(trimLine, "=", 2)
		key := strings.TrimSpace(kv[0])

		// If we find a line containing the input option,
		// we skip it
		if key == option {
			continue
		}

		resultContent = append(resultContent, line)
	}

	return strings.Join(resultContent, "\n") + "\n"
}
