/*
Copyright 2019-2022 The CloudNativePG Contributors

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

package e2e

import (
	"github.com/EnterpriseDB/cloud-native-postgresql/tests"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// A failing test to verify that our ignore-fails label is correctly ignored
// when evaluating the test reports.
var _ = Describe("ignoreFails on e2e tests", Label(tests.LabelIgnoreFails),
	func() {
		It("generates a failing tests that should be ignored", func() {
			Expect(true).To(BeFalse())
		})
	})
