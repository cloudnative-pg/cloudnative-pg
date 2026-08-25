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

package leaseobserver

import (
	"errors"
	"time"

	coordinationv1 "k8s.io/api/coordination/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func leaseWithHolder(holder string, renewTime time.Time, durationSeconds *int32) *coordinationv1.Lease {
	var renew *metav1.MicroTime
	if !renewTime.IsZero() {
		renew = &metav1.MicroTime{Time: renewTime}
	}
	return &coordinationv1.Lease{
		Spec: coordinationv1.LeaseSpec{
			HolderIdentity:       ptr.To(holder),
			RenewTime:            renew,
			LeaseDurationSeconds: durationSeconds,
		},
	}
}

var _ = Describe("Tracker.Observe", func() {
	var (
		tracker *Tracker
		now     time.Time
		key     types.NamespacedName
	)

	BeforeEach(func() {
		now = time.Now()
		tracker = NewTracker()
		tracker.now = func() time.Time { return now }
		key = types.NamespacedName{Namespace: "default", Name: "cluster-example"}
	})

	It("reports NotHeld when the lease is not found, clearing any prior observation", func() {
		result := tracker.Observe(key, leaseWithHolder("pod-1", now, ptr.To(int32(15))), nil)
		Expect(result.Outcome).To(Equal(Held))

		notFoundErr := apierrors.NewNotFound(schema.GroupResource{Resource: "leases"}, key.Name)
		result = tracker.Observe(key, nil, notFoundErr)
		Expect(result.Outcome).To(Equal(NotHeld))

		// A subsequent observation of a held lease starts a fresh window,
		// rather than resuming the previous one.
		result = tracker.Observe(key, leaseWithHolder("pod-1", now, ptr.To(int32(15))), nil)
		Expect(result.Outcome).To(Equal(Held))
		Expect(result.ObservedFor).To(BeZero())
	})

	It("reports NotHeld when HolderIdentity is empty", func() {
		result := tracker.Observe(key, leaseWithHolder("", now, ptr.To(int32(15))), nil)
		Expect(result.Outcome).To(Equal(NotHeld))
	})

	It("reports Held, never Expired, on the very first sight of a held lease", func() {
		oldRenew := now.Add(-1 * time.Hour)
		result := tracker.Observe(key, leaseWithHolder("pod-1", oldRenew, ptr.To(int32(15))), nil)
		Expect(result.Outcome).To(Equal(Held))
		Expect(result.ObservedFor).To(BeZero())
		Expect(result.Holder).To(Equal("pod-1"))
		Expect(result.Duration).To(Equal(15 * time.Second))
	})

	It("reports Expired once observed unchanged for a full LeaseDurationSeconds", func() {
		renewTime := now
		tracker.Observe(key, leaseWithHolder("pod-1", renewTime, ptr.To(int32(15))), nil)

		now = now.Add(20 * time.Second)
		result := tracker.Observe(key, leaseWithHolder("pod-1", renewTime, ptr.To(int32(15))), nil)
		Expect(result.Outcome).To(Equal(Expired))
		Expect(result.ObservedFor).To(Equal(20 * time.Second))
	})

	It("reports Held while observed for less than LeaseDurationSeconds", func() {
		renewTime := now
		tracker.Observe(key, leaseWithHolder("pod-1", renewTime, ptr.To(int32(15))), nil)

		now = now.Add(10 * time.Second)
		result := tracker.Observe(key, leaseWithHolder("pod-1", renewTime, ptr.To(int32(15))), nil)
		Expect(result.Outcome).To(Equal(Held))
	})

	It("resets the window when the holder renews (RenewTime changes)", func() {
		renewTime := now
		tracker.Observe(key, leaseWithHolder("pod-1", renewTime, ptr.To(int32(15))), nil)

		now = now.Add(20 * time.Second)
		renewTime = now
		result := tracker.Observe(key, leaseWithHolder("pod-1", renewTime, ptr.To(int32(15))), nil)
		Expect(result.Outcome).To(Equal(Held))
		Expect(result.ObservedFor).To(BeZero())
	})

	It("resets the window when a different, non-empty holder takes over", func() {
		renewTime := now
		tracker.Observe(key, leaseWithHolder("pod-1", renewTime, ptr.To(int32(15))), nil)

		now = now.Add(20 * time.Second) // pod-1's window would already be Expired
		result := tracker.Observe(key, leaseWithHolder("pod-2", renewTime, ptr.To(int32(15))), nil)
		Expect(result.Outcome).To(Equal(Held))
		Expect(result.ObservedFor).To(BeZero())
		Expect(result.Holder).To(Equal("pod-2"))
	})

	It("reports Unverifiable when a held lease has no LeaseDurationSeconds", func() {
		result := tracker.Observe(key, leaseWithHolder("pod-1", now, nil), nil)
		Expect(result.Outcome).To(Equal(Unverifiable))
		Expect(result.Holder).To(Equal("pod-1"))
	})

	It("reports Unverifiable on a non-NotFound Get error, leaving prior state untouched", func() {
		renewTime := now
		tracker.Observe(key, leaseWithHolder("pod-1", renewTime, ptr.To(int32(15))), nil)

		now = now.Add(5 * time.Second)
		result := tracker.Observe(key, nil, errors.New("apiserver unreachable"))
		Expect(result.Outcome).To(Equal(Unverifiable))

		// Advancing past the full duration from the *original* observedAt
		// still reaches Expired, proving the failed call above did not reset
		// the window.
		now = now.Add(15 * time.Second)
		result = tracker.Observe(key, leaseWithHolder("pod-1", renewTime, ptr.To(int32(15))), nil)
		Expect(result.Outcome).To(Equal(Expired))
	})

	It("does not panic when Forget is called on an untracked key", func() {
		Expect(func() { tracker.Forget(key) }).NotTo(Panic())
	})

	It("is independent of clock skew between the holder and the controller", func() {
		futureRenew := now.Add(1 * time.Hour)
		result := tracker.Observe(key, leaseWithHolder("pod-1", futureRenew, ptr.To(int32(15))), nil)
		Expect(result.Outcome).To(Equal(Held))

		now = now.Add(20 * time.Second)
		result = tracker.Observe(key, leaseWithHolder("pod-1", futureRenew, ptr.To(int32(15))), nil)
		Expect(result.Outcome).To(Equal(Expired))
	})
})
