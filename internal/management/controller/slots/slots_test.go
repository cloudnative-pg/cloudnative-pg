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

package slots

import (
	"context"
	"fmt"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func newSlot(name string) ReplicationSlot {
	return ReplicationSlot{Name: name}
}

var _ = Describe("ReplicationSlotList", func() {
	It("has a working Has method", func() {
		slot1 := newSlot("slot1")
		slot2 := newSlot("slot2")
		list := ReplicationSlotList{Items: []ReplicationSlot{slot1, slot2}}

		Expect(list.Has("slot1")).To(BeTrue())
		Expect(list.Has("slot2")).To(BeTrue())
		Expect(list.Has("slot3")).ToNot(BeTrue())
	})
	It("has a working Get method", func() {
		slot1 := newSlot("slot1")
		slot2 := newSlot("slot2")
		list := ReplicationSlotList{Items: []ReplicationSlot{slot1, slot2}}

		Expect(list.Get("slot1")).To(BeEquivalentTo(&slot1))
		Expect(list.Get("slot2")).To(BeEquivalentTo(&slot2))
		Expect(list.Get("slot3")).To(BeNil())
	})
	It("works as expected when the list is empty", func() {
		var list ReplicationSlotList

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

func (sm *fakeSlotManager) getSlotsStatus(
	ctx context.Context,
	podName string,
	config *apiv1.ReplicationSlotsConfiguration,
) (ReplicationSlotList, error) {
	var slotList ReplicationSlotList
	for _, slot := range sm.slots {
		if slot.name != podName {
			slotList.Items = append(slotList.Items, ReplicationSlot{
				Name:       slot.name,
				RestartLSN: slot.restartLSN,
				Type:       "physical",
				Active:     true,
			})
		}
	}
	return slotList, nil
}

func (sm *fakeSlotManager) updateSlot(ctx context.Context, slot ReplicationSlot) error {
	localSlot, found := sm.slots[slot.Name]
	if !found {
		return fmt.Errorf("while updating slot: Slot %s not found", slot.Name)
	}
	if localSlot.restartLSN != slot.RestartLSN {
		sm.slots[slot.Name] = fakeSlot{name: slot.Name, restartLSN: slot.RestartLSN}
		sm.slotsUpdated++
	}
	return nil
}

func (sm *fakeSlotManager) createSlot(ctx context.Context, slot ReplicationSlot) error {
	if _, found := sm.slots[slot.Name]; found {
		return fmt.Errorf("while creating slot: Slot %s already exists", slot.Name)
	}
	sm.slots[slot.Name] = fakeSlot{name: slot.Name, restartLSN: slot.RestartLSN}
	sm.slotsCreated++
	return nil
}

func (sm *fakeSlotManager) dropSlot(ctx context.Context, slot ReplicationSlot) error {
	if _, found := sm.slots[slot.Name]; !found {
		return fmt.Errorf("while deleting slot: Slot %s not found", slot.Name)
	}
	delete(sm.slots, slot.Name)
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
		localSlotsBefore, err := local.getSlotsStatus(ctx, localPodName, &config)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(localSlotsBefore.Items).Should(HaveLen(0))

		err = synchronizeReplicationSlots(context.TODO(), primary, local, localPodName, &config)
		Expect(err).ShouldNot(HaveOccurred())

		localSlotsAfter, err := local.getSlotsStatus(ctx, localPodName, &config)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(localSlotsAfter.Items).Should(HaveLen(2))
		Expect(localSlotsAfter.Has(pod3)).To(BeTrue())
		Expect(localSlotsAfter.Has(pod4)).To(BeTrue())
		Expect(local.slotsCreated).To(Equal(2))
	})
	It("can update slots in local when ReplayLSN in primary advanced", func() {
		// advance slot3 in primary
		newLSN := "0/308C4D8"
		err := primary.updateSlot(ctx, ReplicationSlot{Name: pod3, RestartLSN: newLSN})
		Expect(err).ShouldNot(HaveOccurred())

		err = synchronizeReplicationSlots(context.TODO(), primary, local, localPodName, &config)
		Expect(err).ShouldNot(HaveOccurred())

		localSlotsAfter, err := local.getSlotsStatus(ctx, localPodName, &config)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(localSlotsAfter.Items).Should(HaveLen(2))
		Expect(localSlotsAfter.Has(pod3)).To(BeTrue())
		slot := localSlotsAfter.Get(pod3)
		Expect(slot.RestartLSN).To(Equal(newLSN))
		Expect(local.slotsUpdated).To(Equal(1))
	})
	It("can drop slots in local when they are no longer in primary", func() {
		err := primary.dropSlot(ctx, ReplicationSlot{Name: pod4})
		Expect(err).ShouldNot(HaveOccurred())

		err = synchronizeReplicationSlots(context.TODO(), primary, local, localPodName, &config)
		Expect(err).ShouldNot(HaveOccurred())

		localSlotsAfter, err := local.getSlotsStatus(ctx, localPodName, &config)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(localSlotsAfter.Items).Should(HaveLen(1))
		Expect(localSlotsAfter.Has(pod3)).To(BeTrue())
		Expect(local.slotsDeleted).To(Equal(1))
	})
})
