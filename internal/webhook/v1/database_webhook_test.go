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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"

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
	createFDWSpec := func(name string) apiv1.FDWSpec {
		return apiv1.FDWSpec{
			DatabaseObjectSpec: apiv1.DatabaseObjectSpec{
				Name:   name,
				Ensure: apiv1.EnsurePresent,
			},
		}
	}

	// helper to extract the field paths (e.g. spec.extensions[1].name) from the error list
	extractErrorFields := func(errs field.ErrorList) []string {
		res := make([]string, 0, len(errs))
		for _, e := range errs {
			res = append(res, e.Field)
		}
		return res
	}

	// expectDuplicateErrors validates that we have exactly the expected duplicate errors with correct bad values
	expectDuplicateErrors := func(errs field.ErrorList, expected map[string]string) {
		Expect(errs).To(HaveLen(len(expected)))
		for path, val := range expected {
			var found *field.Error
			for i := range errs {
				if errs[i].Field == path {
					found = errs[i]
					break
				}
			}
			Expect(found).To(HaveOccurred(), "missing duplicate error for %s", path)
			Expect(found.Type).To(Equal(field.ErrorTypeDuplicate), "expected duplicate error type for %s", path)
			Expect(found.BadValue).To(Equal(val), "unexpected bad value for %s", path)
		}
	}

	BeforeEach(func() {
		v = &DatabaseCustomValidator{}
	})

	It("doesn't complain when extensions and schemas are null", func() {
		db := &apiv1.Database{Spec: apiv1.DatabaseSpec{}}
		errs := v.validate(db)
		Expect(errs).To(BeEmpty())
	})

	It("doesn't complain if there are no duplicate extensions and no duplicate schemas", func() {
		db := &apiv1.Database{
			Spec: apiv1.DatabaseSpec{
				Extensions: []apiv1.ExtensionSpec{
					createExtensionSpec("postgis"),
				},
				Schemas: []apiv1.SchemaSpec{
					createSchemaSpec("test_schema"),
				},
			},
		}
		errs := v.validate(db)
		Expect(errs).To(BeEmpty())
	})

	It("complains if there are duplicate extensions", func() {
		db := &apiv1.Database{
			Spec: apiv1.DatabaseSpec{
				Extensions: []apiv1.ExtensionSpec{
					createExtensionSpec("postgis"),
					createExtensionSpec("postgis"),
					createExtensionSpec("cube"),
				},
			},
		}
		errs := v.validate(db)
		Expect(extractErrorFields(errs)).To(ConsistOf("spec.extensions[1].name"))
		expectDuplicateErrors(errs, map[string]string{"spec.extensions[1].name": "postgis"})
	})

	It("complains if there are duplicate schemas", func() {
		db := &apiv1.Database{
			Spec: apiv1.DatabaseSpec{
				Schemas: []apiv1.SchemaSpec{
					createSchemaSpec("test_one"),
					createSchemaSpec("test_two"),
					createSchemaSpec("test_two"),
				},
			},
		}
		errs := v.validate(db)
		Expect(extractErrorFields(errs)).To(ConsistOf("spec.schemas[2].name"))
		expectDuplicateErrors(errs, map[string]string{"spec.schemas[2].name": "test_two"})
	})

	It("doesn't complain with distinct FDWs and usage names", func() {
		db := &apiv1.Database{
			Spec: apiv1.DatabaseSpec{
				FDWs: []apiv1.FDWSpec{
					{
						DatabaseObjectSpec: apiv1.DatabaseObjectSpec{Name: "fdw1", Ensure: apiv1.EnsurePresent},
						Usages:             []apiv1.UsageSpec{{Name: "usage1"}, {Name: "usage2"}},
						Options:            []apiv1.OptionSpec{{Name: "option1"}, {Name: "option2"}},
					},
					{
						DatabaseObjectSpec: apiv1.DatabaseObjectSpec{Name: "fdw2", Ensure: apiv1.EnsurePresent},
						Usages:             []apiv1.UsageSpec{{Name: "usage3"}, {Name: "usage4"}},
						Options:            []apiv1.OptionSpec{{Name: "option3"}, {Name: "option4"}},
					},
				},
			},
		}
		errs := v.validate(db)
		Expect(errs).To(BeEmpty())
	})

	It("complains if there are duplicate FDWs", func() {
		db := &apiv1.Database{
			Spec: apiv1.DatabaseSpec{
				FDWs: []apiv1.FDWSpec{
					createFDWSpec("postgres_fdw"),
					createFDWSpec("mysql_fdw"),
					createFDWSpec("postgres_fdw"),
				},
			},
		}
		errs := v.validate(db)
		Expect(extractErrorFields(errs)).To(ConsistOf("spec.fdws[2].name"))
		expectDuplicateErrors(errs, map[string]string{"spec.fdws[2].name": "postgres_fdw"})
	})

	It("complains if there are duplicate usage names within an FDW", func() {
		db := &apiv1.Database{
			Spec: apiv1.DatabaseSpec{
				FDWs: []apiv1.FDWSpec{
					{
						DatabaseObjectSpec: apiv1.DatabaseObjectSpec{Name: "postgre_fdw", Ensure: apiv1.EnsurePresent},
						Usages:             []apiv1.UsageSpec{{Name: "usage1"}, {Name: "usage2"}, {Name: "usage1"}},
					},
				},
			},
		}
		errs := v.validate(db)
		Expect(extractErrorFields(errs)).To(ConsistOf("spec.fdws[0].usages[2].name"))
		expectDuplicateErrors(errs, map[string]string{"spec.fdws[0].usages[2].name": "usage1"})
	})

	It("complains for duplicate FDW and duplicate usage and option names", func() { // consolidated wording
		db := &apiv1.Database{
			Spec: apiv1.DatabaseSpec{
				FDWs: []apiv1.FDWSpec{
					{
						DatabaseObjectSpec: apiv1.DatabaseObjectSpec{Name: "duplicate_fdw", Ensure: apiv1.EnsurePresent},
						Usages:             []apiv1.UsageSpec{{Name: "dup_usage"}, {Name: "dup_usage"}},
						Options:            []apiv1.OptionSpec{{Name: "dup_option"}, {Name: "dup_option"}},
					},
					{
						DatabaseObjectSpec: apiv1.DatabaseObjectSpec{Name: "duplicate_fdw", Ensure: apiv1.EnsurePresent},
					},
				},
			},
		}
		errs := v.validate(db)
		Expect(extractErrorFields(errs)).To(ConsistOf(
			"spec.fdws[0].options[1].name",
			"spec.fdws[0].usages[1].name",
			"spec.fdws[1].name",
		))
		expectDuplicateErrors(errs, map[string]string{
			"spec.fdws[0].options[1].name": "dup_option",
			"spec.fdws[0].usages[1].name":  "dup_usage",
			"spec.fdws[1].name":            "duplicate_fdw",
		})
	})

	It("doesn't complain with distinct foreign servers", func() {
		db := &apiv1.Database{
			Spec: apiv1.DatabaseSpec{
				FDWs: []apiv1.FDWSpec{
					{
						DatabaseObjectSpec: apiv1.DatabaseObjectSpec{Name: "fdw1"},
					},
					{
						DatabaseObjectSpec: apiv1.DatabaseObjectSpec{Name: "fdw2"},
					},
				},
				Servers: []apiv1.ServerSpec{
					{DatabaseObjectSpec: apiv1.DatabaseObjectSpec{Name: "server1", Ensure: apiv1.EnsurePresent}, FdwName: "fdw1"},
					{DatabaseObjectSpec: apiv1.DatabaseObjectSpec{Name: "server2", Ensure: apiv1.EnsurePresent}, FdwName: "fdw2"},
				},
			},
		}
		errs := v.validate(db)
		Expect(errs).To(BeEmpty())
	})

	It("complains for duplicate foreign servers", func() {
		db := &apiv1.Database{
			Spec: apiv1.DatabaseSpec{
				FDWs: []apiv1.FDWSpec{{DatabaseObjectSpec: apiv1.DatabaseObjectSpec{Name: "fdw1"}}},
				Servers: []apiv1.ServerSpec{
					{DatabaseObjectSpec: apiv1.DatabaseObjectSpec{Name: "dup_server", Ensure: apiv1.EnsurePresent}, FdwName: "fdw1"},
					{DatabaseObjectSpec: apiv1.DatabaseObjectSpec{Name: "dup_server", Ensure: apiv1.EnsurePresent}, FdwName: "fdw1"},
				},
			},
		}
		errs := v.validate(db)
		Expect(extractErrorFields(errs)).To(ConsistOf("spec.servers[1].name"))
		expectDuplicateErrors(errs, map[string]string{"spec.servers[1].name": "dup_server"})
	})

	It("complains for duplicate options within a single foreign server", func() {
		db := &apiv1.Database{
			Spec: apiv1.DatabaseSpec{
				FDWs: []apiv1.FDWSpec{{DatabaseObjectSpec: apiv1.DatabaseObjectSpec{Name: "fdw1"}}},
				Servers: []apiv1.ServerSpec{
					{
						FdwName:            "fdw1",
						DatabaseObjectSpec: apiv1.DatabaseObjectSpec{Name: "server1", Ensure: apiv1.EnsurePresent},
						Options:            []apiv1.OptionSpec{{Name: "duplicate_option"}, {Name: "duplicate_option"}},
					},
				},
			},
		}
		errs := v.validate(db)
		Expect(extractErrorFields(errs)).To(ConsistOf("spec.servers[0].options[1].name"))
		expectDuplicateErrors(errs, map[string]string{"spec.servers[0].options[1].name": "duplicate_option"})
	})

	It("complains for duplicate usages within a single foreign server", func() {
		db := &apiv1.Database{
			Spec: apiv1.DatabaseSpec{
				FDWs: []apiv1.FDWSpec{{DatabaseObjectSpec: apiv1.DatabaseObjectSpec{Name: "fdw1"}}},
				Servers: []apiv1.ServerSpec{
					{
						FdwName:            "fdw1",
						DatabaseObjectSpec: apiv1.DatabaseObjectSpec{Name: "server1", Ensure: apiv1.EnsurePresent},
						Usages:             []apiv1.UsageSpec{{Name: "duplicate_usage"}, {Name: "duplicate_usage"}},
					},
				},
			},
		}
		errs := v.validate(db)
		Expect(extractErrorFields(errs)).To(ConsistOf("spec.servers[0].usages[1].name"))
		expectDuplicateErrors(errs, map[string]string{"spec.servers[0].usages[1].name": "duplicate_usage"})
	})

	It("complains for a combination of duplicate servers, options, and usages", func() {
		db := &apiv1.Database{
			Spec: apiv1.DatabaseSpec{
				FDWs: []apiv1.FDWSpec{{DatabaseObjectSpec: apiv1.DatabaseObjectSpec{Name: "fdw1"}}},
				Servers: []apiv1.ServerSpec{
					{
						FdwName:            "fdw1",
						DatabaseObjectSpec: apiv1.DatabaseObjectSpec{Name: "server1", Ensure: apiv1.EnsurePresent},
						Options:            []apiv1.OptionSpec{{Name: "dup_option"}, {Name: "dup_option"}},
						Usages:             []apiv1.UsageSpec{{Name: "dup_usage"}, {Name: "dup_usage"}},
					},
					{
						FdwName:            "fdw1",
						DatabaseObjectSpec: apiv1.DatabaseObjectSpec{Name: "server1", Ensure: apiv1.EnsurePresent},
					},
				},
			},
		}
		errs := v.validate(db)
		Expect(extractErrorFields(errs)).To(ConsistOf(
			"spec.servers[0].options[1].name",
			"spec.servers[0].usages[1].name",
			"spec.servers[1].name",
		))
		expectDuplicateErrors(errs, map[string]string{
			"spec.servers[0].options[1].name": "dup_option",
			"spec.servers[0].usages[1].name":  "dup_usage",
			"spec.servers[1].name":            "server1",
		})
	})
})

