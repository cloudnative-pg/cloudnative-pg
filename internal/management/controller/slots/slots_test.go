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
