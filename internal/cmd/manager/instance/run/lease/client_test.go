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

package lease

import (
	"context"

	coordinationv1 "k8s.io/api/coordination/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("annotatingLeasesGetter", func() {
	const (
		namespace = "test-ns"
		name      = "test-cluster"
		other     = "example.com/other"
	)

	var (
		ctx        context.Context
		kubeClient *fake.Clientset
		position   *utils.LeasePosition
		leases     *annotatingLeases
	)

	newLease := func() *coordinationv1.Lease {
		return &coordinationv1.Lease{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:   namespace,
				Name:        name,
				Annotations: map[string]string{other: "kept"},
			},
		}
	}

	stored := func() *coordinationv1.Lease {
		result, err := kubeClient.CoordinationV1().Leases(namespace).Get(ctx, name, metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())
		return result
	}

	BeforeEach(func() {
		ctx = context.Background()
		kubeClient = fake.NewSimpleClientset()
		position = &utils.LeasePosition{
			LSN:      "0/7000028",
			Timeline: 3,
			State:    utils.PgDataStateInProduction,
			Pod:      "test-cluster-1",
		}
		getter := &annotatingLeasesGetter{
			LeasesGetter: kubeClient.CoordinationV1(),
			source: func(context.Context, *coordinationv1.Lease) *utils.LeasePosition {
				return position
			},
		}
		leases = getter.Leases(namespace).(*annotatingLeases)
	})

	It("sets the annotation on Create, next to the existing ones", func() {
		_, err := leases.Create(ctx, newLease(), metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		expected, err := position.Marshal()
		Expect(err).ToNot(HaveOccurred())
		Expect(stored().Annotations).To(Equal(map[string]string{
			other:                             "kept",
			utils.LeasePositionAnnotationName: expected,
		}))
	})

	It("sets the annotation on Update, replacing the previous value", func() {
		lease := newLease()
		lease.Annotations[utils.LeasePositionAnnotationName] = `{"state":"shut down"}`
		_, err := kubeClient.CoordinationV1().Leases(namespace).Create(ctx, lease, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		_, err = leases.Update(ctx, lease, metav1.UpdateOptions{})
		Expect(err).ToNot(HaveOccurred())

		expected, err := position.Marshal()
		Expect(err).ToNot(HaveOccurred())
		Expect(stored().Annotations).To(HaveKeyWithValue(utils.LeasePositionAnnotationName, expected))
		Expect(stored().Annotations).To(HaveKeyWithValue(other, "kept"))
	})

	It("does not change the lease it is given", func() {
		lease := newLease()
		_, err := leases.Create(ctx, lease, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())
		Expect(lease.Annotations).To(Equal(map[string]string{other: "kept"}))
	})

	It("passes the lease being written to the source", func() {
		var seen *coordinationv1.Lease
		leases.source = func(_ context.Context, lease *coordinationv1.Lease) *utils.LeasePosition {
			seen = lease
			return nil
		}

		lease := newLease()
		_, err := leases.Create(ctx, lease, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())
		Expect(seen).To(BeIdenticalTo(lease))
	})

	It("leaves the annotations alone when the source has no position", func() {
		position = nil
		lease := newLease()
		lease.Annotations[utils.LeasePositionAnnotationName] = `{"state":"shut down"}`
		_, err := kubeClient.CoordinationV1().Leases(namespace).Create(ctx, lease, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		_, err = leases.Update(ctx, lease, metav1.UpdateOptions{})
		Expect(err).ToNot(HaveOccurred())
		Expect(stored().Annotations).To(Equal(lease.Annotations))
	})

	It("writes the lease when there is no source at all", func() {
		leases.source = nil
		_, err := leases.Create(ctx, newLease(), metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())
		Expect(stored().Annotations).To(Equal(map[string]string{other: "kept"}))
	})
})
