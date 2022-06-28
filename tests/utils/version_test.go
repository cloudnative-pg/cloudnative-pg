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

package utils

import (
	"github.com/cloudnative-pg/cloudnative-pg/pkg/versions"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Guess the correct version of a postgres image", func() {
	It("works with 9.6", func() {
		version, err := BumpPostgresImageMajorVersion("docker.io/library/postgres:9.6.4")
		Expect(err).ShouldNot(HaveOccurred())
		Expect(version).To(Equal("docker.io/library/postgres:10"))
	})

	It("works with latest image", func() {
		version, err := BumpPostgresImageMajorVersion(versions.DefaultImageName)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(version).To(Equal(versions.DefaultImageName))
	})
})
