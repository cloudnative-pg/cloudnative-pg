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

package controller

import (
	"context"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/controller/slots"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type fakeSlot struct {
	name   string
	active bool
}

type fakeReplicationSlotManager struct {
	replicationSlots map[fakeSlot]bool
}

const slotPrefix = "_cnpg_"

func (fk fakeReplicationSlotManager) GetCurrentHAReplicationSlots(
	instanceName string,
	cluster *apiv1.Cluster,
) (*slots.ReplicationSlotList, error) {
	var slotList slots.ReplicationSlotList
	for slot := range fk.replicationSlots {
		slotList.Items = append(slotList.Items, slots.ReplicationSlot{
			SlotName: slot.name,
			Type:     slots.SlotTypePhysical,
			Active:   slot.active,
		})
	}
	return &slotList, nil
}

func (fk fakeReplicationSlotManager) Create(ctx context.Context, slot slots.ReplicationSlot) error {
	fk.replicationSlots[fakeSlot{name: slot.SlotName}] = true
	return nil
}

func (fk fakeReplicationSlotManager) Delete(ctx context.Context, slot slots.ReplicationSlot) error {
	delete(fk.replicationSlots, fakeSlot{name: slot.SlotName})
	return nil
}

func (fk fakeReplicationSlotManager) Update(ctx context.Context, slot slots.ReplicationSlot) error {
	return nil
}

func (fk fakeReplicationSlotManager) List(
	ctx context.Context,
	podName string,
	config *apiv1.ReplicationSlotsConfiguration,
) (slots.ReplicationSlotList, error) {
	var slotList slots.ReplicationSlotList
	for slot, active := range fk.replicationSlots {
		if slot.name != podName {
			slotList.Items = append(slotList.Items, slots.ReplicationSlot{
				SlotName:   slot.name,
				RestartLSN: "",
				Type:       "physical",
				Active:     active,
			})
		}
	}
	return slotList, nil
}

func makeClusterWithInstanceNames(instanceNames []string, primary string) apiv1.Cluster {
	return apiv1.Cluster{
		Spec: apiv1.ClusterSpec{
			ReplicationSlots: &apiv1.ReplicationSlotsConfiguration{
				HighAvailability: &apiv1.ReplicationSlotsHAConfiguration{
					Enabled:    true,
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

var _ = Describe("HA Replication Slots reconciliation in Primary", func() {
	It("can create a new replication slot for a new cluster instance", func() {
		fakeSlotManager := fakeReplicationSlotManager{
			replicationSlots: map[fakeSlot]bool{
				{name: slotPrefix + "instance1"}: true,
				{name: slotPrefix + "instance2"}: true,
			},
		}

		cluster := makeClusterWithInstanceNames([]string{"instance1", "instance2", "instance3"}, "instance1")

		Expect(fakeSlotManager.replicationSlots).To(HaveLen(2))
		Expect(fakeSlotManager.replicationSlots[fakeSlot{name: "_cnpg_instance1"}]).To(BeTrue())
		Expect(fakeSlotManager.replicationSlots[fakeSlot{name: "_cnpg_instance2"}]).To(BeTrue())

		err := reconcileReplicationSlots(context.TODO(), "instance1", fakeSlotManager, &cluster)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(fakeSlotManager.replicationSlots[fakeSlot{name: "_cnpg_instance1"}]).To(BeFalse())
		Expect(fakeSlotManager.replicationSlots[fakeSlot{name: "_cnpg_instance3"}]).To(BeTrue())
		Expect(fakeSlotManager.replicationSlots[fakeSlot{name: "_cnpg_instance2"}]).To(BeTrue())
		Expect(fakeSlotManager.replicationSlots).To(HaveLen(2))
	})

	It("can delete an inactive replication slot that is not in the cluster", func() {
		fakeSlotManager := fakeReplicationSlotManager{
			replicationSlots: map[fakeSlot]bool{
				{name: slotPrefix + "instance1"}: true,
				{name: slotPrefix + "instance2"}: true,
				{name: slotPrefix + "instance3"}: true,
			},
		}

		cluster := makeClusterWithInstanceNames([]string{"instance1", "instance2"}, "instance1")

		Expect(fakeSlotManager.replicationSlots).To(HaveLen(3))

		err := reconcileReplicationSlots(context.TODO(), "instance1", fakeSlotManager, &cluster)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(fakeSlotManager.replicationSlots[fakeSlot{name: "_cnpg_instance3"}]).To(BeFalse())
		Expect(fakeSlotManager.replicationSlots).To(HaveLen(1))
	})

	It("will not delete an active replication slot that is not in the cluster", func() {
		fakeSlotManager := fakeReplicationSlotManager{
			replicationSlots: map[fakeSlot]bool{
				{name: slotPrefix + "instance1"}:               true,
				{name: slotPrefix + "instance2"}:               true,
				{name: slotPrefix + "instance3", active: true}: true,
			},
		}

		cluster := makeClusterWithInstanceNames([]string{"instance1", "instance2"}, "instance1")

		Expect(fakeSlotManager.replicationSlots).To(HaveLen(3))

		err := reconcileReplicationSlots(context.TODO(), "instance1", fakeSlotManager, &cluster)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(fakeSlotManager.replicationSlots[fakeSlot{name: slotPrefix + "instance3", active: true}]).To(BeTrue())
		Expect(fakeSlotManager.replicationSlots).To(HaveLen(2))
	})
})
