/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
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
