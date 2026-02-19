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

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Pooler validation", func() {
	var v *PoolerCustomValidator
	BeforeEach(func() {
		v = &PoolerCustomValidator{}
	})

	It("doesn't allow specifying authQuerySecret without any authQuery", func() {
		pooler := &apiv1.Pooler{
			Spec: apiv1.PoolerSpec{
				PgBouncer: &apiv1.PgBouncerSpec{
					AuthQuerySecret: &apiv1.LocalObjectReference{
						Name: "test",
					},
				},
			},
		}

		Expect(v.validatePgBouncer(pooler)).NotTo(BeEmpty())
	})

	It("doesn't allow specifying authQuery without any authQuerySecret", func() {
		pooler := &apiv1.Pooler{
			Spec: apiv1.PoolerSpec{
				PgBouncer: &apiv1.PgBouncerSpec{
					AuthQuery: "test",
				},
			},
		}

		Expect(v.validatePgBouncer(pooler)).NotTo(BeEmpty())
	})

	It("allows having both authQuery and authQuerySecret", func() {
		pooler := &apiv1.Pooler{
			Spec: apiv1.PoolerSpec{
				PgBouncer: &apiv1.PgBouncerSpec{
					AuthQuery: "test",
					AuthQuerySecret: &apiv1.LocalObjectReference{
						Name: "test",
					},
				},
			},
		}

		Expect(v.validatePgBouncer(pooler)).To(BeEmpty())
	})

	It("allows the autoconfiguration mode", func() {
		pooler := &apiv1.Pooler{
			Spec: apiv1.PoolerSpec{
				PgBouncer: &apiv1.PgBouncerSpec{},
			},
		}

		Expect(v.validatePgBouncer(pooler)).To(BeEmpty())
	})

	It("doesn't allow not specifying a cluster name", func() {
		pooler := &apiv1.Pooler{
			Spec: apiv1.PoolerSpec{
				Cluster: apiv1.LocalObjectReference{Name: ""},
			},
		}
		Expect(v.validateCluster(pooler)).NotTo(BeEmpty())
	})

	It("doesn't allow to have a pooler with the same name of the cluster", func() {
		pooler := &apiv1.Pooler{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test",
			},
			Spec: apiv1.PoolerSpec{
				Cluster: apiv1.LocalObjectReference{
					Name: "test",
				},
			},
		}
		Expect(v.validateCluster(pooler)).NotTo(BeEmpty())
	})

	It("doesn't complain when specifying a cluster name", func() {
		pooler := &apiv1.Pooler{
			Spec: apiv1.PoolerSpec{
				Cluster: apiv1.LocalObjectReference{Name: "cluster-example"},
			},
		}
		Expect(v.validateCluster(pooler)).To(BeEmpty())
	})

	It("does complain when given a fixed parameter", func() {
		pooler := &apiv1.Pooler{
			Spec: apiv1.PoolerSpec{
				PgBouncer: &apiv1.PgBouncerSpec{
					Parameters: map[string]string{"pool_mode": "test"},
				},
			},
		}
		Expect(v.validatePgbouncerGenericParameters(pooler)).NotTo(BeEmpty())
	})

	It("does not complain when given a valid parameter", func() {
		pooler := &apiv1.Pooler{
			Spec: apiv1.PoolerSpec{
				PgBouncer: &apiv1.PgBouncerSpec{
					Parameters: map[string]string{"verbose": "10"},
				},
			},
		}
		Expect(v.validatePgbouncerGenericParameters(pooler)).To(BeEmpty())
	})

	It("does not allow wildcard in databases list", func() {
		pooler := &apiv1.Pooler{
			Spec: apiv1.PoolerSpec{
				PgBouncer: &apiv1.PgBouncerSpec{
					Databases: []apiv1.PgBouncerDatabaseConfig{
						{Name: "mydb"},
						{Name: "*"},
					},
				},
			},
		}
		Expect(v.validatePgbouncerDatabases(pooler)).NotTo(BeEmpty())
	})

	It("allows databases without wildcard", func() {
		pooler := &apiv1.Pooler{
			Spec: apiv1.PoolerSpec{
				PgBouncer: &apiv1.PgBouncerSpec{
					Databases: []apiv1.PgBouncerDatabaseConfig{
						{Name: "mydb"},
						{Name: "otherdb"},
					},
				},
			},
		}
		Expect(v.validatePgbouncerDatabases(pooler)).To(BeEmpty())
	})

	It("allows empty databases list", func() {
		pooler := &apiv1.Pooler{
			Spec: apiv1.PoolerSpec{
				PgBouncer: &apiv1.PgBouncerSpec{
					Databases: []apiv1.PgBouncerDatabaseConfig{},
				},
			},
		}
		Expect(v.validatePgbouncerDatabases(pooler)).To(BeEmpty())
	})
})
