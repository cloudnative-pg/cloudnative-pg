/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package configparser

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// FakeData is an example of the configuration structure
// that can be used with this configparser
type FakeData struct {
	// WatchNamespace is the namespace where the operator should watch and
	// is configurable via environment variables or via the OpenShift console
	WatchNamespace string `json:"watchNamespace" env:"WATCH_NAMESPACE"`

	// InheritedAnnotations is a list of annotations that every resource could inherit from
	// the owning Cluster
	InheritedAnnotations []string `json:"inheritedAnnotations" env:"INHERITED_ANNOTATIONS"`

	// InheritedLabels is a list of labels that every resource could inherit from
	// the owning Cluster
	InheritedLabels []string `json:"inheritedLabels" env:"INHERITED_LABELS"`

	// EnablePodDebugging enable debugging mode in new generated pods
	EnablePodDebugging bool `json:"enablePodDebugging" env:"POD_DEBUG"`
}

var defaultInheritedAnnotations = []string{
	"first",
	"second",
	"third",
}

const oneNamespace = "one-namespace"

// readConfigMap reads the configuration from the environment and the passed in data map
func (config *FakeData) readConfigMap(data map[string]string, env EnvironmentSource) {
	ReadConfigMap(config, &FakeData{InheritedAnnotations: defaultInheritedAnnotations}, data, env)
}

var _ = Describe("Data test suite", func() {
	It("correctly splits and trims lists", func() {
		list := splitAndTrim("string, with space , inside\t")
		Expect(list).To(Equal([]string{"string", "with space", "inside"}))
	})

	It("loads values from a map", func() {
		config := &FakeData{}
		config.readConfigMap(map[string]string{
			"WATCH_NAMESPACE":       oneNamespace,
			"INHERITED_ANNOTATIONS": "one, two",
			"INHERITED_LABELS":      "alpha, beta",
		}, NewFakeEnvironment(nil))
		Expect(config.WatchNamespace).To(Equal(oneNamespace))
		Expect(config.InheritedAnnotations).To(Equal([]string{"one", "two"}))
		Expect(config.InheritedLabels).To(Equal([]string{"alpha", "beta"}))
	})

	It("loads values from environment", func() {
		config := &FakeData{}
		fakeEnv := NewFakeEnvironment(map[string]string{
			"WATCH_NAMESPACE":       oneNamespace,
			"INHERITED_ANNOTATIONS": "one, two",
			"INHERITED_LABELS":      "alpha, beta",
		})
		config.readConfigMap(nil, fakeEnv)
		Expect(config.WatchNamespace).To(Equal(oneNamespace))
		Expect(config.InheritedAnnotations).To(Equal([]string{"one", "two"}))
		Expect(config.InheritedLabels).To(Equal([]string{"alpha", "beta"}))
	})

	It("handles correctly default values of slices", func() {
		config := &FakeData{}
		config.readConfigMap(nil, NewFakeEnvironment(nil))
		Expect(config.InheritedAnnotations).To(Equal(defaultInheritedAnnotations))
		Expect(config.InheritedLabels).To(BeNil())
	})
})

// FakeEnvironment is an EnvironmentSource that fetches data from an internal map
type FakeEnvironment struct {
	values map[string]string
}

// NewFakeEnvironment creates a FakeEnvironment with the specified data inside
func NewFakeEnvironment(data map[string]string) FakeEnvironment {
	f := FakeEnvironment{}
	if data == nil {
		data = make(map[string]string)
	}
	f.values = data
	return f
}

// Getenv retrieves the value of the environment variable named by the key
func (f FakeEnvironment) Getenv(key string) string {
	return f.values[key]
}
