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

package failsafe

import (
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Parse", func() {
	It("extracts the target primary from a valid response body", func() {
		resp := Parse([]byte(`{"targetPrimary":"cluster-example-2"}`))
		Expect(resp.TargetPrimary).To(Equal("cluster-example-2"))
	})

	It("returns a zero Response when the field is omitted", func() {
		resp := Parse([]byte(`{}`))
		Expect(resp.TargetPrimary).To(BeEmpty())
	})

	It("returns a zero Response, not an error, for a pre-upgrade peer's plain body", func() {
		resp := Parse([]byte("OK"))
		Expect(resp.TargetPrimary).To(BeEmpty())
	})

	It("returns a zero Response for an empty body", func() {
		resp := Parse(nil)
		Expect(resp.TargetPrimary).To(BeEmpty())
	})
})

var _ = Describe("Write", func() {
	It("encodes the response as JSON with the expected content type", func() {
		w := httptest.NewRecorder()

		Expect(Write(w, Response{TargetPrimary: "cluster-example-1"})).To(Succeed())

		Expect(w.Header().Get("Content-Type")).To(Equal("application/json"))
		Expect(w.Body.String()).To(Equal(`{"targetPrimary":"cluster-example-1"}`))
	})

	It("omits the field when empty", func() {
		w := httptest.NewRecorder()

		Expect(Write(w, Response{})).To(Succeed())

		Expect(w.Body.String()).To(Equal(`{}`))
	})
})
