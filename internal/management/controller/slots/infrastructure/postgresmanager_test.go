/*
Copyright The CloudNativePG Contributors

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

package infrastructure

import (
	"context"
	"database/sql"
	"errors"

	"github.com/DATA-DOG/go-sqlmock"

	v1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PostgresManager", func() {
	var (
		manager Manager
		mock    sqlmock.Sqlmock
		db      *sql.DB
		slot    ReplicationSlot
	)

	BeforeEach(func() {
		var err error
		db, mock, err = sqlmock.New()
		Expect(err).NotTo(HaveOccurred())
		manager = NewPostgresManager(&mockPooler{db: db})
		slot = ReplicationSlot{
			SlotName:   "slot1",
			Type:       SlotTypePhysical,
			Active:     true,
			RestartLSN: "lsn1",
		}
	})

	AfterEach(func() {
		Expect(mock.ExpectationsWereMet()).To(Succeed())
	})

	Context("Create", func() {
		It("should successfully create a replication slot", func() {
			mock.ExpectExec("SELECT pg_create_physical_replication_slot").
				WithArgs(slot.SlotName, slot.RestartLSN != "").
				WillReturnResult(sqlmock.NewResult(1, 1))

			err := manager.Create(context.Background(), slot)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should return error when the database execution fails", func() {
			mock.ExpectExec("SELECT pg_create_physical_replication_slot").
				WithArgs(slot.SlotName, slot.RestartLSN != "").
				WillReturnError(errors.New("mock error"))

			err := manager.Create(context.Background(), slot)
			Expect(err).To(HaveOccurred())
		})
	})

	Context("List", func() {
		var config *v1.ReplicationSlotsConfiguration
		BeforeEach(func() {
			config = &v1.ReplicationSlotsConfiguration{
				HighAvailability: &v1.ReplicationSlotsHAConfiguration{
					Enabled:    new(bool),
					SlotPrefix: "_cnpg_",
				},
				UpdateInterval: 30,
			}
		})

		It("should successfully list replication slots", func() {
			rows := sqlmock.NewRows([]string{"slot_name", "slot_type", "active", "restart_lsn"}).
				AddRow("_cnpg_slot1", string(SlotTypePhysical), true, "lsn1").
				AddRow("slot2", string(SlotTypePhysical), true, "lsn2")

			mock.ExpectQuery("^SELECT (.+) FROM pg_replication_slots").
				WillReturnRows(rows)

			result, err := manager.List(context.Background(), config)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Items).To(HaveLen(2))
			Expect(result.Has("_cnpg_slot1")).To(BeTrue())
			Expect(result.Has("slot2")).To(BeTrue())

			slot1 := result.Get("_cnpg_slot1")
			Expect(slot1.Type).To(Equal(SlotTypePhysical))
			Expect(slot1.Active).To(BeTrue())
			Expect(slot1.RestartLSN).To(Equal("lsn1"))
			Expect(slot1.IsHA).To(BeTrue())

			slot2 := result.Get("slot2")
			Expect(slot2.Type).To(Equal(SlotTypePhysical))
			Expect(slot2.Active).To(BeTrue())
			Expect(slot2.RestartLSN).To(Equal("lsn2"))
			Expect(slot2.IsHA).To(BeFalse())
		})

		It("should return error when database query fails", func() {
			mock.ExpectQuery("^SELECT (.+) FROM pg_replication_slots").
				WillReturnError(errors.New("mock error"))

			_, err := manager.List(context.Background(), config)
			Expect(err).To(HaveOccurred())
		})
	})

	Context("Update", func() {
		It("should successfully update a replication slot", func() {
			mock.ExpectExec("SELECT pg_replication_slot_advance").
				WithArgs(slot.SlotName, slot.RestartLSN).
				WillReturnResult(sqlmock.NewResult(1, 1))

			err := manager.Update(context.Background(), slot)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should return error when the database execution fails", func() {
			mock.ExpectExec("SELECT pg_replication_slot_advance").
				WithArgs(slot.SlotName, slot.RestartLSN).
				WillReturnError(errors.New("mock error"))

			err := manager.Update(context.Background(), slot)
			Expect(err).To(HaveOccurred())
		})

		It("should not update a replication slot when RestartLSN is empty", func() {
			slot.RestartLSN = ""
			err := manager.Update(context.Background(), slot)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("Delete", func() {
		It("should successfully delete a replication slot", func() {
			slot.Active = false

			mock.ExpectExec("SELECT pg_drop_replication_slot").WithArgs(slot.SlotName).
				WillReturnResult(sqlmock.NewResult(1, 1))

			err := manager.Delete(context.Background(), slot)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should return error when the database execution fails", func() {
			slot.Active = false
			mock.ExpectExec("SELECT pg_drop_replication_slot").WithArgs(slot.SlotName).
				WillReturnError(errors.New("mock error"))

			err := manager.Delete(context.Background(), slot)
			Expect(err).To(HaveOccurred())
		})

		It("should not delete an active replication slot", func() {
			slot.RestartLSN = ""

			err := manager.Delete(context.Background(), slot)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
