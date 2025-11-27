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

package config

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

var _ = Describe("PgBouncer database entries", func() {
	var pooler *apiv1.Pooler

	BeforeEach(func() {
		pooler = &apiv1.Pooler{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pooler",
				Namespace: "default",
			},
			Spec: apiv1.PoolerSpec{
				Cluster: apiv1.LocalObjectReference{
					Name: "test-cluster",
				},
				Type: apiv1.PoolerTypeRW,
				PgBouncer: &apiv1.PgBouncerSpec{
					PoolMode: apiv1.PgBouncerPoolModeSession,
				},
			},
		}
	})

	Describe("buildDatabaseEntries", func() {
		It("returns default wildcard entry when no databases are configured", func() {
			result := buildDatabaseEntries(pooler)
			Expect(result).To(Equal("* = host=test-cluster-rw\n"))
		})

		It("returns custom database entries when configured", func() {
			pooler.Spec.PgBouncer.Databases = []apiv1.PgBouncerDatabaseConfig{
				{
					Name:     "mydb",
					PoolMode: apiv1.PgBouncerPoolModeTransaction,
					Parameters: map[string]string{
						"pool_size": "20",
					},
				},
			}

			result := buildDatabaseEntries(pooler)
			Expect(result).To(ContainSubstring("mydb = host=test-cluster-rw pool_mode=transaction pool_size=20"))
		})

		It("automatically adds wildcard fallback when not specified", func() {
			pooler.Spec.PgBouncer.Databases = []apiv1.PgBouncerDatabaseConfig{
				{
					Name: "mydb",
				},
			}

			result := buildDatabaseEntries(pooler)
			Expect(result).To(ContainSubstring("mydb = host=test-cluster-rw"))
			Expect(result).To(ContainSubstring("* = host=test-cluster-rw"))
		})

		It("ignores wildcard entries and adds default wildcard automatically", func() {
			pooler.Spec.PgBouncer.Databases = []apiv1.PgBouncerDatabaseConfig{
				{
					Name: "mydb",
				},
				{
					Name: "*", // This should be ignored
					Parameters: map[string]string{
						"pool_size": "10",
					},
				},
			}

			result := buildDatabaseEntries(pooler)
			// Count occurrences of wildcard - should be exactly one (the automatic one)
			wildcardCount := strings.Count(result, "* = ")
			Expect(wildcardCount).To(Equal(1))
			// The wildcard should be the default one, not the user-specified one
			Expect(result).To(ContainSubstring("* = host=test-cluster-rw"))
			Expect(result).NotTo(ContainSubstring("pool_size=10"))
		})

		It("includes dbname when specified", func() {
			pooler.Spec.PgBouncer.Databases = []apiv1.PgBouncerDatabaseConfig{
				{
					Name:   "clientdb",
					DBName: "actualdb",
				},
			}

			result := buildDatabaseEntries(pooler)
			Expect(result).To(ContainSubstring("clientdb = host=test-cluster-rw dbname=actualdb"))
		})

		It("always uses cluster service as host", func() {
			pooler.Spec.PgBouncer.Databases = []apiv1.PgBouncerDatabaseConfig{
				{
					Name: "mydb",
				},
			}

			result := buildDatabaseEntries(pooler)
			Expect(result).To(ContainSubstring("mydb = host=test-cluster-rw"))
		})

		It("includes reserve_pool when specified in parameters", func() {
			pooler.Spec.PgBouncer.Databases = []apiv1.PgBouncerDatabaseConfig{
				{
					Name: "mydb",
					Parameters: map[string]string{
						"reserve_pool": "5",
					},
				},
			}

			result := buildDatabaseEntries(pooler)
			Expect(result).To(ContainSubstring("reserve_pool=5"))
		})

		It("includes parameters when specified", func() {
			pooler.Spec.PgBouncer.Databases = []apiv1.PgBouncerDatabaseConfig{
				{
					Name: "mydb",
					Parameters: map[string]string{
						"connect_query": "SET application_name='myapp'",
					},
				},
			}

			result := buildDatabaseEntries(pooler)
			Expect(result).To(ContainSubstring("connect_query=SET application_name='myapp'"))
		})

		It("sorts database entries alphabetically with wildcard last", func() {
			pooler.Spec.PgBouncer.Databases = []apiv1.PgBouncerDatabaseConfig{
				{Name: "zdb"},
				{Name: "adb"},
				{Name: "mdb"},
			}

			result := buildDatabaseEntries(pooler)
			lines := []string{}
			for _, line := range splitLines(result) {
				if line != "" {
					lines = append(lines, line)
				}
			}

			// 3 user databases + 1 automatic wildcard = 4 entries
			Expect(len(lines)).To(Equal(4))
			Expect(lines[0]).To(HavePrefix("adb"))
			Expect(lines[1]).To(HavePrefix("mdb"))
			Expect(lines[2]).To(HavePrefix("zdb"))
			Expect(lines[3]).To(HavePrefix("*"))
		})

		It("always adds wildcard at the end", func() {
			pooler.Spec.PgBouncer.Databases = []apiv1.PgBouncerDatabaseConfig{
				{
					Name:     "analytics",
					PoolMode: apiv1.PgBouncerPoolModeTransaction,
					Parameters: map[string]string{
						"pool_size": "50",
					},
				},
			}

			result := buildDatabaseEntries(pooler)
			Expect(result).To(ContainSubstring("analytics = host=test-cluster-rw pool_mode=transaction pool_size=50"))
			Expect(result).To(ContainSubstring("* = host=test-cluster-rw"))
			// Wildcard should be last
			lines := splitLines(result)
			nonEmptyLines := []string{}
			for _, l := range lines {
				if l != "" {
					nonEmptyLines = append(nonEmptyLines, l)
				}
			}
			Expect(nonEmptyLines[len(nonEmptyLines)-1]).To(HavePrefix("*"))
		})

		It("sorts parameters for stable output", func() {
			pooler.Spec.PgBouncer.Databases = []apiv1.PgBouncerDatabaseConfig{
				{
					Name: "mydb",
					Parameters: map[string]string{
						"z_param": "z_value",
						"a_param": "a_value",
						"m_param": "m_value",
					},
				},
			}

			result := buildDatabaseEntries(pooler)
			// Parameters should be sorted: a_param, m_param, z_param
			Expect(result).To(MatchRegexp(`a_param=a_value.*m_param=m_value.*z_param=z_value`))
		})
	})

	Describe("buildSingleDatabaseEntry", func() {
		It("builds a minimal entry with just host", func() {
			db := apiv1.PgBouncerDatabaseConfig{
				Name: "testdb",
			}

			entry := buildSingleDatabaseEntry(db, "cluster-rw")
			Expect(entry.Name).To(Equal("testdb"))
			Expect(entry.Config).To(Equal("host=cluster-rw"))
		})

		It("builds a complete entry with all options", func() {
			db := apiv1.PgBouncerDatabaseConfig{
				Name:     "fulldb",
				DBName:   "realdb",
				PoolMode: apiv1.PgBouncerPoolModeSession,
				Parameters: map[string]string{
					"pool_size":       "30",
					"reserve_pool":    "10",
					"client_encoding": "UTF8",
				},
			}

			entry := buildSingleDatabaseEntry(db, "cluster-rw")
			Expect(entry.Name).To(Equal("fulldb"))
			Expect(entry.Config).To(ContainSubstring("host=cluster-rw"))
			Expect(entry.Config).To(ContainSubstring("dbname=realdb"))
			Expect(entry.Config).To(ContainSubstring("pool_mode=session"))
			Expect(entry.Config).To(ContainSubstring("pool_size=30"))
			Expect(entry.Config).To(ContainSubstring("reserve_pool=10"))
			Expect(entry.Config).To(ContainSubstring("client_encoding=UTF8"))
		})

		It("uses default host always", func() {
			db := apiv1.PgBouncerDatabaseConfig{
				Name: "testdb",
			}

			entry := buildSingleDatabaseEntry(db, "my-cluster-rw")
			Expect(entry.Config).To(HavePrefix("host=my-cluster-rw"))
		})
	})
})

// splitLines splits a string into lines, handling different line ending styles
func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
