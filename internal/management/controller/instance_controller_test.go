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
	"time"

	cnpgTypes "github.com/cloudnative-pg/machinery/pkg/types"
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

var _ = Describe("walReceiverExactlyCaughtUp", func() {
	It("is true when the received LSN equals the sender's reported WAL end position", func() {
		Expect(walReceiverExactlyCaughtUp("1/100", "1/100")).To(BeTrue())
	})

	It("is false when the sender's WAL end position is empty (unavailable, e.g. older instance manager)", func() {
		Expect(walReceiverExactlyCaughtUp("1/100", "")).To(BeFalse())
	})

	It("is false when the received LSN is empty", func() {
		Expect(walReceiverExactlyCaughtUp("", "1/100")).To(BeFalse())
	})

	It("is false when the sender's WAL end position is ahead (bytes still in flight)", func() {
		Expect(walReceiverExactlyCaughtUp("1/100", "1/200")).To(BeFalse())
	})
})

var _ = Describe("walReceiverStallDecision", func() {
	const grace = walReceiverStallGraceInPromotion

	It("is done immediately when the WAL receiver is down, regardless of LSN state", func() {
		now := time.Now()
		done, lastLSN, lastProgressAt := walReceiverStallDecision(
			false, "1/100", "1/50", now.Add(-time.Hour), now)
		Expect(done).To(BeTrue())
		Expect(lastLSN).To(Equal(cnpgTypes.LSN("1/100")))
		Expect(lastProgressAt).To(Equal(now))
	})

	It("keeps waiting and resets the clock when the LSN advances", func() {
		now := time.Now()
		staleProgress := now.Add(-grace) // already at the boundary, but progress resets it
		done, lastLSN, lastProgressAt := walReceiverStallDecision(
			true, "1/200", "1/100", staleProgress, now)
		Expect(done).To(BeFalse())
		Expect(lastLSN).To(Equal(cnpgTypes.LSN("1/200")))
		Expect(lastProgressAt).To(Equal(now))
	})

	It("keeps waiting while stalled but still inside the grace period", func() {
		now := time.Now()
		lastProgressAt := now.Add(-(grace - time.Second))
		done, lastLSN, newLastProgressAt := walReceiverStallDecision(
			true, "1/100", "1/100", lastProgressAt, now)
		Expect(done).To(BeFalse())
		Expect(lastLSN).To(Equal(cnpgTypes.LSN("1/100")))
		Expect(newLastProgressAt).To(Equal(lastProgressAt))
	})

	It("bypasses the wait exactly at the grace boundary", func() {
		now := time.Now()
		lastProgressAt := now.Add(-grace)
		done, lastLSN, newLastProgressAt := walReceiverStallDecision(
			true, "1/100", "1/100", lastProgressAt, now)
		Expect(done).To(BeTrue())
		Expect(lastLSN).To(Equal(cnpgTypes.LSN("1/100")))
		Expect(newLastProgressAt).To(Equal(lastProgressAt))
	})

	It("bypasses the wait once stalled past the grace period", func() {
		now := time.Now()
		lastProgressAt := now.Add(-(grace + 5*time.Second))
		done, lastLSN, newLastProgressAt := walReceiverStallDecision(
			true, "1/100", "1/100", lastProgressAt, now)
		Expect(done).To(BeTrue())
		Expect(lastLSN).To(Equal(cnpgTypes.LSN("1/100")))
		Expect(newLastProgressAt).To(Equal(lastProgressAt))
	})

	It("restarts the grace clock after an advance-then-freeze sequence", func() {
		t0 := time.Now()
		// First poll: no baseline yet, so the first observation counts as progress.
		done, lastLSN, lastProgressAt := walReceiverStallDecision(true, "1/0", "", t0, t0)
		Expect(done).To(BeFalse())
		Expect(lastLSN).To(Equal(cnpgTypes.LSN("1/0")))
		Expect(lastProgressAt).To(Equal(t0))

		// Advances partway through the grace window: the clock resets.
		t1 := t0.Add(grace - time.Second)
		done, lastLSN, lastProgressAt = walReceiverStallDecision(true, "1/100", lastLSN, lastProgressAt, t1)
		Expect(done).To(BeFalse())
		Expect(lastLSN).To(Equal(cnpgTypes.LSN("1/100")))
		Expect(lastProgressAt).To(Equal(t1))

		// Frozen at the same LSN for just under the (reset) grace period: still waiting.
		t2 := t1.Add(grace - time.Second)
		done, lastLSN, lastProgressAt = walReceiverStallDecision(true, "1/100", lastLSN, lastProgressAt, t2)
		Expect(done).To(BeFalse())
		Expect(lastProgressAt).To(Equal(t1))

		// Frozen past the reset grace period: now it bypasses.
		t3 := t1.Add(grace)
		done, _, lastProgressAt = walReceiverStallDecision(true, "1/100", lastLSN, lastProgressAt, t3)
		Expect(done).To(BeTrue())
		Expect(lastProgressAt).To(Equal(t1))
	})

	It("treats a NULL/empty received LSN as no progress and lets the stall clock run", func() {
		now := time.Now()
		lastProgressAt := now.Add(-(grace + time.Second))
		done, lastLSN, newLastProgressAt := walReceiverStallDecision(
			true, "", "", lastProgressAt, now)
		Expect(done).To(BeTrue())
		Expect(lastLSN).To(BeEmpty())
		Expect(newLastProgressAt).To(Equal(lastProgressAt))
	})

	It("does not treat an empty current LSN as progress even from a non-empty high-water mark", func() {
		now := time.Now()
		lastProgressAt := now.Add(-time.Second)
		done, lastLSN, newLastProgressAt := walReceiverStallDecision(
			true, "", "1/100", lastProgressAt, now)
		Expect(done).To(BeFalse())                        // still inside the grace period
		Expect(lastLSN).To(Equal(cnpgTypes.LSN("1/100"))) // high-water mark preserved: "" isn't progress
		Expect(newLastProgressAt).To(Equal(lastProgressAt))
	})

	It("does not treat an LSN going backward as progress or lower the high-water mark", func() {
		now := time.Now()
		lastProgressAt := now.Add(-(grace + time.Second))
		done, lastLSN, newLastProgressAt := walReceiverStallDecision(
			true, "1/50", "1/100", lastProgressAt, now)
		Expect(done).To(BeTrue())                         // no progress -> the stall clock keeps running -> bypass
		Expect(lastLSN).To(Equal(cnpgTypes.LSN("1/100"))) // high-water mark is kept, not lowered to "1/50"
		Expect(newLastProgressAt).To(Equal(lastProgressAt))
	})
})
