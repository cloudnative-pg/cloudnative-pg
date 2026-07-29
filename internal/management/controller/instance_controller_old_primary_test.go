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
	"os"
	"path/filepath"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/webserver/metricserver"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// newOldPrimaryTestReconciler builds an InstanceReconciler whose supporting
// reconcile steps (config file generation, metrics, monitoring queries, cache
// population, promotion token verification) are all no-ops given an empty
// Cluster spec, so that Reconcile can be driven all the way down to the old
// primary demotion / readiness gate ordering without a running PostgreSQL.
func newOldPrimaryTestReconciler(namespace, clusterName, podName string) (*InstanceReconciler, *postgres.Instance) {
	pgData := GinkgoT().TempDir()
	Expect(os.WriteFile(filepath.Join(pgData, "PG_VERSION"), []byte("17"), 0o600)).To(Succeed())

	pgInstance := postgres.NewInstance().
		WithNamespace(namespace).
		WithPodName(podName).
		WithClusterName(clusterName)
	pgInstance.PgData = pgData

	fakeClient := fake.NewClientBuilder().
		WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
		Build()

	reconciler := NewInstanceReconciler(
		pgInstance,
		fakeClient,
		metricserver.NewExporter(pgInstance, nil),
		nil,
		nil,
		nil,
		record.NewFakeRecorder(10),
	)
	// Skip the first-reconcile initialization path: it is unrelated to the
	// ordering under test and pulls in bootstrap concerns of its own.
	reconciler.firstReconcileDone.Store(true)

	return reconciler, pgInstance
}

var _ = Describe("old primary demotion ordering", func() {
	const (
		namespace   = "default"
		clusterName = "cluster-example"
		oldPrimary  = "cluster-example-1"
		newPrimary  = "cluster-example-2"
	)

	// The old primary's PostgreSQL is unresponsive (there is no real postgres
	// running in this test, so IsReady() / pg_isready always fails) and the
	// cluster has already moved TargetPrimary away from it. Before this fix,
	// the IsReady() gate returned a requeue before reconcileOldPrimary was ever
	// reached, and the demotion request was never sent. After the fix, the
	// demotion request must be sent regardless of readiness.
	It("reaches the demotion request even though the instance is not ready", func(ctx SpecContext) {
		reconciler, pgInstance := newOldPrimaryTestReconciler(namespace, clusterName, oldPrimary)

		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: clusterName},
			Status: apiv1.ClusterStatus{
				TargetPrimary: newPrimary,
			},
		}
		Expect(reconciler.GetClient().Create(ctx, cluster)).To(Succeed())

		// Nobody would normally drain this channel until the demotion request
		// is processed and the shutdown chain unwinds. For this test we only
		// care that the request was sent, which proves reconcileOldPrimary was
		// reached before Reconcile blocked. We then cancel the reconcile
		// context, which is what unblocks reconcileOldPrimary's <-ctx.Done().
		reconcileCtx, cancel := context.WithCancel(ctx)
		defer cancel()

		received := make(chan postgres.InstanceCommand, 1)
		go func() {
			select {
			case cmd := <-pgInstance.GetInstanceCommandChan():
				received <- cmd
				cancel()
			case <-reconcileCtx.Done():
			}
		}()

		_, err := reconciler.Reconcile(reconcileCtx, reconcile.Request{})
		Expect(err).ToNot(HaveOccurred())

		Eventually(received).WithTimeout(5 * time.Second).Should(Receive())
	})

	// The mirror case: the instance is not ready and it IS the target primary
	// (it has not been demoted away from). reconcileOldPrimary must no-op in
	// this case (cluster.Status.TargetPrimary == r.instance.GetPodName()), and
	// the instance must fall through to the readiness gate and requeue, never
	// touching the instance command channel.
	It("does not demote the instance when it is still the target primary", func(ctx SpecContext) {
		reconciler, pgInstance := newOldPrimaryTestReconciler(namespace, clusterName, oldPrimary)

		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: clusterName},
			Status: apiv1.ClusterStatus{
				TargetPrimary: oldPrimary,
			},
		}
		Expect(reconciler.GetClient().Create(ctx, cluster)).To(Succeed())

		received := make(chan postgres.InstanceCommand, 1)
		go func() {
			select {
			case cmd := <-pgInstance.GetInstanceCommandChan():
				received <- cmd
			case <-time.After(2 * time.Second):
			}
		}()

		result, err := reconciler.Reconcile(ctx, reconcile.Request{})
		Expect(err).ToNot(HaveOccurred())
		Expect(result.RequeueAfter).To(BeNumerically(">", 0))
		Consistently(received).WithTimeout(2 * time.Second).ShouldNot(Receive())
	})
})
