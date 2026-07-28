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
	"os"
	"path/filepath"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/internal/webhook/guard"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/certs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/constants"
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

var _ = Describe("reconcileCheckWalArchiveFile", func() {
	var (
		pgData     string
		filePath   string
		reconciler *InstanceReconciler
	)

	BeforeEach(func() {
		pgData = GinkgoT().TempDir()
		filePath = filepath.Join(pgData, constants.CheckEmptyWalArchiveFile)

		pgInstance := postgres.NewInstance()
		pgInstance.PgData = pgData
		reconciler = &InstanceReconciler{instance: pgInstance}
	})

	writeMarkerFile := func() {
		Expect(os.WriteFile(filePath, []byte{}, 0o600)).To(Succeed())
	}

	archivingCondition := func(status metav1.ConditionStatus, reason apiv1.ConditionReason) metav1.Condition {
		return metav1.Condition{
			Type:   string(apiv1.ConditionContinuousArchiving),
			Status: status,
			Reason: string(reason),
		}
	}

	clusterWithBarman := func(conditions ...metav1.Condition) *apiv1.Cluster {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Backup: &apiv1.BackupConfiguration{
					BarmanObjectStore: &apiv1.BarmanObjectStoreConfiguration{},
				},
			},
		}
		cluster.Status.Conditions = conditions
		return cluster
	}

	clusterWithoutArchiver := func(conditions ...metav1.Condition) *apiv1.Cluster {
		cluster := &apiv1.Cluster{}
		cluster.Status.Conditions = conditions
		return cluster
	}

	When("the cluster has a WAL archiver configured", func() {
		It("removes the marker file after a real archiving success", func() {
			writeMarkerFile()
			cluster := clusterWithBarman(
				archivingCondition(metav1.ConditionTrue, apiv1.ConditionReasonContinuousArchivingSuccess))

			Expect(reconciler.reconcileCheckWalArchiveFile(cluster)).To(Succeed())
			Expect(filePath).NotTo(BeAnExistingFile())
		})

		It("keeps the marker file while archiving is failing", func() {
			writeMarkerFile()
			cluster := clusterWithBarman(
				archivingCondition(metav1.ConditionFalse, apiv1.ConditionReasonContinuousArchivingFailing))

			Expect(reconciler.reconcileCheckWalArchiveFile(cluster)).To(Succeed())
			Expect(filePath).To(BeAnExistingFile())
		})

		It("keeps the marker file when no archiving condition is present", func() {
			writeMarkerFile()

			Expect(reconciler.reconcileCheckWalArchiveFile(clusterWithBarman())).To(Succeed())
			Expect(filePath).To(BeAnExistingFile())
		})

		It("keeps the marker file when the condition is a stale archiver-less no-op", func() {
			// Regression: right after an archiver is added, the condition still
			// holds True/NotConfigured from the archiver-less era. Consuming the
			// marker file here would skip the empty-archive check on the very first
			// real archiving attempt.
			writeMarkerFile()
			cluster := clusterWithBarman(
				archivingCondition(metav1.ConditionTrue, apiv1.ConditionReasonContinuousArchivingNotConfigured))

			Expect(reconciler.reconcileCheckWalArchiveFile(cluster)).To(Succeed())
			Expect(filePath).To(BeAnExistingFile())
		})

		It("treats an enabled plugin as a potential archiver", func() {
			writeMarkerFile()
			cluster := clusterWithoutArchiver(
				archivingCondition(metav1.ConditionTrue, apiv1.ConditionReasonContinuousArchivingNotConfigured))
			cluster.Spec.Plugins = []apiv1.PluginConfiguration{{Name: "some-plugin"}}

			Expect(reconciler.reconcileCheckWalArchiveFile(cluster)).To(Succeed())
			Expect(filePath).To(BeAnExistingFile())
		})
	})

	When("the cluster has no WAL archiver configured", func() {
		It("re-creates a missing marker file", func() {
			Expect(reconciler.reconcileCheckWalArchiveFile(clusterWithoutArchiver())).To(Succeed())
			Expect(filePath).To(BeAnExistingFile())
		})

		It("leaves an existing marker file in place", func() {
			writeMarkerFile()

			Expect(reconciler.reconcileCheckWalArchiveFile(clusterWithoutArchiver())).To(Succeed())
			Expect(filePath).To(BeAnExistingFile())
		})

		It("does not consume the marker file on a stale success condition", func() {
			// Upgrade path: a cluster created by an operator predating the
			// NotConfigured reason reports True/Success for its no-op archiving.
			writeMarkerFile()
			cluster := clusterWithoutArchiver(
				archivingCondition(metav1.ConditionTrue, apiv1.ConditionReasonContinuousArchivingSuccess))

			Expect(reconciler.reconcileCheckWalArchiveFile(cluster)).To(Succeed())
			Expect(filePath).To(BeAnExistingFile())
		})
	})
})
