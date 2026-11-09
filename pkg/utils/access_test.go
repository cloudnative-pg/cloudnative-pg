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

package utils

import (
	"errors"

	authorizationv1 "k8s.io/api/authorization/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("CheckClusterWideAccess", func() {
	var (
		kubeClient *kubefake.Clientset
		reviews    []authorizationv1.ResourceAttributes
		denied     string
		reviewErr  error
	)

	resources := []string{"clusterroles", "clusterrolebindings"}
	verbs := []string{"list", "watch"}

	BeforeEach(func() {
		reviews = nil
		denied = ""
		reviewErr = nil
		kubeClient = kubefake.NewClientset()
		kubeClient.PrependReactor("create", "selfsubjectaccessreviews",
			func(action k8stesting.Action) (bool, runtime.Object, error) {
				review := action.(k8stesting.CreateAction).GetObject().(*authorizationv1.SelfSubjectAccessReview)
				reviews = append(reviews, *review.Spec.ResourceAttributes)
				if reviewErr != nil {
					return true, nil, reviewErr
				}
				if review.Spec.ResourceAttributes.Resource == denied {
					review.Status.Reason = "no RBAC policy matched"
					return true, review, nil
				}
				review.Status.Allowed = true
				return true, review, nil
			})
	})

	It("checks every verb on every resource cluster-wide", func(ctx SpecContext) {
		allowed, reason, err := CheckClusterWideAccess(ctx, kubeClient, "rbac.authorization.k8s.io", resources, verbs)
		Expect(err).ToNot(HaveOccurred())
		Expect(allowed).To(BeTrue())
		Expect(reason).To(BeEmpty())

		Expect(reviews).To(HaveLen(4))
		for _, review := range reviews {
			Expect(review.Namespace).To(BeEmpty())
			Expect(review.Group).To(Equal("rbac.authorization.k8s.io"))
		}
	})

	It("reports the first missing permission", func(ctx SpecContext) {
		denied = "clusterrolebindings"

		allowed, reason, err := CheckClusterWideAccess(ctx, kubeClient, "rbac.authorization.k8s.io", resources, verbs)
		Expect(err).ToNot(HaveOccurred())
		Expect(allowed).To(BeFalse())
		Expect(reason).To(Equal(
			"cannot list clusterrolebindings.rbac.authorization.k8s.io cluster-wide: no RBAC policy matched"))
		Expect(reviews).To(HaveLen(3))
	})

	It("fails when the review cannot be created", func(ctx SpecContext) {
		reviewErr = errors.New("connection refused")

		allowed, _, err := CheckClusterWideAccess(ctx, kubeClient, "rbac.authorization.k8s.io", resources, verbs)
		Expect(err).To(MatchError(ContainSubstring("connection refused")))
		Expect(allowed).To(BeFalse())
	})
})
