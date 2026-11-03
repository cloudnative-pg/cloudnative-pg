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

package status

import (
	"context"
	"errors"

	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PatchObjectWithOptimisticLock", func() {
	newRole := func() *apiv1.DatabaseRole {
		return &apiv1.DatabaseRole{
			ObjectMeta: metav1.ObjectMeta{Name: "role-a", Namespace: "default"},
		}
	}

	It("reports the error the API server returned, instead of retrying or swallowing it", func(ctx SpecContext) {
		scheme := schemeBuilder.BuildWithAllKnownScheme()
		apiErr := apierrs.NewInternalError(errors.New("apiserver is down"))
		cli := fake.NewClientBuilder().
			WithScheme(scheme).
			WithStatusSubresource(&apiv1.DatabaseRole{}).
			WithObjects(newRole()).
			WithInterceptorFuncs(interceptor.Funcs{
				SubResourcePatch: func(
					_ context.Context, _ client.Client, _ string,
					_ client.Object, _ client.Patch, _ ...client.SubResourcePatchOption,
				) error {
					return apiErr
				},
			}).
			Build()

		_, err := PatchObjectWithOptimisticLock(ctx, cli, newRole(), func(role *apiv1.DatabaseRole) {
			role.Status.Password = &apiv1.GeneratedPasswordState{SecretName: "role-a-password"}
		})
		Expect(err).To(MatchError(ContainSubstring("apiserver is down")))
	})
})
