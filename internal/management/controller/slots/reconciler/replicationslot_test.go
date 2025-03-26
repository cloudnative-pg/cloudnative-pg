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
