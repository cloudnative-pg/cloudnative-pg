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
	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("decideFailoverQuorumAction", func() {
	oldStatus := apiv1.FailoverQuorumStatus{
		Method:        "any",
		Primary:       "cluster-1",
		StandbyNumber: 1,
		StandbyNames:  []string{"cluster-2", "cluster-3"},
	}
	newStatus := apiv1.FailoverQuorumStatus{
		Method:        "any",
		Primary:       "cluster-1",
		StandbyNumber: 2,
		StandbyNames:  []string{"cluster-2", "cluster-3", "cluster-4"},
	}
	empty := apiv1.FailoverQuorumStatus{}

	DescribeTable("decisions",
		func(
			desired, published apiv1.FailoverQuorumStatus,
			pending *pendingFailoverQuorum,
			aligned int,
			expected failoverQuorumAction,
		) {
			Expect(decideFailoverQuorumAction(desired, published, pending, aligned)).To(Equal(expected))
		},
		Entry("does nothing when the status is already published",
			newStatus, newStatus, nil, 0, failoverQuorumActionNone),
		Entry("does nothing when only nil and empty slices differ",
			apiv1.FailoverQuorumStatus{StandbyNames: []string{}}, empty, nil, 0, failoverQuorumActionNone),
		Entry("publishes an empty status immediately",
			empty, oldStatus, nil, 0, failoverQuorumActionPublish),
		Entry("publishes an empty status immediately even with a stale pending state",
			empty, oldStatus, &pendingFailoverQuorum{desired: newStatus, lsn: "0/5000060"}, 0,
			failoverQuorumActionPublish),
		Entry("captures the LSN when a change is detected",
			newStatus, oldStatus, nil, 0, failoverQuorumActionCapture),
		Entry("captures the LSN when a change is detected on an empty published status",
			newStatus, empty, nil, 0, failoverQuorumActionCapture),
		Entry("captures again when the desired status changed while waiting",
			newStatus, empty, &pendingFailoverQuorum{desired: oldStatus, lsn: "0/5000060"}, 2,
			failoverQuorumActionCapture),
		Entry("waits when no standby is aligned",
			newStatus, empty, &pendingFailoverQuorum{desired: newStatus, lsn: "0/5000060"}, 0,
			failoverQuorumActionWait),
		Entry("waits when fewer than the required standbys are aligned",
			newStatus, empty, &pendingFailoverQuorum{desired: newStatus, lsn: "0/5000060"}, 1,
			failoverQuorumActionWait),
		Entry("publishes when exactly the required standbys are aligned",
			newStatus, empty, &pendingFailoverQuorum{desired: newStatus, lsn: "0/5000060"}, 2,
			failoverQuorumActionPublish),
		Entry("publishes when more than the required standbys are aligned",
			newStatus, empty, &pendingFailoverQuorum{desired: newStatus, lsn: "0/5000060"}, 3,
			failoverQuorumActionPublish),
	)
})
