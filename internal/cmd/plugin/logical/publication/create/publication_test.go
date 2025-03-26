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

package create

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("create publication SQL generator", func() {
	It("can publish all tables", func() {
		Expect(PublicationCmdBuilder{
			PublicationName:   "app",
			PublicationTarget: PublicationTargetALLTables{},
		}.ToSQL()).To(Equal(`CREATE PUBLICATION "app" FOR ALL TABLES`))
	})

	It("can publish all tables with custom parameters", func() {
		Expect(PublicationCmdBuilder{
			PublicationName:       "app",
			PublicationTarget:     PublicationTargetALLTables{},
			PublicationParameters: "publish='insert'",
		}.ToSQL()).To(Equal(`CREATE PUBLICATION "app" FOR ALL TABLES WITH (publish='insert')`))
	})

	It("can publish a list of tables via multiple publication objects", func() {
		// This is supported from PG 15
		Expect(PublicationCmdBuilder{
			PublicationName: "app",
			PublicationTarget: &PublicationTargetPublicationObjects{
				PublicationObjects: []PublicationObject{
					PublicationObjectTableExpression{
						TableExpressions: []string{"a"},
					},
					PublicationObjectTableExpression{
						TableExpressions: []string{"b"},
					},
				},
			},
		}.ToSQL()).To(Equal(`CREATE PUBLICATION "app" FOR TABLE a, TABLE b`))
	})

	It("can publish a list of tables via multiple table expressions", func() {
		// This is supported in PG < 15
		Expect(PublicationCmdBuilder{
			PublicationName: "app",
			PublicationTarget: &PublicationTargetPublicationObjects{
				PublicationObjects: []PublicationObject{
					PublicationObjectTableExpression{
						TableExpressions: []string{"a", "b"},
					},
				},
			},
		}.ToSQL()).To(Equal(`CREATE PUBLICATION "app" FOR TABLE a, b`))
	})

	It("can publish a schema via multiple publication objects", func() {
		Expect(PublicationCmdBuilder{
			PublicationName: "app",
			PublicationTarget: &PublicationTargetPublicationObjects{
				PublicationObjects: []PublicationObject{
					PublicationObjectSchema{
						SchemaName: "public",
					},
				},
			},
		}.ToSQL()).To(Equal(`CREATE PUBLICATION "app" FOR TABLES IN SCHEMA "public"`))
	})

	It("can publish multiple schemas via multiple publication objects", func() {
		Expect(PublicationCmdBuilder{
			PublicationName: "app",
			PublicationTarget: &PublicationTargetPublicationObjects{
				PublicationObjects: []PublicationObject{
					PublicationObjectSchema{
						SchemaName: "public",
					},
					PublicationObjectSchema{
						SchemaName: "next",
					},
				},
			},
		}.ToSQL()).To(Equal(`CREATE PUBLICATION "app" FOR TABLES IN SCHEMA "public", TABLES IN SCHEMA "next"`))
	})
})
