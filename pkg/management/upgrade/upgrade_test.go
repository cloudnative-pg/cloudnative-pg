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

package upgrade

import (
	"context"
	"errors"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("validateInstanceManagerHash", func() {
	const (
		clusterName  = "test-cluster"
		namespace    = "test-namespace"
		instanceArch = "amd64"
		hash         = "precalculatedHash"
	)
	var (
		ctx        context.Context
		fakeClient client.Client
		cluster    *apiv1.Cluster
	)
	BeforeEach(func() {
		fakeClient = fake.NewClientBuilder().WithScheme(schemeBuilder.BuildWithAllKnownScheme()).Build()

		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterName,
				Namespace: namespace,
			},
			Status: apiv1.ClusterStatus{
				AvailableArchitectures: []apiv1.AvailableArchitecture{
					{
						GoArch: instanceArch,
						Hash:   hash,
					},
				},
			},
		}
		_ = fakeClient.Create(ctx, cluster)
	})

	It("succeeds", func() {
		err := validateInstanceManagerHash(
			fakeClient,
			clusterName,
			namespace,
			instanceArch,
			hash,
		)
		Expect(err).ToNot(HaveOccurred())
	})

	It("fails when the requested arch is missing", func() {
		err := validateInstanceManagerHash(
			fakeClient,
			clusterName,
			namespace,
			"arm64",
			"arm64hash",
		)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(Equal("missing architecture arm64"))
	})

	It("fails when the hash doesn't match", func() {
		err := validateInstanceManagerHash(
			fakeClient,
			clusterName,
			namespace,
			instanceArch,
			"differentHash",
		)
		Expect(err).To(HaveOccurred())
		Expect(errors.Is(err, ErrorInvalidInstanceManagerBinary)).To(BeTrue())
	})
})
