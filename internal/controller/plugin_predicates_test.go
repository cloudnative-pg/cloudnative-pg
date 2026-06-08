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
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("operatorNamespaceServiceEndpointSlicePredicate", func() {
	const operatorNamespace = "cnpg-system"

	predicate := operatorNamespaceServiceEndpointSlicePredicate(operatorNamespace)

	newSlice := func(namespace, serviceName string) *discoveryv1.EndpointSlice {
		slice := &discoveryv1.EndpointSlice{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "example-abcde",
				Namespace: namespace,
			},
		}
		if serviceName != "" {
			slice.Labels = map[string]string{
				discoveryv1.LabelServiceName: serviceName,
			}
		}
		return slice
	}

	It("accepts slices in the operator namespace that back a Service", func() {
		slice := newSlice(operatorNamespace, "some-plugin")
		Expect(predicate.Create(event.CreateEvent{Object: slice})).To(BeTrue())
	})

	It("rejects slices outside the operator namespace", func() {
		slice := newSlice("other-namespace", "some-plugin")
		Expect(predicate.Create(event.CreateEvent{Object: slice})).To(BeFalse())
	})

	It("rejects slices without the kubernetes.io/service-name label", func() {
		slice := newSlice(operatorNamespace, "")
		Expect(predicate.Create(event.CreateEvent{Object: slice})).To(BeFalse())
	})

	It("rejects slices whose kubernetes.io/service-name label is empty", func() {
		slice := newSlice(operatorNamespace, "")
		slice.Labels = map[string]string{discoveryv1.LabelServiceName: ""}
		Expect(predicate.Create(event.CreateEvent{Object: slice})).To(BeFalse())
	})

	// A plugin rollout does not create the EndpointSlice from scratch: the
	// slice already exists and its endpoints transition Ready/NotReady, so in
	// production the triggering event is almost always an Update. These cases
	// lock in that the predicate is evaluated against the updated object.
	It("accepts an update when the new slice is in the operator namespace and backs a Service", func() {
		Expect(predicate.Update(event.UpdateEvent{
			ObjectOld: newSlice(operatorNamespace, "some-plugin"),
			ObjectNew: newSlice(operatorNamespace, "some-plugin"),
		})).To(BeTrue())
	})

	It("rejects an update when the new slice is outside the operator namespace", func() {
		Expect(predicate.Update(event.UpdateEvent{
			ObjectOld: newSlice("other-namespace", "some-plugin"),
			ObjectNew: newSlice("other-namespace", "some-plugin"),
		})).To(BeFalse())
	})
})
