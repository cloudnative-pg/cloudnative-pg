/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package configuration

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Annotation and label inheritance", func() {
	It("manages inherited annotations", func() {
		config := Data{
			InheritedAnnotations: []string{"one", "two"},
		}

		Expect(config.IsAnnotationInherited("one")).To(BeTrue())
		Expect(config.IsAnnotationInherited("two")).To(BeTrue())
		Expect(config.IsAnnotationInherited("three")).To(BeFalse())
	})

	It("manages inherited labels", func() {
		config := Data{
			InheritedLabels: []string{"alpha", "beta"},
		}

		Expect(config.IsLabelInherited("alpha")).To(BeTrue())
		Expect(config.IsLabelInherited("beta")).To(BeTrue())
		Expect(config.IsLabelInherited("gamma")).To(BeFalse())
	})

	It("manages inherited annotations containing glob patterns", func() {
		config := Data{
			InheritedAnnotations: []string{"qa.test.com/*", "prod.test.com/*"},
		}

		Expect(config.IsAnnotationInherited("qa.test.com/one")).To(BeTrue())
		Expect(config.IsAnnotationInherited("prod.test.com/two")).To(BeTrue())
		Expect(config.IsAnnotationInherited("testing.test.com/three")).To(BeFalse())
	})

	It("manages inherited labels containing glob patterns", func() {
		config := Data{
			InheritedLabels: []string{"qa.testing.com/*", "prod.testing.com/*"},
		}

		Expect(config.IsLabelInherited("qa.testing.com/one")).To(BeTrue())
		Expect(config.IsLabelInherited("prod.testing.com/two")).To(BeTrue())
		Expect(config.IsLabelInherited("testing.testing.com/three")).To(BeFalse())
	})

	It("skips invalid patterns during evaluation", func() {
		config := Data{
			InheritedLabels: []string{"[abc", "prod.testing.com/*"},
		}

		Expect(config.IsLabelInherited("prod.testing.com/two")).To(BeTrue())
		Expect(config.IsLabelInherited("testing.testing.com/three")).To(BeFalse())
	})

	When("every namespace is watched", func() {
		It("sets the watched namespaces to empty", func() {
			config := Data{
				WatchNamespace: "",
			}
			Expect(config.WatchedNamespaces()).To(HaveLen(0))

			// Additional commas and spaces doesn't change the meaning
			config = Data{
				WatchNamespace: ",  ,,",
			}
			Expect(config.WatchedNamespaces()).To(HaveLen(0))
		})
	})

	When("a single namespace is watched", func() {
		It("sets the watched namespaces to that one", func() {
			config := Data{
				WatchNamespace: "pg",
			}
			Expect(config.WatchedNamespaces()).To(HaveLen(1))
			Expect(config.WatchedNamespaces()[0]).To(Equal("pg"))

			// Additional commas and spaces doesn't change the meaning
			config = Data{
				WatchNamespace: ",  ,pg, ",
			}
			Expect(config.WatchedNamespaces()).To(HaveLen(1))
			Expect(config.WatchedNamespaces()[0]).To(Equal("pg"))
		})
	})

	When("multiple namespaces are specified", func() {
		It("sets the watched namespaces to the correct list", func() {
			config := Data{
				WatchNamespace: "pg,pg_staging,pg_prod",
			}
			Expect(config.WatchedNamespaces()).To(HaveLen(3))
			Expect(config.WatchedNamespaces()).To(Equal([]string{
				"pg",
				"pg_staging",
				"pg_prod",
			}))

			// Additional commas and spaces doesn't change the meaning
			config = Data{
				WatchNamespace: ",  ,pg ,pg_staging   ,  pg_prod, ",
			}
			Expect(config.WatchedNamespaces()).To(HaveLen(3))
			Expect(config.WatchedNamespaces()).To(Equal([]string{
				"pg",
				"pg_staging",
				"pg_prod",
			}))
		})
	})
})
