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

	// This is the lifetime of the generated certificates
	CertificateDuration int `json:"certificateDuration" env:"CERTIFICATE_DURATION"`

	//  Threshold to consider a certificate as expiring
	ExpiringCheckThreshold int `json:"expiringCheckThreshold" env:"EXPIRING_CHECK_THRESHOLD"`
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
			"WATCH_NAMESPACE":          oneNamespace,
			"INHERITED_ANNOTATIONS":    "one, two",
			"INHERITED_LABELS":         "alpha, beta",
			"EXPIRING_CHECK_THRESHOLD": "2",
		})
		config.readConfigMap(nil, fakeEnv)
		Expect(config.WatchNamespace).To(Equal(oneNamespace))
		Expect(config.InheritedAnnotations).To(Equal([]string{"one", "two"}))
		Expect(config.InheritedLabels).To(Equal([]string{"alpha", "beta"}))
		Expect(config.ExpiringCheckThreshold).To(Equal(2))
	})

	It("reset to default value if format is not correct", func() {
		config := &FakeData{
			CertificateDuration:    90,
			ExpiringCheckThreshold: 7,
		}
		fakeEnv := NewFakeEnvironment(map[string]string{
			"EXPIRING_CHECK_THRESHOLD": "3600min",
			"CERTIFICATE_DURATION":     "unknown",
		})
		defaultData := &FakeData{
			CertificateDuration:    90,
			ExpiringCheckThreshold: 7,
		}
		ReadConfigMap(config, defaultData, nil, fakeEnv)
		Expect(config.ExpiringCheckThreshold).To(Equal(7))
		Expect(config.CertificateDuration).To(Equal(90))
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
