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

var _ = DescribeTable("Parse and Write",
	func(body string, want Response, roundTrips bool) {
		Expect(Parse([]byte(body))).To(Equal(want))
		if !roundTrips {
			return
		}
		w := httptest.NewRecorder()
		Expect(Write(w, want)).To(Succeed())
		Expect(w.Header().Get("Content-Type")).To(Equal("application/json"))
		Expect(w.Body.String()).To(Equal(body))
	},
	Entry("a target primary", `{"targetPrimary":"cluster-example-2"}`, Response{TargetPrimary: "cluster-example-2"}, true),
	Entry("no target primary", `{}`, Response{}, true),
	// A pre-upgrade peer answers a plain body: it carries no signal, not an error.
	Entry("a pre-upgrade peer's plain body", "OK", Response{}, false),
	Entry("an empty body", "", Response{}, false),
)
