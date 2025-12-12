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

package controller

import (
	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("getNamespacesToWatch", func() {
	It("should return nil when WatchNamespace is empty", func() {
		conf := &configuration.Data{
			WatchNamespace:    "",
			OperatorNamespace: "cnpg-system",
		}
		result := getNamespacesToWatch(conf)
		Expect(result).To(BeNil())
	})

	It("should include operator namespace when watching a single namespace", func() {
		conf := &configuration.Data{
			WatchNamespace:    "app-namespace",
			OperatorNamespace: "cnpg-system",
		}
		result := getNamespacesToWatch(conf)
		Expect(result).To(HaveLen(2))
		Expect(result).To(HaveKey("app-namespace"))
		Expect(result).To(HaveKey("cnpg-system"))
	})

	It("should include operator namespace when watching multiple namespaces", func() {
		conf := &configuration.Data{
			WatchNamespace:    "ns1,ns2,ns3",
			OperatorNamespace: "cnpg-system",
		}
		result := getNamespacesToWatch(conf)
		Expect(result).To(HaveLen(4))
		Expect(result).To(HaveKey("ns1"))
		Expect(result).To(HaveKey("ns2"))
		Expect(result).To(HaveKey("ns3"))
		Expect(result).To(HaveKey("cnpg-system"))
	})

	It("should handle operator namespace already in watched namespaces", func() {
		conf := &configuration.Data{
			WatchNamespace:    "ns1,cnpg-system,ns2",
			OperatorNamespace: "cnpg-system",
		}
		result := getNamespacesToWatch(conf)
		Expect(result).To(HaveLen(3))
		Expect(result).To(HaveKey("ns1"))
		Expect(result).To(HaveKey("ns2"))
		Expect(result).To(HaveKey("cnpg-system"))
	})

	It("should handle namespaces with whitespace", func() {
		conf := &configuration.Data{
			WatchNamespace:    " ns1 , ns2 , ns3 ",
			OperatorNamespace: "cnpg-system",
		}
		result := getNamespacesToWatch(conf)
		Expect(result).To(HaveLen(4))
		Expect(result).To(HaveKey("ns1"))
		Expect(result).To(HaveKey("ns2"))
		Expect(result).To(HaveKey("ns3"))
		Expect(result).To(HaveKey("cnpg-system"))
	})

	It("should handle empty namespace entries", func() {
		conf := &configuration.Data{
			WatchNamespace:    "ns1,,ns2,",
			OperatorNamespace: "cnpg-system",
		}
		result := getNamespacesToWatch(conf)
		Expect(result).To(HaveLen(3))
		Expect(result).To(HaveKey("ns1"))
		Expect(result).To(HaveKey("ns2"))
		Expect(result).To(HaveKey("cnpg-system"))
	})
})
