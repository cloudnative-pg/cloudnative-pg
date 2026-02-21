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

package reconciler

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"k8s.io/utils/ptr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/controller/slots/infrastructure"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const slotPrefix = "_cnpg_"

var repSlotColumns = []string{"slot_name", "slot_type", "active", "restart_lsn", "holds_xmin"}

func makeClusterWithInstanceNames(instanceNames []string, primary string) apiv1.Cluster {
	return apiv1.Cluster{
		Spec: apiv1.ClusterSpec{
			ReplicationSlots: &apiv1.ReplicationSlotsConfiguration{
				HighAvailability: &apiv1.ReplicationSlotsHAConfiguration{
					Enabled:    ptr.To(true),
					SlotPrefix: slotPrefix,
				},
			},
		},
		Status: apiv1.ClusterStatus{
			InstanceNames:  instanceNames,
			CurrentPrimary: primary,
			TargetPrimary:  primary,
		},
	}
}

func newRepSlot(name string, active bool, restartLSN string) []driver.Value {
	return []driver.Value{
		slotPrefix + name, string(infrastructure.SlotTypePhysical), active, restartLSN, false,
	}
}

var _ = Describe("HA Replication Slots reconciliation in Primary", func() {
	var (
		db   *sql.DB
		mock sqlmock.Sqlmock
	)
	BeforeEach(func() {
		var err error
		db, mock, err = sqlmock.New()
		Expect(err).NotTo(HaveOccurred())
	})
	AfterEach(func() {
		Expect(mock.ExpectationsWereMet()).To(Succeed())
	})
	It("can create a new replication slot for a new cluster instance", func(ctx SpecContext) {
		rows := sqlmock.NewRows(repSlotColumns).
			AddRow(newRepSlot("instance1", true, "lsn1")...).
			AddRow(newRepSlot("instance2", true, "lsn2")...)

		mock.ExpectQuery("^SELECT (.+) FROM pg_catalog.pg_replication_slots").
			WillReturnRows(rows)

		mock.ExpectExec("SELECT pg_catalog.pg_create_physical_replication_slot").
			WithArgs(slotPrefix+"instance3", false).
			WillReturnResult(sqlmock.NewResult(1, 1))

		cluster := makeClusterWithInstanceNames([]string{"instance1", "instance2", "instance3"}, "instance1")

		_, err := ReconcileReplicationSlots(ctx, "instance1", db, &cluster)
		Expect(err).ShouldNot(HaveOccurred())
	})

	It("can delete an inactive HA replication slot that is not in the cluster", func(ctx SpecContext) {
		rows := sqlmock.NewRows(repSlotColumns).
			AddRow(newRepSlot("instance1", true, "lsn1")...).
			AddRow(newRepSlot("instance2", true, "lsn2")...).
			AddRow(newRepSlot("instance3", false, "lsn2")...)

		mock.ExpectQuery("^SELECT (.+) FROM pg_catalog.pg_replication_slots").
			WillReturnRows(rows)

		mock.ExpectExec("SELECT pg_catalog.pg_drop_replication_slot").WithArgs(slotPrefix + "instance3").
			WillReturnResult(sqlmock.NewResult(1, 1))

		cluster := makeClusterWithInstanceNames([]string{"instance1", "instance2"}, "instance1")

		_, err := ReconcileReplicationSlots(ctx, "instance1", db, &cluster)
		Expect(err).ShouldNot(HaveOccurred())
	})

	It("will not delete an active HA replication slot that is not in the cluster", func(ctx SpecContext) {
		rows := sqlmock.NewRows(repSlotColumns).
			AddRow(newRepSlot("instance1", true, "lsn1")...).
			AddRow(newRepSlot("instance2", true, "lsn2")...).
			AddRow(newRepSlot("instance3", true, "lsn2")...)

		mock.ExpectQuery("^SELECT (.+) FROM pg_catalog.pg_replication_slots").
			WillReturnRows(rows)

		cluster := makeClusterWithInstanceNames([]string{"instance1", "instance2"}, "instance1")

		_, err := ReconcileReplicationSlots(ctx, "instance1", db, &cluster)
		Expect(err).ShouldNot(HaveOccurred())
	})
})

