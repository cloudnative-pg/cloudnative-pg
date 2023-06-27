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

package runner

import (
	"context"
	"fmt"

	"k8s.io/utils/pointer"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/controller/slots/infrastructure"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type fakeSlot struct {
	name       string
	restartLSN string
}

type fakeSlotManager struct {
	slots        map[string]fakeSlot
	slotsUpdated int
	slotsCreated int
	slotsDeleted int
}

func (sm *fakeSlotManager) List(
	_ context.Context,
	_ *apiv1.ReplicationSlotsConfiguration,
) (infrastructure.ReplicationSlotList, error) {
	var slotList infrastructure.ReplicationSlotList
	for _, slot := range sm.slots {
		slotList.Items = append(slotList.Items, infrastructure.ReplicationSlot{
			SlotName:   slot.name,
			RestartLSN: slot.restartLSN,
			Type:       infrastructure.SlotTypePhysical,
			Active:     false,
		})
	}
	return slotList, nil
}

func (sm *fakeSlotManager) Update(_ context.Context, slot infrastructure.ReplicationSlot) error {
	localSlot, found := sm.slots[slot.SlotName]
	if !found {
		return fmt.Errorf("while updating slot: Slot %s not found", slot.SlotName)
	}
	if localSlot.restartLSN != slot.RestartLSN {
		sm.slots[slot.SlotName] = fakeSlot{name: slot.SlotName, restartLSN: slot.RestartLSN}
		sm.slotsUpdated++
	}
	return nil
}

func (sm *fakeSlotManager) Create(_ context.Context, slot infrastructure.ReplicationSlot) error {
	if _, found := sm.slots[slot.SlotName]; found {
		return fmt.Errorf("while creating slot: Slot %s already exists", slot.SlotName)
	}
	sm.slots[slot.SlotName] = fakeSlot{name: slot.SlotName, restartLSN: slot.RestartLSN}
	sm.slotsCreated++
	return nil
}

func (sm *fakeSlotManager) Delete(_ context.Context, slot infrastructure.ReplicationSlot) error {
	if _, found := sm.slots[slot.SlotName]; !found {
		return fmt.Errorf("while deleting slot: Slot %s not found", slot.SlotName)
	}
	delete(sm.slots, slot.SlotName)
	sm.slotsDeleted++
	return nil
}

var _ = Describe("Slot synchronization", func() {
	ctx := context.TODO()
	localPodName := "cluster-2"
	localSlotName := "_cnpg_cluster_2"
	slot3 := "cluster-3"
	slot4 := "cluster-4"

	primary := &fakeSlotManager{
		slots: map[string]fakeSlot{
			localSlotName: {name: localSlotName, restartLSN: "0/301C4D8"},
			slot3:         {name: slot3, restartLSN: "0/302C4D8"},
			slot4:         {name: slot4, restartLSN: "0/303C4D8"},
		},
	}
	local := &fakeSlotManager{
		slots: map[string]fakeSlot{},
	}
	config := apiv1.ReplicationSlotsConfiguration{
		HighAvailability: &apiv1.ReplicationSlotsHAConfiguration{
			Enabled:    pointer.Bool(true),
			SlotPrefix: "_cnpg_",
		},
	}

	It("can create slots in local from those on primary", func() {
		localSlotsBefore, err := local.List(ctx, &config)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(localSlotsBefore.Items).Should(HaveLen(0))

		err = synchronizeReplicationSlots(context.TODO(), primary, local, localPodName, &config)
		Expect(err).ShouldNot(HaveOccurred())

		localSlotsAfter, err := local.List(ctx, &config)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(localSlotsAfter.Items).Should(HaveLen(2))
		Expect(localSlotsAfter.Has(slot3)).To(BeTrue())
		Expect(localSlotsAfter.Has(slot4)).To(BeTrue())
		Expect(local.slotsCreated).To(Equal(2))
	})
	It("can update slots in local when ReplayLSN in primary advanced", func() {
		// advance slot3 in primary
		newLSN := "0/308C4D8"
		err := primary.Update(ctx, infrastructure.ReplicationSlot{SlotName: slot3, RestartLSN: newLSN})
		Expect(err).ShouldNot(HaveOccurred())

		err = synchronizeReplicationSlots(context.TODO(), primary, local, localPodName, &config)
		Expect(err).ShouldNot(HaveOccurred())

		localSlotsAfter, err := local.List(ctx, &config)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(localSlotsAfter.Items).Should(HaveLen(2))
		Expect(localSlotsAfter.Has(slot3)).To(BeTrue())
		slot := localSlotsAfter.Get(slot3)
		Expect(slot.RestartLSN).To(Equal(newLSN))
		Expect(local.slotsUpdated).To(Equal(1))
	})
	It("can drop slots in local when they are no longer in primary", func() {
		err := primary.Delete(ctx, infrastructure.ReplicationSlot{SlotName: slot4})
		Expect(err).ShouldNot(HaveOccurred())

		err = synchronizeReplicationSlots(context.TODO(), primary, local, localPodName, &config)
		Expect(err).ShouldNot(HaveOccurred())

		localSlotsAfter, err := local.List(ctx, &config)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(localSlotsAfter.Items).Should(HaveLen(1))
		Expect(localSlotsAfter.Has(slot3)).To(BeTrue())
		Expect(local.slotsDeleted).To(Equal(1))
	})
})
