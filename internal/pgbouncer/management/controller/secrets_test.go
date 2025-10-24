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
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("getSecrets tests", func() {
	var (
		client client.WithWatch
		pooler *apiv1.Pooler
	)

	BeforeEach(func() {
		client, pooler = buildTestEnv()
	})

	Context("when status is not populated yet", func() {
		It("should return error", func(ctx context.Context) {
			pooler.Status.Secrets = nil

			_, err := getSecrets(ctx, client, pooler)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(Equal("status not populated yet"))
		})
	})

	Context("when all secrets are found", func() {
		It("should return secrets without error", func(ctx context.Context) {
			res, err := getSecrets(ctx, client, pooler)

			Expect(err).ToNot(HaveOccurred())
			Expect(res.ClientCA.Name).To(Equal(clientCAName))
			Expect(res.ClientTLS.Name).To(Equal(clientTLSName))
			Expect(res.ServerCA.Name).To(Equal(serverCAName))
			Expect(res.AuthQuery).To(BeNil())
		})
	})

	Context("when a secret is not found", func() {
		BeforeEach(func() {
			pooler.Status.Secrets.ServerCA = apiv1.SecretVersion{Name: "nonexistent"}
		})

		It("should return error", func(ctx context.Context) {
			_, err := getSecrets(ctx, client, pooler)

			Expect(err).To(HaveOccurred())
		})
	})
})
