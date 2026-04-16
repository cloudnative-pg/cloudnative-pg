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

package configuration

import (
	"slices"
	"strings"
	"testing"
)

func FuzzConfigurationParsing(f *testing.F) {
	f.Add("qa.test.com/*,prod.test.com/*", "qa.test.com/one", ",  ,pg ,pg_staging   ,  pg_prod, ", "a,,,b , c")
	f.Add("[abc,prod.testing.com/*", "prod.testing.com/two", ",  ,,", "")
	f.Add("one,two", "three", "pg", "plugin-a,plugin-b")

	f.Fuzz(func(t *testing.T, rawPatterns, value, watchNamespace, includePlugins string) {
		patterns := strings.Split(rawPatterns, ",")

		config := Data{
			InheritedAnnotations: patterns,
			InheritedLabels:      patterns,
			WatchNamespace:       watchNamespace,
			IncludePlugins:       includePlugins,
		}

		expectedMatch := evaluateGlobPatterns(patterns, value)
		if got := config.IsAnnotationInherited(value); got != expectedMatch {
			t.Fatalf("annotation inheritance mismatch: got %t want %t", got, expectedMatch)
		}
		if got := config.IsLabelInherited(value); got != expectedMatch {
			t.Fatalf("label inheritance mismatch: got %t want %t", got, expectedMatch)
		}

		watchedNamespaces := config.WatchedNamespaces()
		expectedNamespaces := cleanNamespaceList(watchNamespace)
		if !slices.Equal(watchedNamespaces, expectedNamespaces) {
			t.Fatalf("watched namespaces mismatch: got %#v want %#v", watchedNamespaces, expectedNamespaces)
		}
		for _, namespace := range watchedNamespaces {
			if namespace == "" {
				t.Fatalf("empty namespace returned from %q", watchNamespace)
			}
			if namespace != strings.TrimSpace(namespace) {
				t.Fatalf("namespace is not trimmed: %q", namespace)
			}
		}

		plugins := config.GetIncludePlugins()
		if !slices.Equal(plugins, config.GetIncludePlugins()) {
			t.Fatalf("plugin parsing must be deterministic")
		}
		for _, plugin := range plugins {
			if plugin == "" {
				t.Fatalf("empty plugin returned from %q", includePlugins)
			}
			if plugin != strings.TrimSpace(plugin) {
				t.Fatalf("plugin is not trimmed: %q", plugin)
			}
		}
	})
}
