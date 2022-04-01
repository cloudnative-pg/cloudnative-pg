/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

// Package configfile contains primitives needed to manage a configuration file
// with the syntax of PostgreSQL
package configfile

import (
	"fmt"
	"strings"

	"github.com/lib/pq"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/fileutils"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/stringset"
)

// UpdatePostgresConfigurationFile search and replace options in a Postgres configuration file.
// If the configuration file doesn't exist, it will be written.
func UpdatePostgresConfigurationFile(fileName string, options map[string]string) (changed bool, err error) {
	rawCurrentContent, err := fileutils.ReadFile(fileName)
	if err != nil {
		return false, fmt.Errorf("error while reading content of %v: %w", fileName, err)
	}

	updatedContent := UpdateConfigurationContents(string(rawCurrentContent), options)
	return fileutils.WriteStringToFile(fileName, updatedContent)
}

// UpdateConfigurationContents search and replace options in a configuration file whose
// content is passed
func UpdateConfigurationContents(content string, options map[string]string) string {
	lines := splitLines(content)

	// Change matching existing lines
	resultContent := make([]string, 0, len(lines)+len(options))
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

	return strings.Join(resultContent, "\n") + "\n"
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
