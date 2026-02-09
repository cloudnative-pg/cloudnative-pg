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

package autoresize

import (
	"k8s.io/utils/ptr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("EvaluateWALSafety", func() {
	var (
		walSafety *apiv1.WALSafetyPolicy
		walHealth *postgres.WALHealthStatus
	)

	BeforeEach(func() {
		walSafety = &apiv1.WALSafetyPolicy{
			AcknowledgeWALRisk:    true,
			RequireArchiveHealthy: ptr.To(true),
			MaxPendingWALFiles:    ptr.To(100),
		}
		walHealth = &postgres.WALHealthStatus{
			ArchiveHealthy:  true,
			PendingWALFiles: 5,
		}
	})

	Context("when PVC role does not need WAL safety", func() {
		It("should allow resize for tablespace volumes", func() {
			result := EvaluateWALSafety(
				string(utils.PVCRolePgTablespace),
				false,
				walSafety,
				walHealth,
			)
			Expect(result.Allowed).To(BeTrue())
		})

		It("should allow resize for data volumes in multi-volume clusters", func() {
			result := EvaluateWALSafety(
				string(utils.PVCRolePgData),
				false, // has separate WAL storage
				walSafety,
				walHealth,
			)
			Expect(result.Allowed).To(BeTrue())
		})
	})

	Context("single-volume cluster checks", func() {
		It("should block resize when acknowledgeWALRisk is not set", func() {
			walSafety.AcknowledgeWALRisk = false

			result := EvaluateWALSafety(
				string(utils.PVCRolePgData),
				true, // single volume
				walSafety,
				walHealth,
			)
			Expect(result.Allowed).To(BeFalse())
			Expect(result.BlockReason).To(Equal(WALSafetyBlockSingleVolumeNoAck))
		})

		It("should block resize when walSafety is nil for single-volume clusters", func() {
			result := EvaluateWALSafety(
				string(utils.PVCRolePgData),
				true, // single volume
				nil,  // no safety policy
				walHealth,
			)
			Expect(result.Allowed).To(BeFalse())
			Expect(result.BlockReason).To(Equal(WALSafetyBlockSingleVolumeNoAck))
		})

		It("should allow resize when acknowledgeWALRisk is set", func() {
			walSafety.AcknowledgeWALRisk = true

			result := EvaluateWALSafety(
				string(utils.PVCRolePgData),
				true, // single volume
				walSafety,
				walHealth,
			)
			Expect(result.Allowed).To(BeTrue())
		})
	})

	Context("WAL volume checks", func() {
		It("should always apply WAL safety to WAL volumes", func() {
			walHealth.ArchiveHealthy = false

			result := EvaluateWALSafety(
				string(utils.PVCRolePgWal),
				false,
				walSafety,
				walHealth,
			)
			Expect(result.Allowed).To(BeFalse())
			Expect(result.BlockReason).To(Equal(WALSafetyBlockArchiveUnhealthy))
		})
	})

	Context("archive health check", func() {
		It("should block when archive is unhealthy and requireArchiveHealthy is true", func() {
			walHealth.ArchiveHealthy = false

			result := EvaluateWALSafety(
				string(utils.PVCRolePgWal),
				false,
				walSafety,
				walHealth,
			)
			Expect(result.Allowed).To(BeFalse())
			Expect(result.BlockReason).To(Equal(WALSafetyBlockArchiveUnhealthy))
		})

		It("should allow when archive is unhealthy but requireArchiveHealthy is false", func() {
			walHealth.ArchiveHealthy = false
			walSafety.RequireArchiveHealthy = ptr.To(false)

			result := EvaluateWALSafety(
				string(utils.PVCRolePgWal),
				false,
				walSafety,
				walHealth,
			)
			Expect(result.Allowed).To(BeTrue())
		})

		It("should allow when archive is healthy", func() {
			walHealth.ArchiveHealthy = true

			result := EvaluateWALSafety(
				string(utils.PVCRolePgWal),
				false,
				walSafety,
				walHealth,
			)
			Expect(result.Allowed).To(BeTrue())
		})
	})

	Context("pending WAL files check", func() {
		It("should block when pending WAL files exceed threshold", func() {
			walSafety.MaxPendingWALFiles = ptr.To(50)
			walHealth.PendingWALFiles = 75

			result := EvaluateWALSafety(
				string(utils.PVCRolePgWal),
				false,
				walSafety,
				walHealth,
			)
			Expect(result.Allowed).To(BeFalse())
			Expect(result.BlockReason).To(Equal(WALSafetyBlockPendingWAL))
		})

		It("should allow when pending WAL files are below threshold", func() {
			walSafety.MaxPendingWALFiles = ptr.To(50)
			walHealth.PendingWALFiles = 25

			result := EvaluateWALSafety(
				string(utils.PVCRolePgWal),
				false,
				walSafety,
				walHealth,
			)
			Expect(result.Allowed).To(BeTrue())
		})
	})

	Context("slot retention check", func() {
		It("should block when slot retention exceeds threshold", func() {
			walSafety.MaxSlotRetentionBytes = ptr.To(int64(1073741824)) // 1 GiB
			walHealth.InactiveSlots = []postgres.WALInactiveSlotInfo{
				{SlotName: "slot1", RetentionBytes: 2147483648}, // 2 GiB
			}

			result := EvaluateWALSafety(
				string(utils.PVCRolePgWal),
				false,
				walSafety,
				walHealth,
			)
			Expect(result.Allowed).To(BeFalse())
			Expect(result.BlockReason).To(Equal(WALSafetyBlockSlotRetention))
			Expect(result.BlockMessage).To(ContainSubstring("slot1"))
		})

		It("should allow when slot retention is below threshold", func() {
			walSafety.MaxSlotRetentionBytes = ptr.To(int64(1073741824)) // 1 GiB
			walHealth.InactiveSlots = []postgres.WALInactiveSlotInfo{
				{SlotName: "slot1", RetentionBytes: 536870912}, // 512 MiB
			}

			result := EvaluateWALSafety(
				string(utils.PVCRolePgWal),
				false,
				walSafety,
				walHealth,
			)
			Expect(result.Allowed).To(BeTrue())
		})

		It("should allow when maxSlotRetentionBytes is not set", func() {
			walSafety.MaxSlotRetentionBytes = nil
			walHealth.InactiveSlots = []postgres.WALInactiveSlotInfo{
				{SlotName: "slot1", RetentionBytes: 9999999999},
			}

			result := EvaluateWALSafety(
				string(utils.PVCRolePgWal),
				false,
				walSafety,
				walHealth,
			)
			Expect(result.Allowed).To(BeTrue())
		})
	})

	Context("nil walHealth", func() {
		It("should allow resize when walHealth is nil (fail-open) but record reason", func() {
			// Fail-open: when WAL health is unavailable, the resize is allowed with a warning.
			// The primary threat is disk full → database crash. Blocking resize when we can't
			// verify WAL health is more dangerous than allowing it.
			result := EvaluateWALSafety(
				string(utils.PVCRolePgWal),
				false,
				walSafety,
				nil, // no health data
			)
			Expect(result.Allowed).To(BeTrue())
			Expect(result.BlockReason).To(Equal(WALSafetyBlockHealthUnavailable))
			Expect(result.BlockMessage).To(ContainSubstring("permitted without WAL health verification"))
		})
	})

	Context("nil walSafety with WAL volume", func() {
		It("should use default WAL safety policy", func() {
			// defaults: requireArchiveHealthy=true, maxPendingWALFiles=100
			walHealth.ArchiveHealthy = false

			result := EvaluateWALSafety(
				string(utils.PVCRolePgWal),
				false,
				nil, // will use defaults
				walHealth,
			)
			Expect(result.Allowed).To(BeFalse())
			Expect(result.BlockReason).To(Equal(WALSafetyBlockArchiveUnhealthy))
		})
	})
})

var _ = Describe("needsWALSafetyCheck", func() {
	It("should return true for WAL volumes", func() {
		Expect(needsWALSafetyCheck(string(utils.PVCRolePgWal), false)).To(BeTrue())
		Expect(needsWALSafetyCheck(string(utils.PVCRolePgWal), true)).To(BeTrue())
	})

	It("should return true for data volumes in single-volume clusters", func() {
		Expect(needsWALSafetyCheck(string(utils.PVCRolePgData), true)).To(BeTrue())
	})

	It("should return false for data volumes in multi-volume clusters", func() {
		Expect(needsWALSafetyCheck(string(utils.PVCRolePgData), false)).To(BeFalse())
	})

	It("should return false for tablespace volumes", func() {
		Expect(needsWALSafetyCheck(string(utils.PVCRolePgTablespace), false)).To(BeFalse())
		Expect(needsWALSafetyCheck(string(utils.PVCRolePgTablespace), true)).To(BeFalse())
	})
})
