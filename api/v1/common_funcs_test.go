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

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// crossNamespaceReferrer is a resource that may refer to a cluster living in
// another namespace
type crossNamespaceReferrer interface {
	GetClusterRef() ClusterObjectReference
	GetClusterNamespace() string
	IsCrossNamespace() bool
}

var _ = Describe("Cross-namespace cluster references", func() {
	meta := metav1.ObjectMeta{Name: "obj", Namespace: "app"}

	// newReferrers returns a resource of every kind supporting cross-namespace
	// references, living in the "app" namespace and referring to the passed
	// cluster
	newReferrers := func(ref ClusterObjectReference) map[string]crossNamespaceReferrer {
		return map[string]crossNamespaceReferrer{
			"Database":     &Database{ObjectMeta: meta, Spec: DatabaseSpec{ClusterRef: ref}},
			"DatabaseRole": &DatabaseRole{ObjectMeta: meta, Spec: DatabaseRoleSpec{ClusterRef: ref}},
			"Publication":  &Publication{ObjectMeta: meta, Spec: PublicationSpec{ClusterRef: ref}},
			"Subscription": &Subscription{ObjectMeta: meta, Spec: SubscriptionSpec{ClusterRef: ref}},
		}
	}

	DescribeTable("resolve the namespace of the cluster",
		func(ref ClusterObjectReference, expectedNamespace string, expectedCrossNamespace bool) {
			for kind, referrer := range newReferrers(ref) {
				Expect(referrer.GetClusterRef()).To(Equal(ref), kind)
				Expect(referrer.GetClusterNamespace()).To(Equal(expectedNamespace), kind)
				Expect(referrer.IsCrossNamespace()).To(Equal(expectedCrossNamespace), kind)
			}
		},
		Entry("without a namespace",
			ClusterObjectReference{Name: "pg"}, "app", false),
		Entry("with the namespace of the resource",
			ClusterObjectReference{Name: "pg", Namespace: "app"}, "app", false),
		Entry("with another namespace",
			ClusterObjectReference{Name: "pg", Namespace: "postgres"}, "postgres", true),
	)
})
