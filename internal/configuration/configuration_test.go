/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package configuration

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Data test suite", func() {
	It("correctly splits and trims lists", func() {
		list := splitAndTrim("string, with space , inside\t")
		Expect(list).To(Equal([]string{"string", "with space", "inside"}))
	})

	It("loads values from a map", func() {
		config := &Data{}
		config.ReadConfigMap(map[string]string{
			"WATCH_NAMESPACE":       "one-namespace",
			"INHERITED_ANNOTATIONS": "one, two",
			"INHERITED_LABELS":      "alpha, beta",
		})
		Expect(config.WatchNamespace).To(Equal("one-namespace"))
		Expect(config.InheritedAnnotations).To(Equal([]string{"one", "two"}))
		Expect(config.InheritedLabels).To(Equal([]string{"alpha", "beta"}))
	})

	It("loads values from environment", func() {
		config := &Data{}
		fakeEnv := NewFakeEnvironment(map[string]string{
			"WATCH_NAMESPACE":       "one-namespace",
			"INHERITED_ANNOTATIONS": "one, two",
			"INHERITED_LABELS":      "alpha, beta",
		})
		config.readConfigMap(nil, fakeEnv)
		Expect(config.WatchNamespace).To(Equal("one-namespace"))
		Expect(config.InheritedAnnotations).To(Equal([]string{"one", "two"}))
		Expect(config.InheritedLabels).To(Equal([]string{"alpha", "beta"}))
	})

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
})

// FakeEnvironment is an EnvironmentSource that fetch data from an internal map.
type FakeEnvironment struct {
	values map[string]string
}

// NewFakeEnvironment creates a FakeEnvironment with the specified data inside.
func NewFakeEnvironment(data map[string]string) FakeEnvironment {
	f := FakeEnvironment{}
	if data == nil {
		data = make(map[string]string)
	}
	f.values = data
	return f
}

// Getenv retrieves the value of the environment variable named by the key.
func (f FakeEnvironment) Getenv(key string) string {
	return f.values[key]
}
