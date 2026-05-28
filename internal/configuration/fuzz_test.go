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
	f.Add("a*", "abc", "", "")
	f.Add("\x00", "\x00", "ns\x00", "p\x00")

	f.Fuzz(func(t *testing.T, rawPatterns, value, watchNamespace, includePlugins string) {
		patterns := strings.Split(rawPatterns, ",")

		config := Data{
			InheritedAnnotations: patterns,
			InheritedLabels:      patterns,
			WatchNamespace:       watchNamespace,
			IncludePlugins:       includePlugins,
		}

		matched := config.IsAnnotationInherited(value)
		for _, pattern := range patterns {
			// pattern has no glob metacharacters; path.Match must treat it literally
			if pattern == value && !strings.ContainsAny(pattern, "*?[\\") {
				if !matched {
					t.Fatalf("exact-match pattern %q did not match value %q", pattern, value)
				}
				break
			}
		}

		namespaces := config.WatchedNamespaces()
		rejoined := Data{WatchNamespace: strings.Join(namespaces, ",")}
		if got := rejoined.WatchedNamespaces(); !slices.Equal(got, namespaces) {
			t.Fatalf("WatchedNamespaces not idempotent: %#v -> %#v", namespaces, got)
		}
		for _, namespace := range namespaces {
			if namespace == "" {
				t.Fatalf("empty namespace returned from %q", watchNamespace)
			}
			if namespace != strings.TrimSpace(namespace) {
				t.Fatalf("namespace not trimmed: %q (from %q)", namespace, watchNamespace)
			}
		}

		plugins := config.GetIncludePlugins()
		rejoinedPlugins := Data{IncludePlugins: strings.Join(plugins, ",")}
		if got := rejoinedPlugins.GetIncludePlugins(); !slices.Equal(got, plugins) {
			t.Fatalf("GetIncludePlugins not idempotent: %#v -> %#v", plugins, got)
		}
		for _, plugin := range plugins {
			if plugin == "" {
				t.Fatalf("empty plugin returned from %q", includePlugins)
			}
			if plugin != strings.TrimSpace(plugin) {
				t.Fatalf("plugin not trimmed: %q (from %q)", plugin, includePlugins)
			}
		}
	})
}
