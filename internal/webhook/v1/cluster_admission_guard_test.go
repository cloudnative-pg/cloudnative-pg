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
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/internal/webhook/guard"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Cluster admission guard", func() {
	var (
		cluster    *apiv1.Cluster
		fakeClient client.Client
		writes     int
	)

	// countWrites counts every write the guard sends to the API server,
	// status included
	countWrites := interceptor.Funcs{
		Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
			writes++
			return c.Update(ctx, obj, opts...)
		},
		Patch: func(
			ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption,
		) error {
			writes++
			return c.Patch(ctx, obj, patch, opts...)
		},
		SubResourceUpdate: func(
			ctx context.Context, c client.Client, subResourceName string, obj client.Object,
			opts ...client.SubResourceUpdateOption,
		) error {
			writes++
			return c.SubResource(subResourceName).Update(ctx, obj, opts...)
		},
		SubResourcePatch: func(
			ctx context.Context, c client.Client, subResourceName string, obj client.Object, patch client.Patch,
			opts ...client.SubResourcePatchOption,
		) error {
			writes++
			return c.SubResource(subResourceName).Patch(ctx, obj, patch, opts...)
		},
	}

	admit := func(ctx context.Context) {
		result, err := NewClusterAdmissionGuard().EnsureResourceIsAdmitted(ctx,
			guard.AdmissionParams[*apiv1.Cluster]{
				Object:       cluster,
				Client:       fakeClient,
				ApplyChanges: true,
			})
		Expect(err).ToNot(HaveOccurred())
		Expect(result.IsZero()).To(BeTrue())
	}

	BeforeEach(func(ctx SpecContext) {
		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "cluster-example"},
			Spec: apiv1.ClusterSpec{
				Instances:            2,
				StorageConfiguration: apiv1.StorageConfiguration{Size: "1Gi"},
			},
		}
		// A defaulted Cluster, so the guard has nothing to mutate and only
		// runs the validation
		Expect((&ClusterCustomDefaulter{}).Default(ctx, cluster)).To(Succeed())

		writes = 0
		fakeClient = fake.NewClientBuilder().
			WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
			WithObjects(cluster).
			WithStatusSubresource(&apiv1.Cluster{}).
			WithInterceptorFuncs(countWrites).
			Build()
	})

	It("keeps the reason of a Cluster waiting for the user, without writing it", func(ctx SpecContext) {
		cluster.Status.Phase = apiv1.PhaseWaitingForUser
		cluster.Status.PhaseReason = "User must issue a supervised switchover"

		admit(ctx)

		// Clearing the reason here is what made the reconcilers persist an
		// empty reason that RegisterPhase then set again, in a loop
		Expect(cluster.Status.Phase).To(BeEquivalentTo(apiv1.PhaseWaitingForUser))
		Expect(cluster.Status.PhaseReason).To(Equal("User must issue a supervised switchover"))
		Expect(writes).To(BeZero())
	})

	It("keeps the reason of a Cluster on every reconcile", func(ctx SpecContext) {
		cluster.Status.Phase = apiv1.PhaseWaitingForUser
		cluster.Status.PhaseReason = "User must issue a supervised switchover"

		for range 3 {
			admit(ctx)
		}

		Expect(cluster.Status.PhaseReason).To(Equal("User must issue a supervised switchover"))
		Expect(writes).To(BeZero())
	})

	It("clears a previous admission error once the Cluster is valid again", func(ctx SpecContext) {
		cluster.Status.Phase = apiv1.PhaseDefinitionInvalid
		cluster.Status.PhaseReason = "spec.instances: Invalid value"

		admit(ctx)

		// The reason is cleared in memory; persisting the new phase is left
		// to the phase machinery, so the guard itself does not write
		Expect(cluster.Status.PhaseReason).To(BeEmpty())
		Expect(writes).To(BeZero())
	})
})
