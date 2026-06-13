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
	coordinationv1 "k8s.io/api/coordination/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("primaryLeasePredicate", func() {
	lease := &coordinationv1.Lease{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster-example", Namespace: "default"},
	}

	It("ignores Create events", func() {
		Expect(primaryLeasePredicate.Create(event.CreateEvent{Object: lease})).To(BeFalse())
	})

	It("ignores Update events even when spec changes", func() {
		oldLease := lease.DeepCopy()
		newLease := lease.DeepCopy()
		one := int32(1)
		newLease.Spec.LeaseDurationSeconds = &one
		Expect(primaryLeasePredicate.Update(event.UpdateEvent{
			ObjectOld: oldLease,
			ObjectNew: newLease,
		})).To(BeFalse())
	})

	It("ignores Generic events", func() {
		Expect(primaryLeasePredicate.Generic(event.GenericEvent{Object: lease})).To(BeFalse())
	})

	It("forwards Delete events so the parent cluster reconciles and recreates the lease", func() {
		Expect(primaryLeasePredicate.Delete(event.DeleteEvent{Object: lease})).To(BeTrue())
	})
})
