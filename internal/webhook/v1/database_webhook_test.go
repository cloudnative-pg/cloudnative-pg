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

package v1

import (
	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Database validation", func() {
	var v *DatabaseCustomValidator

	createExtensionSpec := func(name string) apiv1.ExtensionSpec {
		return apiv1.ExtensionSpec{
			DatabaseObjectSpec: apiv1.DatabaseObjectSpec{
				Name:   name,
				Ensure: apiv1.EnsurePresent,
			},
		}
	}
	createSchemaSpec := func(name string) apiv1.SchemaSpec {
		return apiv1.SchemaSpec{
			DatabaseObjectSpec: apiv1.DatabaseObjectSpec{
				Name:   name,
				Ensure: apiv1.EnsurePresent,
			},
		}
	}

	BeforeEach(func() {
		v = &DatabaseCustomValidator{}
	})

	DescribeTable(
		"Database validation",
		func(db *apiv1.Database, errorCount int) {
			foundErrors := v.validate(db)
			Expect(foundErrors).To(HaveLen(errorCount))
		},
		Entry(
			"doesn't complain when extensions and schemas are null",
			&apiv1.Database{
				Spec: apiv1.DatabaseSpec{},
			},
			0,
		),
		Entry(
			"doesn't complain if there are no duplicate extensions and no duplicate schemas",
			&apiv1.Database{
				Spec: apiv1.DatabaseSpec{
					Extensions: []apiv1.ExtensionSpec{
						createExtensionSpec("postgis"),
					},
					Schemas: []apiv1.SchemaSpec{
						createSchemaSpec("test_schema"),
					},
				},
			},
			0,
		),
		Entry(
			"complain if there are duplicate extensions",
			&apiv1.Database{
				Spec: apiv1.DatabaseSpec{
					Extensions: []apiv1.ExtensionSpec{
						createExtensionSpec("postgis"),
						createExtensionSpec("postgis"),
						createExtensionSpec("cube"),
					},
				},
			},
			1,
		),

		Entry(
			"complain if there are duplicate schemas",
			&apiv1.Database{
				Spec: apiv1.DatabaseSpec{
					Schemas: []apiv1.SchemaSpec{
						createSchemaSpec("test_one"),
						createSchemaSpec("test_two"),
						createSchemaSpec("test_two"),
					},
				},
			},
			1,
		),
	)
})
