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
	"time"

	. "github.com/onsi/ginkgo/v2"
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
			Expect(config.WatchedNamespaces()).To(BeEmpty())

			// Additional commas and spaces doesn't change the meaning
			config = Data{
				WatchNamespace: ",  ,,",
			}
			Expect(config.WatchedNamespaces()).To(BeEmpty())
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

	Context("included plugin list", func() {
		It("is empty by default", func() {
			Expect(newDefaultConfig().GetIncludePlugins()).To(BeEmpty())
		})

		It("contains a set of comma-separated plugins", func() {
			Expect((&Data{
				IncludePlugins: "a,b,c",
			}).GetIncludePlugins()).To(ContainElements("a", "b", "c"))
			Expect((&Data{
				IncludePlugins: "a,,,b,c",
			}).GetIncludePlugins()).To(ContainElements("a", "b", "c"))
			Expect((&Data{
				IncludePlugins: "a,,,b , c",
			}).GetIncludePlugins()).To(ContainElements("a", "b", "c"))
		})
	})

	It("returns correct delay for clusters rollout", func() {
		config := Data{ClustersRolloutDelay: 10}
		Expect(config.GetClustersRolloutDelay()).To(Equal(10 * time.Second))
	})

	It("returns zero as default delay for clusters rollout when not set", func() {
		config := Data{}
		Expect(config.GetClustersRolloutDelay()).To(BeZero())
	})

	It("returns correct delay for instances rollout", func() {
		config := Data{InstancesRolloutDelay: 5}
		Expect(config.GetInstancesRolloutDelay()).To(Equal(5 * time.Second))
	})

	It("returns zero as default delay for instances rollout when not set", func() {
		config := Data{}
		Expect(config.GetInstancesRolloutDelay()).To(BeZero())
	})
})

var _ = Describe("Namespaced deployment config validation", func() {
	It("should pass when namespaced is disabled", func() {
		conf := &Data{
			Namespaced:        false,
			OperatorNamespace: "operator-ns",
			WatchNamespace:    "watch-ns",
		}
		Expect(conf.Validate()).To(Succeed())
	})

	It("should pass when namespaced is enabled and namespaces match", func() {
		conf := &Data{
			Namespaced:        true,
			OperatorNamespace: "same-ns",
			WatchNamespace:    "same-ns",
		}
		Expect(conf.Validate()).To(Succeed())
	})

	It("should fail when namespaced is enabled and namespaces differ", func() {
		conf := &Data{
			Namespaced:        true,
			OperatorNamespace: "operator-ns",
			WatchNamespace:    "watch-ns",
		}
		err := conf.Validate()
		Expect(err).To(Equal(ErrNamespaceMismatch))
	})

	It("should fail when namespaced is enabled and operator namespace is empty", func() {
		conf := &Data{
			Namespaced:        true,
			OperatorNamespace: "",
			WatchNamespace:    "watch-ns",
		}
		err := conf.Validate()
		Expect(err).To(Equal(ErrNamespaceEmpty))
	})

	It("should fail when namespaced is enabled and watch namespace is empty", func() {
		conf := &Data{
			Namespaced:        true,
			OperatorNamespace: "operator-ns",
			WatchNamespace:    "",
		}
		err := conf.Validate()
		Expect(err).To(Equal(ErrNamespaceEmpty))
	})

	It("should fail when namespaced is enabled and both namespaces are empty", func() {
		conf := &Data{
			Namespaced:        true,
			OperatorNamespace: "",
			WatchNamespace:    "",
		}
		err := conf.Validate()
		Expect(err).To(Equal(ErrNamespaceEmpty))
	})
})