var _ = Describe("dropReplicationSlots", func() {
	const selectPgRepSlot = "^SELECT (.+) FROM pg_catalog.pg_replication_slots"

	var (
		db   *sql.DB
		mock sqlmock.Sqlmock
	)
	BeforeEach(func() {
		var err error
		db, mock, err = sqlmock.New()
		Expect(err).NotTo(HaveOccurred())
	})
	AfterEach(func() {
		Expect(mock.ExpectationsWereMet()).To(Succeed())
	})

	It("returns error when listing slots fails", func(ctx SpecContext) {
		cluster := makeClusterWithInstanceNames([]string{}, "")

		mock.ExpectQuery(selectPgRepSlot).WillReturnError(errors.New("triggered list error"))

		_, err := dropReplicationSlots(ctx, db, &cluster, true)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("triggered list error"))
	})

	It("skips deletion of active HA slots and reschedules", func(ctx SpecContext) {
		rows := sqlmock.NewRows(repSlotColumns).
			AddRow(newRepSlot("instance1", true, "lsn1")...)
		mock.ExpectQuery(selectPgRepSlot).WillReturnRows(rows)

		cluster := makeClusterWithInstanceNames([]string{}, "")

		res, err := dropReplicationSlots(ctx, db, &cluster, true)
		Expect(err).NotTo(HaveOccurred())
		Expect(res.RequeueAfter).To(Equal(time.Second))
	})

	It("skips the deletion of user defined replication slots on the primary", func(ctx SpecContext) {
		rows := sqlmock.NewRows(repSlotColumns).
			AddRow("custom-slot", string(infrastructure.SlotTypePhysical), true, "lsn1", false)
		mock.ExpectQuery("^SELECT (.+) FROM pg_catalog.pg_replication_slots").
			WillReturnRows(rows)

		cluster := makeClusterWithInstanceNames([]string{}, "")

		res, err := dropReplicationSlots(ctx, db, &cluster, true)
		Expect(err).NotTo(HaveOccurred())
		Expect(res.RequeueAfter).To(Equal(time.Duration(0)))
		Expect(res.IsZero()).To(BeTrue())
	})

	It("returns error when deleting a slot fails", func(ctx SpecContext) {
		rows := sqlmock.NewRows(repSlotColumns).
			AddRow(newRepSlot("instance1", false, "lsn1")...)
		mock.ExpectQuery(selectPgRepSlot).WillReturnRows(rows)

		mock.ExpectExec("SELECT pg_catalog.pg_drop_replication_slot").WithArgs(slotPrefix + "instance1").
			WillReturnError(errors.New("delete error"))

		cluster := makeClusterWithInstanceNames([]string{}, "")

		_, err := dropReplicationSlots(ctx, db, &cluster, true)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("delete error"))
	})

	It("deletes inactive slots and does not reschedule", func(ctx SpecContext) {
		rows := sqlmock.NewRows(repSlotColumns).
			AddRow(newRepSlot("instance1", false, "lsn1")...)
		mock.ExpectQuery(selectPgRepSlot).WillReturnRows(rows)

		mock.ExpectExec("SELECT pg_catalog.pg_drop_replication_slot").WithArgs(slotPrefix + "instance1").
			WillReturnResult(sqlmock.NewResult(1, 1))

		cluster := makeClusterWithInstanceNames([]string{}, "")

		res, err := dropReplicationSlots(ctx, db, &cluster, true)
		Expect(err).NotTo(HaveOccurred())
		Expect(res.RequeueAfter).To(Equal(time.Duration(0)))
	})
})

