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

package metrics

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Set default queries", func() {
	q := NewQueriesCollector("test", nil, "db")

	It("does assign nothing with empty default queries", func() {
		Expect(q.userQueries).To(BeEmpty())
		Expect(q.mappings).To(BeEmpty())
		Expect(q.variableLabels).To(BeEmpty())
		Expect(q.userQueries).To(BeEmpty())
		Expect(q.mappings).To(BeEmpty())
		Expect(q.variableLabels).To(BeEmpty())
	})

	It("properly works", func() {
		Expect(q.userQueries).To(BeEmpty())
		Expect(q.mappings).To(BeEmpty())
		Expect(q.variableLabels).To(BeEmpty())

		defaultQueries := UserQueries{
			"collector": UserQuery{
				Query: "SELECT FROM unit_tests",
				Metrics: []Mapping{
					{
						"test": ColumnMapping{
							Usage:       LABEL,
							Description: "test query",
						},
					},
				},
			},
		}

		q.InjectUserQueries(defaultQueries)
		Expect(len(q.userQueries)).To(BeEquivalentTo(1))
		Expect(len(q.mappings)).To(BeEquivalentTo(1))
		Expect(q.mappings["collector"]["test"].Name).To(BeEquivalentTo("test"))
		Expect(q.variableLabels["collector"]).To(BeEquivalentTo(VariableSet{"test"}))
	})
})
