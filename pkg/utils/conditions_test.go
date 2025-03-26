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

package utils

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("condition reasons", func() {
	It("must detect as allowed the commonly used condition reasons", func() {
		Expect(IsConditionReasonValid("MinimumReplicasAvailable")).To(BeTrue())
		Expect(IsConditionReasonValid("NewReplicaSetAvailable")).To(BeTrue())
	})

	It("must reject the condition reasons we were using before directly using the Kubernetes API", func() {
		Expect(IsConditionReasonValid("Continuous Archiving is Failing")).To(BeFalse())
		Expect(IsConditionReasonValid("Continuous Archiving is Working")).To(BeFalse())
		Expect(IsConditionReasonValid("Last backup succeeded")).To(BeFalse())
	})
})
