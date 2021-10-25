/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Pooler validation", func() {
	It("doesn't allow specifying authQuerySecret without any authQuery", func() {
		pooler := Pooler{
			Spec: PoolerSpec{
				PgBouncer: &PgBouncerSpec{
					AuthQuerySecret: &LocalObjectReference{
						Name: "test",
					},
				},
			},
		}

		Expect(pooler.validatePgBouncer()).NotTo(BeEmpty())
	})

	It("doesn't allow specifying authQuery without any authQuerySecret", func() {
		pooler := Pooler{
			Spec: PoolerSpec{
				PgBouncer: &PgBouncerSpec{
					AuthQuery: "test",
				},
			},
		}

		Expect(pooler.validatePgBouncer()).NotTo(BeEmpty())
	})

	It("allows having both authQuery and authQuerySecret", func() {
		pooler := Pooler{
			Spec: PoolerSpec{
				PgBouncer: &PgBouncerSpec{
					AuthQuery: "test",
					AuthQuerySecret: &LocalObjectReference{
						Name: "test",
					},
				},
			},
		}

		Expect(pooler.validatePgBouncer()).To(BeEmpty())
	})

	It("allows the autoconfiguration mode", func() {
		pooler := Pooler{
			Spec: PoolerSpec{
				PgBouncer: &PgBouncerSpec{},
			},
		}

		Expect(pooler.validatePgBouncer()).To(BeEmpty())
	})

	It("doesn't allow not specifying a cluster name", func() {
		pooler := Pooler{
			Spec: PoolerSpec{
				Cluster: LocalObjectReference{Name: ""},
			},
		}
		Expect(pooler.validateCluster()).NotTo(BeEmpty())
	})

	It("doesn't allow to have a pooler with the same name of the cluster", func() {
		pooler := Pooler{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test",
			},
			Spec: PoolerSpec{
				Cluster: LocalObjectReference{
					Name: "test",
				},
			},
		}
		Expect(pooler.validateCluster()).NotTo(BeEmpty())
	})

	It("doesn't complain when specifying a cluster name", func() {
		pooler := Pooler{
			Spec: PoolerSpec{
				Cluster: LocalObjectReference{Name: "cluster-example"},
			},
		}
		Expect(pooler.validateCluster()).To(BeEmpty())
	})

	It("does complain when given a fixed parameter", func() {
		pooler := Pooler{
			Spec: PoolerSpec{
				PgBouncer: &PgBouncerSpec{
					Parameters: map[string]string{"pool_mode": "test"},
				},
			},
		}
		Expect(pooler.validatePgbouncerGenericParameters()).NotTo(BeEmpty())
	})

	It("does not complain when given a valid parameter", func() {
		pooler := Pooler{
			Spec: PoolerSpec{
				PgBouncer: &PgBouncerSpec{
					Parameters: map[string]string{"verbose": "10"},
				},
			},
		}
		Expect(pooler.validatePgbouncerGenericParameters()).To(BeEmpty())
	})
})
