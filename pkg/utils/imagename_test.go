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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("image name management", func() {
	It("should normalize image names", func() {
		Expect(NewReference("postgres").GetNormalizedName()).To(
			Equal("docker.io/library/postgres:latest"))
		Expect(NewReference("myimage/postgres").GetNormalizedName()).To(
			Equal("docker.io/myimage/postgres:latest"))
		Expect(NewReference("localhost:5000/postgres").GetNormalizedName()).To(
			Equal("localhost:5000/postgres:latest"))
		Expect(NewReference("registry.localhost:5000/postgres:14.4").GetNormalizedName()).To(
			Equal("registry.localhost:5000/postgres:14.4"))
		Expect(NewReference("ghcr.io/cloudnative-pg/postgresql:34").GetNormalizedName()).To(
			Equal("ghcr.io/cloudnative-pg/postgresql:34"))
	})

	It("should extract tag names", func() {
		Expect(GetImageTag("postgres")).To(Equal("latest"))
		Expect(GetImageTag("postgres:34.3")).To(Equal("34.3"))
		Expect(GetImageTag("postgres:13@sha256:cff94de382ca538861622bbe84cfe03f44f307a9846a5c5eda672cf4dc692866")).
			To(Equal("13"))
	})

	It("should not extract a tag name", func() {
		Expect(GetImageTag("postgres@sha256:cff94dd382ca538861622bbe84cfe03f44f307a9846a5c5eda672cf4dc692866")).
			To(BeEmpty())
	})
})
