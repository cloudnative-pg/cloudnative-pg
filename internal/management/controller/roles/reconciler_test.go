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

package roles

import (
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Role reconciler test", func() {
	It("reconcile an empty cluster", func(ctx SpecContext) {
		cluster := &apiv1.Cluster{}
		instance := &postgres.Instance{}
		mockClient := fake.NewClientBuilder().Build()

		result, err := Reconcile(ctx, instance, cluster, mockClient)
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(BeEquivalentTo(reconcile.Result{}))
	})

	It("reconcile fails with no database connection", func(ctx SpecContext) {
		instance := &postgres.Instance{}
		mockClient := fake.NewClientBuilder().Build()
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Managed: &apiv1.ManagedConfiguration{
					Roles: []apiv1.RoleConfiguration{
						{
							Name:    "dante",
							Comment: "divine comedy",
						},
					},
				},
			},
		}
		pgStringError := "while listing DB roles for role reconciler: " +
			"failed to connect to `user=postgres database=postgres`: " +
			"/controller/run/.s.PGSQL.5432 (/controller/run): " +
			"dial error: dial unix /controller/run/.s.PGSQL.5432: connect: no such file or directory"
		result, err := Reconcile(ctx, instance, cluster, mockClient)
		Expect(err.Error()).To(BeEquivalentTo(pgStringError))
		Expect(result).To(BeEquivalentTo(reconcile.Result{}))
	})
})
