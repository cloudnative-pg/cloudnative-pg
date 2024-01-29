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
	"errors"
	"strings"
	"time"

	"k8s.io/utils/ptr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/controller/slots/infrastructure"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type fakeSlot struct {
	name   string
	active bool
	isHA   bool
}

type fakeReplicationSlotManager struct {
	replicationSlots   map[fakeSlot]bool
	triggerListError   bool
	triggerDeleteError bool
}

const slotPrefix = "_cnpg_"

func (fk fakeReplicationSlotManager) Create(_ context.Context, slot infrastructure.ReplicationSlot) error {
	isHA := strings.HasPrefix(slot.SlotName, slotPrefix)
	fk.replicationSlots[fakeSlot{name: slot.SlotName, isHA: isHA}] = true
	return nil
}

func (fk fakeReplicationSlotManager) Delete(_ context.Context, slot infrastructure.ReplicationSlot) error {
	if fk.triggerDeleteError {
		return errors.New("triggered delete error")
	}
	delete(fk.replicationSlots, fakeSlot{name: slot.SlotName, active: slot.Active, isHA: slot.IsHA})
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
	if fk.triggerListError {
		return slotList, errors.New("triggered list error")
	}

	for slot := range fk.replicationSlots {
		slotList.Items = append(slotList.Items, infrastructure.ReplicationSlot{
			SlotName:   slot.name,
			RestartLSN: "",
			Type:       infrastructure.SlotTypePhysical,
			Active:     slot.active,
			IsHA:       slot.isHA,
		})
	}
	return slotList, nil
}

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

var _ = Describe("HA Replication Slots reconciliation in Primary", func() {
	It("can create a new replication slot for a new cluster instance", func() {
		fakeSlotManager := fakeReplicationSlotManager{
			replicationSlots: map[fakeSlot]bool{
				{name: slotPrefix + "instance1", isHA: true}: true,
				{name: slotPrefix + "instance2", isHA: true}: true,
			},
		}

		cluster := makeClusterWithInstanceNames([]string{"instance1", "instance2", "instance3"}, "instance1")

		Expect(fakeSlotManager.replicationSlots).To(HaveLen(2))
		Expect(fakeSlotManager.replicationSlots[fakeSlot{name: "_cnpg_instance1", isHA: true}]).To(BeTrue())
		Expect(fakeSlotManager.replicationSlots[fakeSlot{name: "_cnpg_instance2", isHA: true}]).To(BeTrue())

		_, err := ReconcileReplicationSlots(context.TODO(), "instance1", fakeSlotManager, &cluster)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(fakeSlotManager.replicationSlots[fakeSlot{name: "_cnpg_instance1", isHA: true}]).To(BeFalse())
		Expect(fakeSlotManager.replicationSlots[fakeSlot{name: "_cnpg_instance3", isHA: true}]).To(BeTrue())
		Expect(fakeSlotManager.replicationSlots[fakeSlot{name: "_cnpg_instance2", isHA: true}]).To(BeTrue())
		Expect(fakeSlotManager.replicationSlots).To(HaveLen(2))
	})

	It("can delete an inactive HA replication slot that is not in the cluster", func() {
		fakeSlotManager := fakeReplicationSlotManager{
			replicationSlots: map[fakeSlot]bool{
				{name: slotPrefix + "instance1", isHA: true}: true,
				{name: slotPrefix + "instance2", isHA: true}: true,
				{name: slotPrefix + "instance3", isHA: true}: true,
			},
		}

		cluster := makeClusterWithInstanceNames([]string{"instance1", "instance2"}, "instance1")

		Expect(fakeSlotManager.replicationSlots).To(HaveLen(3))

		_, err := ReconcileReplicationSlots(context.TODO(), "instance1", fakeSlotManager, &cluster)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(fakeSlotManager.replicationSlots[fakeSlot{name: "_cnpg_instance3", isHA: true}]).To(BeFalse())
		Expect(fakeSlotManager.replicationSlots).To(HaveLen(1))
	})

	It("will not delete an active HA replication slot that is not in the cluster", func() {
		fakeSlotManager := fakeReplicationSlotManager{
			replicationSlots: map[fakeSlot]bool{
				{name: slotPrefix + "instance1", isHA: true}:               true,
				{name: slotPrefix + "instance2", isHA: true}:               true,
				{name: slotPrefix + "instance3", isHA: true, active: true}: true,
			},
		}

		cluster := makeClusterWithInstanceNames([]string{"instance1", "instance2"}, "instance1")

		Expect(fakeSlotManager.replicationSlots).To(HaveLen(3))

		_, err := ReconcileReplicationSlots(context.TODO(), "instance1", fakeSlotManager, &cluster)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(fakeSlotManager.replicationSlots[fakeSlot{name: slotPrefix + "instance3", isHA: true, active: true}]).
			To(BeTrue())
		Expect(fakeSlotManager.replicationSlots).To(HaveLen(2))
	})
})

var _ = Describe("dropReplicationSlots", func() {
	It("returns error when listing slots fails", func() {
		fakeManager := &fakeReplicationSlotManager{
			replicationSlots: make(map[fakeSlot]bool),
			triggerListError: true,
		}
		cluster := makeClusterWithInstanceNames([]string{}, "")

		_, err := dropReplicationSlots(context.Background(), fakeManager, &cluster, true, true)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("triggered list error"))
	})

	It("skips deletion of active HA slots and reschedules", func() {
		fakeManager := &fakeReplicationSlotManager{
			replicationSlots: map[fakeSlot]bool{
				{name: "slot1", active: true, isHA: true}: true,
			},
		}
		cluster := makeClusterWithInstanceNames([]string{}, "")

		res, err := dropReplicationSlots(context.Background(), fakeManager, &cluster, true, true)
		Expect(err).NotTo(HaveOccurred())
		Expect(res.RequeueAfter).To(Equal(time.Second))
	})

	It("skips the deletion of user defined replication slots on the primary", func() {
		fakeManager := &fakeReplicationSlotManager{
			replicationSlots: map[fakeSlot]bool{
				{name: "slot1", active: true}: true,
			},
		}
		cluster := makeClusterWithInstanceNames([]string{}, "")

		res, err := dropReplicationSlots(context.Background(), fakeManager, &cluster, true, true)
		Expect(err).NotTo(HaveOccurred())
		Expect(res.RequeueAfter).To(Equal(time.Duration(0)))
		Expect(res.IsZero()).To(BeTrue())
	})

	It("returns error when deleting a slot fails", func() {
		fakeManager := &fakeReplicationSlotManager{
			replicationSlots: map[fakeSlot]bool{
				{name: "slot1", active: false, isHA: true}: true,
			},
			triggerDeleteError: true,
		}
		cluster := makeClusterWithInstanceNames([]string{}, "")

		_, err := dropReplicationSlots(context.Background(), fakeManager, &cluster, true, true)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("triggered delete error"))
	})

	It("deletes inactive slots and does not reschedule", func() {
		fakeManager := &fakeReplicationSlotManager{
			replicationSlots: map[fakeSlot]bool{
				{name: "slot1", active: false, isHA: true}: true,
			},
		}
		cluster := makeClusterWithInstanceNames([]string{}, "")

		res, err := dropReplicationSlots(context.Background(), fakeManager, &cluster, true, true)
		Expect(err).NotTo(HaveOccurred())
		Expect(res.RequeueAfter).To(Equal(time.Duration(0)))
		Expect(fakeManager.replicationSlots).NotTo(HaveKey(fakeSlot{name: "slot1", active: false}))
	})
})
