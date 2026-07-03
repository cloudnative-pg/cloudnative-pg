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

package runner

import (
	"database/sql"
	"errors"

	"github.com/DATA-DOG/go-sqlmock"
	"k8s.io/utils/ptr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/controller/slots/infrastructure"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Slot synchronization", Ordered, func() {
	const (
		selectPgReplicationSlots = "^SELECT (.+) FROM pg_catalog.pg_replication_slots"
		selectPgSlotAdvance      = "SELECT pg_catalog.pg_replication_slot_advance"

		localPodName  = "cluster-2"
		localSlotName = "_cnpg_cluster_2"
		slot3         = "cluster-3"
		slot4         = "cluster-4"
		lsnSlot3      = "0/302C4D8"
		lsnSlot4      = "0/303C4D8"
	)

	var (
		config = apiv1.ReplicationSlotsConfiguration{
			HighAvailability: &apiv1.ReplicationSlotsHAConfiguration{
				Enabled:    ptr.To(true),
				SlotPrefix: "_cnpg_",
			},
		}
		columns = []string{"slot_name", "slot_type", "active", "restart_lsn", "holds_xmin"}
	)

	var (
		dbLocal, dbPrimary     *sql.DB
		mockLocal, mockPrimary sqlmock.Sqlmock
	)

	BeforeEach(func() {
		var err error
		dbLocal, mockLocal, err = sqlmock.New()
		Expect(err).NotTo(HaveOccurred())
		dbPrimary, mockPrimary, err = sqlmock.New()
		Expect(err).NotTo(HaveOccurred())
	})
	AfterEach(func() {
		Expect(mockLocal.ExpectationsWereMet()).To(Succeed(), "failed expectations in LOCAL")
		Expect(mockPrimary.ExpectationsWereMet()).To(Succeed(), "failed expectations in PRIMARY")
	})

	It("can create slots in local from those on primary", func(ctx SpecContext) {
		// the primary contains slots
		mockPrimary.ExpectQuery(selectPgReplicationSlots).
			WillReturnRows(sqlmock.NewRows(columns).
				AddRow(localSlotName, string(infrastructure.SlotTypePhysical), true, "0/301C4D8", false).
				AddRow(slot3, string(infrastructure.SlotTypePhysical), true, lsnSlot3, false).
				AddRow(slot4, string(infrastructure.SlotTypePhysical), true, lsnSlot4, false))

		// but the local contains none
		mockLocal.ExpectQuery(selectPgReplicationSlots).
			WillReturnRows(sqlmock.NewRows(columns))

		mockLocal.ExpectExec("SELECT pg_catalog.pg_create_physical_replication_slot").
			WithArgs(slot3, true).
			WillReturnResult(sqlmock.NewResult(1, 1))

		mockLocal.ExpectExec(selectPgSlotAdvance).
			WithArgs(slot3, lsnSlot3).
			WillReturnResult(sqlmock.NewResult(1, 1))

		mockLocal.ExpectExec("SELECT pg_catalog.pg_create_physical_replication_slot").
			WithArgs(slot4, true).
			WillReturnResult(sqlmock.NewResult(1, 1))

		mockLocal.ExpectExec(selectPgSlotAdvance).
			WithArgs(slot4, lsnSlot4).
			WillReturnResult(sqlmock.NewResult(1, 1))

		err := synchronizeReplicationSlots(ctx, dbPrimary, dbLocal, localPodName, &config)
		Expect(err).ShouldNot(HaveOccurred())
	})

	It("can update slots in local when ReplayLSN in primary advanced", func(ctx SpecContext) {
		newLSN := "0/308C4D8"

		// Simulate we advance slot3 in primary
		mockPrimary.ExpectQuery(selectPgReplicationSlots).
			WillReturnRows(sqlmock.NewRows(columns).
				AddRow(localSlotName, string(infrastructure.SlotTypePhysical), true, "0/301C4D8", false).
				AddRow(slot3, string(infrastructure.SlotTypePhysical), true, newLSN, false).
				AddRow(slot4, string(infrastructure.SlotTypePhysical), true, lsnSlot4, false))
		// But local has the old values
		mockLocal.ExpectQuery(selectPgReplicationSlots).
			WillReturnRows(sqlmock.NewRows(columns).
				AddRow(slot3, string(infrastructure.SlotTypePhysical), true, lsnSlot3, false).
				AddRow(slot4, string(infrastructure.SlotTypePhysical), true, lsnSlot4, false))

		mockLocal.ExpectExec(selectPgSlotAdvance).
			WithArgs(slot3, newLSN).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mockLocal.ExpectExec(selectPgSlotAdvance).
			WithArgs(slot4, lsnSlot4).
			WillReturnResult(sqlmock.NewResult(1, 1))

		err := synchronizeReplicationSlots(ctx, dbPrimary, dbLocal, localPodName, &config)
		Expect(err).ShouldNot(HaveOccurred())
	})

	It("can drop inactive slots in local when they are no longer in primary", func(ctx SpecContext) {
		// Simulate primary has no longer slot4
		mockPrimary.ExpectQuery(selectPgReplicationSlots).
			WillReturnRows(sqlmock.NewRows(columns).
				AddRow(localSlotName, string(infrastructure.SlotTypePhysical), true, "0/301C4D8", false))
		// But local still has it
		mockLocal.ExpectQuery(selectPgReplicationSlots).
			WillReturnRows(sqlmock.NewRows(columns).
				AddRow(slot4, string(infrastructure.SlotTypePhysical), false, lsnSlot4, false))

		mockLocal.ExpectExec("SELECT pg_catalog.pg_drop_replication_slot").WithArgs(slot4).
			WillReturnResult(sqlmock.NewResult(1, 1))

		err := synchronizeReplicationSlots(ctx, dbPrimary, dbLocal, localPodName, &config)
		Expect(err).ShouldNot(HaveOccurred())
	})

	It("can drop slots in local that hold xmin", func(ctx SpecContext) {
		slotWithXmin := "_cnpg_xmin"
		mockPrimary.ExpectQuery(selectPgReplicationSlots).
			WillReturnRows(sqlmock.NewRows(columns).
				AddRow(localSlotName, string(infrastructure.SlotTypePhysical), true, "0/301C4D8", false).
				AddRow(slotWithXmin, string(infrastructure.SlotTypePhysical), true, "0/301C4D8", true))
		mockLocal.ExpectQuery(selectPgReplicationSlots).
			WillReturnRows(sqlmock.NewRows(columns).
				AddRow(localSlotName, string(infrastructure.SlotTypePhysical), true, "0/301C4D8", false).
				AddRow(slotWithXmin, string(infrastructure.SlotTypePhysical), false, "0/301C4D8", true)) // inactive but with Xmin

		mockLocal.ExpectExec(selectPgSlotAdvance).WithArgs(slotWithXmin, "0/301C4D8").
			WillReturnResult(sqlmock.NewResult(1, 1))
		mockLocal.ExpectExec("SELECT pg_catalog.pg_drop_replication_slot").WithArgs(slotWithXmin).
			WillReturnResult(sqlmock.NewResult(1, 1))

		err := synchronizeReplicationSlots(ctx, dbPrimary, dbLocal, localPodName, &config)
		Expect(err).ShouldNot(HaveOccurred())
	})

	It("continues after a slot fails to be created, and still runs the cleanup", func(ctx SpecContext) {
		staleSlot := "cluster-5"

		mockPrimary.ExpectQuery(selectPgReplicationSlots).
			WillReturnRows(sqlmock.NewRows(columns).
				AddRow(localSlotName, string(infrastructure.SlotTypePhysical), true, "0/301C4D8", false).
				AddRow(slot3, string(infrastructure.SlotTypePhysical), true, lsnSlot3, false).
				AddRow(slot4, string(infrastructure.SlotTypePhysical), true, lsnSlot4, false))

		// the local instance has a stale slot to be dropped and none of the primary ones
		mockLocal.ExpectQuery(selectPgReplicationSlots).
			WillReturnRows(sqlmock.NewRows(columns).
				AddRow(staleSlot, string(infrastructure.SlotTypePhysical), false, lsnSlot4, false))

		mockLocal.ExpectExec("SELECT pg_catalog.pg_create_physical_replication_slot").
			WithArgs(slot3, true).
			WillReturnError(errors.New("mock create failure"))

		// no advance is expected for slot3: creating it failed. slot4 is
		// still created and advanced, and the stale slot is still dropped.
		mockLocal.ExpectExec("SELECT pg_catalog.pg_create_physical_replication_slot").
			WithArgs(slot4, true).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mockLocal.ExpectExec(selectPgSlotAdvance).
			WithArgs(slot4, lsnSlot4).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mockLocal.ExpectExec("SELECT pg_catalog.pg_drop_replication_slot").WithArgs(staleSlot).
			WillReturnError(errors.New("mock drop failure"))

		err := synchronizeReplicationSlots(ctx, dbPrimary, dbLocal, localPodName, &config)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(`creating replication slot "cluster-3": mock create failure`))
		Expect(err.Error()).To(ContainSubstring(`dropping replication slot "cluster-5": mock drop failure`))
	})

	It("continues after a slot fails to be advanced, and still runs the cleanup", func(ctx SpecContext) {
		staleSlot := "cluster-5"

		mockPrimary.ExpectQuery(selectPgReplicationSlots).
			WillReturnRows(sqlmock.NewRows(columns).
				AddRow(localSlotName, string(infrastructure.SlotTypePhysical), true, "0/301C4D8", false).
				AddRow(slot3, string(infrastructure.SlotTypePhysical), true, lsnSlot3, false).
				AddRow(slot4, string(infrastructure.SlotTypePhysical), true, lsnSlot4, false))

		mockLocal.ExpectQuery(selectPgReplicationSlots).
			WillReturnRows(sqlmock.NewRows(columns).
				AddRow(slot3, string(infrastructure.SlotTypePhysical), true, lsnSlot3, false).
				AddRow(slot4, string(infrastructure.SlotTypePhysical), true, lsnSlot4, false).
				AddRow(staleSlot, string(infrastructure.SlotTypePhysical), false, lsnSlot4, false))

		mockLocal.ExpectExec(selectPgSlotAdvance).
			WithArgs(slot3, lsnSlot3).
			WillReturnError(errors.New("mock advance failure"))

		mockLocal.ExpectExec(selectPgSlotAdvance).
			WithArgs(slot4, lsnSlot4).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mockLocal.ExpectExec("SELECT pg_catalog.pg_drop_replication_slot").WithArgs(staleSlot).
			WillReturnResult(sqlmock.NewResult(1, 1))

		err := synchronizeReplicationSlots(ctx, dbPrimary, dbLocal, localPodName, &config)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(`updating replication slot "cluster-3": mock advance failure`))
		Expect(err.Error()).NotTo(ContainSubstring("cluster-4"))
		Expect(err.Error()).NotTo(ContainSubstring("cluster-5"))
	})

	It("continues dropping the remaining stale slots after a drop failure", func(ctx SpecContext) {
		staleSlot5 := "cluster-5"
		staleSlot6 := "cluster-6"

		mockPrimary.ExpectQuery(selectPgReplicationSlots).
			WillReturnRows(sqlmock.NewRows(columns).
				AddRow(localSlotName, string(infrastructure.SlotTypePhysical), true, "0/301C4D8", false))

		mockLocal.ExpectQuery(selectPgReplicationSlots).
			WillReturnRows(sqlmock.NewRows(columns).
				AddRow(staleSlot5, string(infrastructure.SlotTypePhysical), false, lsnSlot4, false).
				AddRow(staleSlot6, string(infrastructure.SlotTypePhysical), false, lsnSlot4, false))

		mockLocal.ExpectExec("SELECT pg_catalog.pg_drop_replication_slot").WithArgs(staleSlot5).
			WillReturnError(errors.New("mock drop failure"))
		mockLocal.ExpectExec("SELECT pg_catalog.pg_drop_replication_slot").WithArgs(staleSlot6).
			WillReturnResult(sqlmock.NewResult(1, 1))

		err := synchronizeReplicationSlots(ctx, dbPrimary, dbLocal, localPodName, &config)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(`dropping replication slot "cluster-5": mock drop failure`))
		Expect(err.Error()).NotTo(ContainSubstring("cluster-6"))
	})

	It("drops a stale user slot only once when synchronizeReplicas is disabled", func(ctx SpecContext) {
		configNoUserSlots := apiv1.ReplicationSlotsConfiguration{
			HighAvailability: &apiv1.ReplicationSlotsHAConfiguration{
				Enabled:    ptr.To(true),
				SlotPrefix: "_cnpg_",
			},
			SynchronizeReplicas: &apiv1.SynchronizeReplicasConfiguration{
				Enabled: ptr.To(false),
			},
		}
		userSlot := "userslot"

		mockPrimary.ExpectQuery(selectPgReplicationSlots).
			WillReturnRows(sqlmock.NewRows(columns).
				AddRow(localSlotName, string(infrastructure.SlotTypePhysical), true, "0/301C4D8", false))

		// the local slot is both missing from the primary and a user slot
		// while synchronizeReplicas is disabled: it must be dropped exactly once
		mockLocal.ExpectQuery(selectPgReplicationSlots).
			WillReturnRows(sqlmock.NewRows(columns).
				AddRow(userSlot, string(infrastructure.SlotTypePhysical), false, lsnSlot4, false))

		mockLocal.ExpectExec("SELECT pg_catalog.pg_drop_replication_slot").WithArgs(userSlot).
			WillReturnResult(sqlmock.NewResult(1, 1))

		err := synchronizeReplicationSlots(ctx, dbPrimary, dbLocal, localPodName, &configNoUserSlots)
		Expect(err).ShouldNot(HaveOccurred())
	})
})

var _ = Describe("Replicator reconcile", func() {
	It("returns the recovered error when the reconciliation panics", func(ctx SpecContext) {
		// a Replicator with a nil instance panics as soon as it dereferences it
		sr := NewReplicator(nil)

		err := sr.reconcile(ctx, &apiv1.ReplicationSlotsConfiguration{})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("recovered from a panic"))
	})
})
