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
	"k8s.io/apimachinery/pkg/util/validation/field"

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
})

var _ = Describe("Pooler validateMonitoring", func() {
	var v *PoolerCustomValidator
	BeforeEach(func() {
		v = &PoolerCustomValidator{}
	})

	// tlsOnPooler builds a Pooler with monitoring.tls.enabled=true and the
	// requested PodMonitor/clientTLSSecret state. Every validateMonitoring
	// test below exercises the tls-enabled branch; the tls-disabled short-
	// circuit has its own dedicated test above.
	tlsOnPooler := func(enablePodMonitor bool, clientTLSSecret *apiv1.LocalObjectReference) *apiv1.Pooler {
		return &apiv1.Pooler{
			Spec: apiv1.PoolerSpec{
				PgBouncer: &apiv1.PgBouncerSpec{
					ClientTLSSecret: clientTLSSecret,
				},
				Monitoring: &apiv1.PoolerMonitoringConfiguration{ //nolint:staticcheck
					EnablePodMonitor: enablePodMonitor,
					TLSConfig:        &apiv1.PoolerMonitoringTLSConfiguration{Enabled: true},
				},
			},
		}
	}

	It("returns no error when metrics TLS is disabled", func() {
		Expect(v.validateMonitoring(&apiv1.Pooler{})).To(BeEmpty())
	})

	It("returns no error when metrics TLS is enabled and clientTLSSecret is set", func() {
		pooler := tlsOnPooler(true, &apiv1.LocalObjectReference{Name: "my-tls"})
		Expect(v.validateMonitoring(pooler)).To(BeEmpty())
	})

	It("returns no error when metrics TLS is enabled and the generated PodMonitor is disabled",
		func() {
			// Escape hatch: with enablePodMonitor=false (the default) the operator does not
			// generate a PodMonitor, so there is no misleading operator-side TLS config to
			// prevent. The user is wiring up their own scraper.
			pooler := tlsOnPooler(false, nil)
			Expect(v.validateMonitoring(pooler)).To(BeEmpty())
		})

	It("rejects when metrics TLS is enabled, generated PodMonitor is on, and clientTLSSecret is missing",
		func() {
			pooler := tlsOnPooler(true, nil)
			errs := v.validateMonitoring(pooler)
			Expect(errs).To(HaveLen(1))
			Expect(errs[0].Field).To(Equal("spec.pgbouncer.clientTLSSecret"))
			Expect(errs[0].Type).To(Equal(field.ErrorTypeRequired))
		})

	It("rejects when clientTLSSecret has an empty name", func() {
		pooler := tlsOnPooler(true, &apiv1.LocalObjectReference{Name: ""})
		Expect(v.validateMonitoring(pooler)).To(HaveLen(1))
	})

	It("rejects when pgbouncer is nil", func() {
		pooler := &apiv1.Pooler{
			Spec: apiv1.PoolerSpec{
				Monitoring: &apiv1.PoolerMonitoringConfiguration{ //nolint:staticcheck
					EnablePodMonitor: true,
					TLSConfig:        &apiv1.PoolerMonitoringTLSConfiguration{Enabled: true},
				},
			},
		}
		Expect(v.validateMonitoring(pooler)).To(HaveLen(1))
	})
})