var _ = Describe("ReconcileReplicationSlots logical slot cleanup integration", func() {
	const selectLogicalSlots = "SELECT .+ FROM pg_catalog.pg_replication_slots WHERE slot_type = 'logical'"

	var (
		db   *sql.DB
		mock sqlmock.Sqlmock
	)

	BeforeEach(func() {
		var err error
		db, mock, err = sqlmock.New()
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		Expect(mock.ExpectationsWereMet()).To(Succeed())
	})

	//nolint:unparam // primary is parameterized for clarity and future test cases
	makeClusterWithLogicalDecoding := func(
		instanceNames []string, primary, imageName string, syncEnabled bool,
	) apiv1.Cluster {
		return apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				ImageName: imageName,
				ReplicationSlots: &apiv1.ReplicationSlotsConfiguration{
					HighAvailability: &apiv1.ReplicationSlotsHAConfiguration{
						Enabled:                    ptr.To(true),
						SlotPrefix:                 slotPrefix,
						SynchronizeLogicalDecoding: syncEnabled,
					},
				},
			},
			Status: apiv1.ClusterStatus{
				InstanceNames:  instanceNames,
				CurrentPrimary: primary,
				TargetPrimary:  primary,
			},
		}
	}

	It("cleans up orphaned failover logical slots on replica with PG17+",
		func(ctx SpecContext) {
			cluster := makeClusterWithLogicalDecoding(
				[]string{"instance1", "instance2"},
				"instance1",
				"ghcr.io/cloudnative-pg/postgresql:17.0",
				true,
			)

			// Expect logical slot cleanup query
			// Only the orphan_slot (synced=false, failover=true) should be dropped
			// external_sub_slot (synced=false, failover=false) should NOT be dropped
			rows := sqlmock.NewRows([]string{"slot_name", "plugin", "active", "restart_lsn", "synced", "failover"}).
				AddRow("orphan_slot", "pgoutput", false, "0/5678", false, true).       // orphaned failover slot - DROP
				AddRow("external_sub_slot", "pgoutput", false, "0/9ABC", false, false) // external subscription - KEEP

			mock.ExpectQuery(selectLogicalSlots).WillReturnRows(rows)

			// Expect slot deletion - only orphan_slot should be dropped
			mock.ExpectExec("SELECT pg_catalog.pg_drop_replication_slot").
				WithArgs("orphan_slot").
				WillReturnResult(sqlmock.NewResult(1, 1))

			// This is a replica (instance2 is not the primary)
			_, err := ReconcileReplicationSlots(ctx, "instance2", db, &cluster)
			Expect(err).NotTo(HaveOccurred())
		})

	It("does NOT cleanup logical slots on primary", func(ctx SpecContext) {
		cluster := makeClusterWithLogicalDecoding(
			[]string{"instance1", "instance2"},
			"instance1",
			"ghcr.io/cloudnative-pg/postgresql:17.0",
			true,
		)

		// Expect HA slot reconciliation query (primary behavior)
		rows := sqlmock.NewRows(repSlotColumns)
		mock.ExpectQuery("^SELECT (.+) FROM pg_catalog.pg_replication_slots").WillReturnRows(rows)

		// Expect slot creation for replica
		mock.ExpectExec("SELECT pg_catalog.pg_create_physical_replication_slot").
			WithArgs(slotPrefix+"instance2", false).
			WillReturnResult(sqlmock.NewResult(1, 1))

		// This is the primary - no logical slot cleanup should occur
		_, err := ReconcileReplicationSlots(ctx, "instance1", db, &cluster)
		Expect(err).NotTo(HaveOccurred())
	})

	It("does NOT cleanup logical slots when synchronizeLogicalDecoding is disabled", func(ctx SpecContext) {
		cluster := makeClusterWithLogicalDecoding(
			[]string{"instance1", "instance2"},
			"instance1",
			"ghcr.io/cloudnative-pg/postgresql:17.0",
			false, // disabled
		)

		// No logical slot cleanup query should be executed
		// This is a replica but syncLogicalDecoding is disabled
		_, err := ReconcileReplicationSlots(ctx, "instance2", db, &cluster)
		Expect(err).NotTo(HaveOccurred())
	})

	It("does NOT cleanup logical slots on PG16 (synced column doesn't exist)", func(ctx SpecContext) {
		cluster := makeClusterWithLogicalDecoding(
			[]string{"instance1", "instance2"},
			"instance1",
			"ghcr.io/cloudnative-pg/postgresql:16.0",
			true,
		)

		// No logical slot cleanup query should be executed for PG16
		_, err := ReconcileReplicationSlots(ctx, "instance2", db, &cluster)
		Expect(err).NotTo(HaveOccurred())
	})
})

