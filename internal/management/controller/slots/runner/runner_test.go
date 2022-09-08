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

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/controller/slots"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func newSlot(name string) slots.ReplicationSlot {
	return slots.ReplicationSlot{SlotName: name}
}

var _ = Describe("ReplicationSlotList", func() {
	It("has a working Has method", func() {
		slot1 := newSlot("slot1")
		slot2 := newSlot("slot2")
		list := slots.ReplicationSlotList{Items: []slots.ReplicationSlot{slot1, slot2}}

		Expect(list.Has("slot1")).To(BeTrue())
		Expect(list.Has("slot2")).To(BeTrue())
		Expect(list.Has("slot3")).ToNot(BeTrue())
	})
	It("has a working Get method", func() {
		slot1 := newSlot("slot1")
		slot2 := newSlot("slot2")
		list := slots.ReplicationSlotList{Items: []slots.ReplicationSlot{slot1, slot2}}

		Expect(list.Get("slot1")).To(BeEquivalentTo(&slot1))
		Expect(list.Get("slot2")).To(BeEquivalentTo(&slot2))
		Expect(list.Get("slot3")).To(BeNil())
	})
	It("works as expected when the list is empty", func() {
		var list slots.ReplicationSlotList

		Expect(list.Get("slot1")).To(BeNil())
		Expect(list.Has("slot1")).ToNot(BeTrue())
	})
})

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
	ctx context.Context,
	podName string,
	config *apiv1.ReplicationSlotsConfiguration,
) (slots.ReplicationSlotList, error) {
	var slotList slots.ReplicationSlotList
	for _, slot := range sm.slots {
		if slot.name != podName {
			slotList.Items = append(slotList.Items, slots.ReplicationSlot{
				SlotName:   slot.name,
				RestartLSN: slot.restartLSN,
				Type:       "physical",
				Active:     true,
			})
		}
	}
	return slotList, nil
}

func (sm *fakeSlotManager) Update(ctx context.Context, slot slots.ReplicationSlot) error {
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

func (sm *fakeSlotManager) Create(ctx context.Context, slot slots.ReplicationSlot) error {
	if _, found := sm.slots[slot.SlotName]; found {
		return fmt.Errorf("while creating slot: Slot %s already exists", slot.SlotName)
	}
	sm.slots[slot.SlotName] = fakeSlot{name: slot.SlotName, restartLSN: slot.RestartLSN}
	sm.slotsCreated++
	return nil
}

func (sm *fakeSlotManager) Delete(ctx context.Context, slot slots.ReplicationSlot) error {
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
	pod3 := "cluster-3"
	pod4 := "cluster-4"

	primary := &fakeSlotManager{
		slots: map[string]fakeSlot{
			localPodName: {name: localPodName, restartLSN: "0/301C4D8"},
			pod3:         {name: pod3, restartLSN: "0/302C4D8"},
			pod4:         {name: pod4, restartLSN: "0/303C4D8"},
		},
	}
	local := &fakeSlotManager{
		slots: map[string]fakeSlot{},
	}
	config := apiv1.ReplicationSlotsConfiguration{
		HighAvailability: &apiv1.ReplicationSlotsHAConfiguration{
			Enabled:    true,
			SlotPrefix: "_cnpg_",
		},
	}

	It("can create slots in local from those on primary", func() {
		localSlotsBefore, err := local.List(ctx, localPodName, &config)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(localSlotsBefore.Items).Should(HaveLen(0))

		err = synchronizeReplicationSlots(context.TODO(), primary, local, localPodName, &config)
		Expect(err).ShouldNot(HaveOccurred())

		localSlotsAfter, err := local.List(ctx, localPodName, &config)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(localSlotsAfter.Items).Should(HaveLen(2))
		Expect(localSlotsAfter.Has(pod3)).To(BeTrue())
		Expect(localSlotsAfter.Has(pod4)).To(BeTrue())
		Expect(local.slotsCreated).To(Equal(2))
	})
	It("can update slots in local when ReplayLSN in primary advanced", func() {
		// advance slot3 in primary
		newLSN := "0/308C4D8"
		err := primary.Update(ctx, slots.ReplicationSlot{SlotName: pod3, RestartLSN: newLSN})
		Expect(err).ShouldNot(HaveOccurred())

		err = synchronizeReplicationSlots(context.TODO(), primary, local, localPodName, &config)
		Expect(err).ShouldNot(HaveOccurred())

		localSlotsAfter, err := local.List(ctx, localPodName, &config)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(localSlotsAfter.Items).Should(HaveLen(2))
		Expect(localSlotsAfter.Has(pod3)).To(BeTrue())
		slot := localSlotsAfter.Get(pod3)
		Expect(slot.RestartLSN).To(Equal(newLSN))
		Expect(local.slotsUpdated).To(Equal(1))
	})
	It("can drop slots in local when they are no longer in primary", func() {
		err := primary.Delete(ctx, slots.ReplicationSlot{SlotName: pod4})
		Expect(err).ShouldNot(HaveOccurred())

		err = synchronizeReplicationSlots(context.TODO(), primary, local, localPodName, &config)
		Expect(err).ShouldNot(HaveOccurred())

		localSlotsAfter, err := local.List(ctx, localPodName, &config)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(localSlotsAfter.Items).Should(HaveLen(1))
		Expect(localSlotsAfter.Has(pod3)).To(BeTrue())
		Expect(local.slotsDeleted).To(Equal(1))
	})
})
