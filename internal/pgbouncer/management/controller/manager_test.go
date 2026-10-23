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

package controller

import (
	"k8s.io/utils/ptr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// fakePgBouncerInstance is a test double tracking pause/resume calls.
type fakePgBouncerInstance struct {
	paused      bool
	pauseCalls  int
	resumeCalls int
}

func (f *fakePgBouncerInstance) Paused() bool { return f.paused }

func (f *fakePgBouncerInstance) Pause() error {
	f.pauseCalls++
	f.paused = true
	return nil
}

func (f *fakePgBouncerInstance) Resume() error {
	f.resumeCalls++
	f.paused = false
	return nil
}

func (f *fakePgBouncerInstance) Reload() error { return nil }

var _ = Describe("synchronizePause", func() {
	newPooler := func(specPaused, statusPaused bool) *apiv1.Pooler {
		pooler := &apiv1.Pooler{Spec: apiv1.PoolerSpec{PgBouncer: &apiv1.PgBouncerSpec{}}}
		if specPaused {
			pooler.Spec.PgBouncer.Paused = ptr.To(true)
		}
		pooler.Status.PausedForSwitchover = statusPaused
		return pooler
	}

	It("pauses when the operator paused it for switchover via status, without touching spec", func() {
		instance := &fakePgBouncerInstance{}
		r := &PgBouncerReconciler{instance: instance}

		pooler := newPooler(false, true)
		Expect(r.synchronizePause(pooler)).To(Succeed())

		Expect(instance.pauseCalls).To(Equal(1))
		Expect(instance.paused).To(BeTrue())
		// The pause came entirely from status: the user-owned spec stays unpaused.
		Expect(pooler.Spec.PgBouncer.IsPaused()).To(BeFalse())
	})

	It("resumes when the switchover pause is cleared from status", func() {
		instance := &fakePgBouncerInstance{paused: true}
		r := &PgBouncerReconciler{instance: instance}

		pooler := newPooler(false, false)
		Expect(r.synchronizePause(pooler)).To(Succeed())

		Expect(instance.resumeCalls).To(Equal(1))
		Expect(instance.paused).To(BeFalse())
	})

	It("still honors a user pause requested via spec", func() {
		instance := &fakePgBouncerInstance{}
		r := &PgBouncerReconciler{instance: instance}

		pooler := newPooler(true, false)
		Expect(r.synchronizePause(pooler)).To(Succeed())

		Expect(instance.pauseCalls).To(Equal(1))
		Expect(instance.paused).To(BeTrue())
	})

	It("stays paused while either spec or status still asks for it", func() {
		instance := &fakePgBouncerInstance{paused: true}
		r := &PgBouncerReconciler{instance: instance}

		// Switchover pause cleared from status, but the user still wants it paused.
		pooler := newPooler(true, false)
		Expect(r.synchronizePause(pooler)).To(Succeed())

		Expect(instance.resumeCalls).To(BeZero())
		Expect(instance.paused).To(BeTrue())
	})
})
