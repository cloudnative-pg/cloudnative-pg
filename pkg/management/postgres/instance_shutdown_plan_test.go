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

package postgres

import (
	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("planFastImmediateShutdown", func() {
	var instance *Instance

	BeforeEach(func() {
		instance = NewInstance()
	})

	It("gives the isolation shutdown its own tight budget and no checkpoint", func() {
		plan := planFastImmediateShutdown(instance, shutDownFastImmediateIsolated)
		Expect(plan.fastTimeout).To(BeNumerically("==", isolationShutdownFastTimeout))
		// The checkpoint is not covered by fastTimeout, so this path has to give it up
		// for the budget to mean anything.
		Expect(plan.skipCheckpoint).To(BeTrue())
	})

	It("leaves the switchover shutdown on switchoverDelay", func() {
		// reconcileOldPrimary demotes the former primary through this command on every
		// switchover, and it must keep the budget the user configured rather than the
		// isolation one.
		switchoverDelay := int32(1234)
		instance.SetCluster(&apiv1.Cluster{
			Spec: apiv1.ClusterSpec{MaxSwitchoverDelay: switchoverDelay},
		})

		plan := planFastImmediateShutdown(instance, shutDownFastImmediate)
		Expect(plan.fastTimeout).To(BeNumerically("==", switchoverDelay))
		Expect(plan.skipCheckpoint).To(BeFalse())
	})

	It("defaults the switchover shutdown to the documented switchoverDelay default", func() {
		Expect(planFastImmediateShutdown(instance, shutDownFastImmediate).fastTimeout).
			To(BeNumerically("==", apiv1.DefaultMaxSwitchoverDelay))
	})
})