var _ = Describe("Database namespace immutability validation", func() {
	var v *DatabaseCustomValidator

	BeforeEach(func() {
		v = &DatabaseCustomValidator{}
	})

	It("allows update when cluster.namespace is unchanged", func() {
		oldDB := &apiv1.Database{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "mydb",
				Namespace: "app-namespace",
			},
			Spec: apiv1.DatabaseSpec{
				ClusterRef: apiv1.ClusterObjectReference{
					Name:      "my-cluster",
					Namespace: "cluster-namespace",
				},
				Name:  "mydb",
				Owner: "app",
			},
		}

		newDB := oldDB.DeepCopy()
		newDB.Spec.Owner = "new-owner" // Change something else

		errs := v.validateDatabaseChanges(newDB, oldDB)
		Expect(errs).To(BeEmpty())
	})

	It("allows update when cluster.namespace was empty and remains empty", func() {
		oldDB := &apiv1.Database{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "mydb",
				Namespace: "app-namespace",
			},
			Spec: apiv1.DatabaseSpec{
				ClusterRef: apiv1.ClusterObjectReference{
					Name: "my-cluster",
				},
				Name:  "mydb",
				Owner: "app",
			},
		}

		newDB := oldDB.DeepCopy()
		newDB.Spec.Owner = "new-owner"

		errs := v.validateDatabaseChanges(newDB, oldDB)
		Expect(errs).To(BeEmpty())
	})

	It("allows setting cluster.namespace when it was previously empty", func() {
		oldDB := &apiv1.Database{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "mydb",
				Namespace: "app-namespace",
			},
			Spec: apiv1.DatabaseSpec{
				ClusterRef: apiv1.ClusterObjectReference{
					Name: "my-cluster",
				},
				Name:  "mydb",
				Owner: "app",
			},
		}

		newDB := oldDB.DeepCopy()
		newDB.Spec.ClusterRef.Namespace = "cluster-namespace"

		errs := v.validateDatabaseChanges(newDB, oldDB)
		Expect(errs).To(BeEmpty())
	})

	It("rejects changing cluster.namespace once it is set", func() {
		oldDB := &apiv1.Database{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "mydb",
				Namespace: "app-namespace",
			},
			Spec: apiv1.DatabaseSpec{
				ClusterRef: apiv1.ClusterObjectReference{
					Name:      "my-cluster",
					Namespace: "original-namespace",
				},
				Name:  "mydb",
				Owner: "app",
			},
		}

		newDB := oldDB.DeepCopy()
		newDB.Spec.ClusterRef.Namespace = "different-namespace"

		errs := v.validateDatabaseChanges(newDB, oldDB)
		Expect(errs).To(HaveLen(1))
		Expect(errs[0].Field).To(Equal("spec.cluster.namespace"))
		Expect(errs[0].Type).To(Equal(field.ErrorTypeInvalid))
		Expect(errs[0].Detail).To(ContainSubstring("immutable"))
	})

	It("rejects clearing cluster.namespace once it is set", func() {
		oldDB := &apiv1.Database{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "mydb",
				Namespace: "app-namespace",
			},
			Spec: apiv1.DatabaseSpec{
				ClusterRef: apiv1.ClusterObjectReference{
					Name:      "my-cluster",
					Namespace: "original-namespace",
				},
				Name:  "mydb",
				Owner: "app",
			},
		}

		newDB := oldDB.DeepCopy()
		newDB.Spec.ClusterRef.Namespace = ""

		errs := v.validateDatabaseChanges(newDB, oldDB)
		Expect(errs).To(HaveLen(1))
		Expect(errs[0].Field).To(Equal("spec.cluster.namespace"))
	})
})
