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

package reconciler

import (
	"context"

	"k8s.io/utils/pointer"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/controller/slots/infrastructure"

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

func (fk fakeReplicationSlotManager) Create(_ context.Context, slot infrastructure.ReplicationSlot) error {
	fk.replicationSlots[fakeSlot{name: slot.SlotName}] = true
	return nil
}

func (fk fakeReplicationSlotManager) Delete(_ context.Context, slot infrastructure.ReplicationSlot) error {
	delete(fk.replicationSlots, fakeSlot{name: slot.SlotName})
	return nil
}

func (fk fakeReplicationSlotManager) Update(_ context.Context, _ infrastructure.ReplicationSlot) error {
	return nil
}

func (fk fakeReplicationSlotManager) List(
	_ context.Context,
	_ *apiv1.ReplicationSlotsConfiguration,
) (infrastructure.ReplicationSlotList, error) {
	var slotList infrastructure.ReplicationSlotList
	for slot := range fk.replicationSlots {
		slotList.Items = append(slotList.Items, infrastructure.ReplicationSlot{
			SlotName:   slot.name,
			RestartLSN: "",
			Type:       infrastructure.SlotTypePhysical,
			Active:     slot.active,
		})
	}
	return slotList, nil
}

func makeClusterWithInstanceNames(instanceNames []string, primary string) apiv1.Cluster {
	return apiv1.Cluster{
		Spec: apiv1.ClusterSpec{
			ReplicationSlots: &apiv1.ReplicationSlotsConfiguration{
				HighAvailability: &apiv1.ReplicationSlotsHAConfiguration{
					Enabled:    pointer.Bool(true),
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

		_, err := ReconcileReplicationSlots(context.TODO(), "instance1", fakeSlotManager, &cluster)
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

		_, err := ReconcileReplicationSlots(context.TODO(), "instance1", fakeSlotManager, &cluster)
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

		_, err := ReconcileReplicationSlots(context.TODO(), "instance1", fakeSlotManager, &cluster)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(fakeSlotManager.replicationSlots[fakeSlot{name: slotPrefix + "instance3", active: true}]).To(BeTrue())
		Expect(fakeSlotManager.replicationSlots).To(HaveLen(2))
	})
})
