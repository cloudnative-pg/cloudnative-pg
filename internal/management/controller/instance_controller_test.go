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
	"errors"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/internal/webhook/guard"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/certs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	instancecertificate "github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/instance/certificate"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// invalidClusterValidator drives the admission guard's validation-failure path,
// so EnsureResourceIsAdmitted short-circuits the reconcile loop with a requeue,
// reproducing an invalid cached Cluster.
type invalidClusterValidator struct{}

func (invalidClusterValidator) ValidateCreate(context.Context, *apiv1.Cluster) (admission.Warnings, error) {
	return nil, errors.New("cluster is invalid")
}

func (invalidClusterValidator) ValidateUpdate(
	context.Context, *apiv1.Cluster, *apiv1.Cluster,
) (admission.Warnings, error) {
	return nil, nil
}

func (invalidClusterValidator) ValidateDelete(context.Context, *apiv1.Cluster) (admission.Warnings, error) {
	return nil, nil
}

var _ = Describe("instance reconciler health probe certificate", func() {
	It("loads the server certificate even when the cached Cluster fails admission validation",
		func(ctx SpecContext) {
			const (
				namespace        = "default"
				clusterName      = "cluster-example"
				serverSecretName = clusterName + "-server"
			)

			root, err := certs.CreateRootCA("common-name", "organization-unit")
			Expect(err).ToNot(HaveOccurred())
			pair, err := root.CreateAndSignPair("host", certs.CertTypeServer, nil)
			Expect(err).ToNot(HaveOccurred())
			serverSecret := pair.GenerateCertificateSecret(namespace, serverSecretName)

			cluster := &apiv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: clusterName},
				Status: apiv1.ClusterStatus{
					Certificates: apiv1.CertificatesStatus{
						CertificatesConfiguration: apiv1.CertificatesConfiguration{
							ServerTLSSecret: serverSecretName,
						},
					},
				},
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
				WithObjects(cluster, serverSecret).
				Build()

			pgInstance := postgres.NewInstance().
				WithNamespace(namespace).
				WithPodName(clusterName + "-1").
				WithClusterName(clusterName)

			reconciler := &InstanceReconciler{
				client:                fakeClient,
				instance:              pgInstance,
				certificateReconciler: instancecertificate.NewReconciler(fakeClient, pgInstance),
				admission:             &guard.Admission[*apiv1.Cluster]{Validator: invalidClusterValidator{}},
			}

			result, err := reconciler.Reconcile(ctx, reconcile.Request{})

			// The guard requeued without error: the loop short-circuited, so
			// nothing below the guard (RefreshSecrets included) ran.
			Expect(err).ToNot(HaveOccurred())
			Expect(result.RequeueAfter).To(BeNumerically(">", 0))

			// The probe certificate was still loaded, because the load runs
			// before the guard. This fails if the call is moved back below it.
			Expect(pgInstance.GetServerCertificate()).ToNot(BeNil())
		})
})

var _ = Describe("enforced parameters alignment for followers", func() {
	It("treats a parameter missing from the comparison map as zero", func() {
		options := enforcedParametersToAlign(
			map[string]int{"max_connections": 200},
			map[string]int{},
		)
		Expect(options).To(Equal(map[string]string{"max_connections": "200"}))
	})

	It("keeps the pg_controldata value when it is higher than the startup value", func() {
		options := enforcedParametersToAlign(
			map[string]int{"max_connections": 200},
			map[string]int{"max_connections": 100},
		)
		Expect(options).To(Equal(map[string]string{"max_connections": "200"}))
	})

	It("changes nothing when the cluster spec is at least as high as pg_controldata", func() {
		options := enforcedParametersToAlign(
			map[string]int{"max_connections": 100, "max_wal_senders": 10},
			map[string]int{"max_connections": 200, "max_wal_senders": 10},
		)
		Expect(options).To(BeEmpty())
	})

	It("evaluates each parameter independently", func() {
		options := enforcedParametersToAlign(
			map[string]int{
				"max_connections":           200,
				"max_wal_senders":           10,
				"max_worker_processes":      32,
				"max_prepared_transactions": 0,
			},
			map[string]int{
				"max_connections":      100,
				"max_wal_senders":      35,
				"max_worker_processes": 8,
			},
		)
		Expect(options).To(Equal(map[string]string{
			"max_connections":      "200",
			"max_worker_processes": "32",
		}))
	})
})
