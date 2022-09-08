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
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
)

func newSlot(name string) ReplicationSlot {
	return ReplicationSlot{SlotName: name}
}

var _ = ginkgo.Describe("ReplicationSlotList", func() {
	ginkgo.It("has a working Has method", func() {
		slot1 := newSlot("slot1")
		slot2 := newSlot("slot2")
		list := ReplicationSlotList{Items: []ReplicationSlot{slot1, slot2}}

		gomega.Expect(list.Has("slot1")).To(gomega.BeTrue())
		gomega.Expect(list.Has("slot2")).To(gomega.BeTrue())
		gomega.Expect(list.Has("slot3")).ToNot(gomega.BeTrue())
	})
	ginkgo.It("has a working Get method", func() {
		slot1 := newSlot("slot1")
		slot2 := newSlot("slot2")
		list := ReplicationSlotList{Items: []ReplicationSlot{slot1, slot2}}

		gomega.Expect(list.Get("slot1")).To(gomega.BeEquivalentTo(&slot1))
		gomega.Expect(list.Get("slot2")).To(gomega.BeEquivalentTo(&slot2))
		gomega.Expect(list.Get("slot3")).To(gomega.BeNil())
	})
	ginkgo.It("works as expected when the list is empty", func() {
		var list ReplicationSlotList

		gomega.Expect(list.Get("slot1")).To(gomega.BeNil())
		gomega.Expect(list.Has("slot1")).ToNot(gomega.BeTrue())
	})
})