var _ = Describe("cleanupOrphanedLogicalSlots", func() {
	var (
		mock sqlmock.Sqlmock
		db   *sql.DB
	)

	BeforeEach(func() {
		var err error
		db, mock, err = sqlmock.New()
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		Expect(mock.ExpectationsWereMet()).To(Succeed())
	})

	It("should drop only orphaned failover slots (synced=false, failover=true, active=false)",
		func(ctx SpecContext) {
			rows := sqlmock.NewRows([]string{"slot_name", "plugin", "active", "restart_lsn", "synced", "failover"}).
				AddRow("synced_slot", "pgoutput", false, "0/1234", true, true).   // synced=true, skip
				AddRow("orphan_slot", "pgoutput", false, "0/5678", false, true).  // orphaned failover slot, DROP
				AddRow("active_orphan", "pgoutput", true, "0/9ABC", false, true). // active, skip
				AddRow("external_sub", "pgoutput", false, "0/DEF0", false, false) // failover=false (external subscription), skip

			mock.ExpectQuery("SELECT .+ FROM pg_catalog.pg_replication_slots WHERE slot_type = 'logical'").
				WillReturnRows(rows)

			// Only orphan_slot should be dropped (synced=false, failover=true, active=false)
			mock.ExpectExec("SELECT pg_catalog.pg_drop_replication_slot").
				WithArgs("orphan_slot").
				WillReturnResult(sqlmock.NewResult(1, 1))

			err := cleanupOrphanedLogicalSlots(ctx, db)
			Expect(err).NotTo(HaveOccurred())
		})

	It("should NOT drop external subscription slots (failover=false)", func(ctx SpecContext) {
		// This test ensures we don't accidentally break external logical replication
		rows := sqlmock.NewRows([]string{"slot_name", "plugin", "active", "restart_lsn", "synced", "failover"}).
			AddRow("test_sub", "pgoutput", false, "0/5678", false, false) // external subscription slot

		mock.ExpectQuery("SELECT .+ FROM pg_catalog.pg_replication_slots WHERE slot_type = 'logical'").
			WillReturnRows(rows)

		// No deletion should occur - test_sub has failover=false
		err := cleanupOrphanedLogicalSlots(ctx, db)
		Expect(err).NotTo(HaveOccurred())
	})

	It("should do nothing when no orphaned failover slots exist", func(ctx SpecContext) {
		rows := sqlmock.NewRows([]string{"slot_name", "plugin", "active", "restart_lsn", "synced", "failover"}).
			AddRow("synced_slot", "pgoutput", false, "0/1234", true, true)

		mock.ExpectQuery("SELECT .+ FROM pg_catalog.pg_replication_slots WHERE slot_type = 'logical'").
			WillReturnRows(rows)

		err := cleanupOrphanedLogicalSlots(ctx, db)
		Expect(err).NotTo(HaveOccurred())
	})

	It("should return error when listing slots fails", func(ctx SpecContext) {
		mock.ExpectQuery("SELECT .+ FROM pg_catalog.pg_replication_slots WHERE slot_type = 'logical'").
			WillReturnError(errors.New("mock error"))

		err := cleanupOrphanedLogicalSlots(ctx, db)
		Expect(err).To(HaveOccurred())
	})

	It("should return error when deleting a slot fails", func(ctx SpecContext) {
		rows := sqlmock.NewRows([]string{"slot_name", "plugin", "active", "restart_lsn", "synced", "failover"}).
			AddRow("orphan_slot", "pgoutput", false, "0/5678", false, true)

		mock.ExpectQuery("SELECT .+ FROM pg_catalog.pg_replication_slots WHERE slot_type = 'logical'").
			WillReturnRows(rows)

		mock.ExpectExec("SELECT pg_catalog.pg_drop_replication_slot").
			WithArgs("orphan_slot").
			WillReturnError(errors.New("delete error"))

		err := cleanupOrphanedLogicalSlots(ctx, db)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("delete error"))
	})
})
