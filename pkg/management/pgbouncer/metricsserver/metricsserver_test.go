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

package metricsserver

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("MetricsServer", func() {
	Describe("Setup", func() {
		BeforeEach(func() {
			server = nil
			registry = nil
			exporter = nil
		})

		It("should register exporters and collectors successfully", func() {
			err := Setup()
			Expect(err).NotTo(HaveOccurred())

			mfs, err := registry.Gather()
			Expect(err).NotTo(HaveOccurred())
			Expect(mfs).NotTo(BeEmpty())

			Expect(exporter.Metrics.CollectionsTotal).NotTo(BeNil())
			Expect(exporter.Metrics.PgCollectionErrors).NotTo(BeNil())
			Expect(exporter.Metrics.Error).NotTo(BeNil())
			Expect(exporter.Metrics.CollectionDuration).NotTo(BeNil())
			Expect(exporter.Metrics.PgbouncerUp).NotTo(BeNil())
			Expect(exporter.Metrics.ShowLists).NotTo(BeNil())
			Expect(exporter.Metrics.ShowPools).NotTo(BeNil())
			Expect(exporter.Metrics.ShowStats).NotTo(BeNil())
		})
	})
})
