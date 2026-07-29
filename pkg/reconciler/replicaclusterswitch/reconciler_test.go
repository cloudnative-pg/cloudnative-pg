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

package replicaclusterswitch

import (
	"context"
	"errors"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/webserver/client/remote"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/replicaclusterswitch/conditions"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// fakeControlData describes a cleanly shut down primary; its REDO WAL file is
// referenced by archivedWALName below.
const fakeControlData = `pg_control version number:               1002
Catalog version number:                  202201241
Database cluster state:                  shut down
Database system identifier:              12345678901234567890123456789012
Latest checkpoint's TimeLineID:       3
Latest checkpoint location:              0/3000FF0
Latest checkpoint's REDO location:         0/3000CC0
Latest checkpoint's REDO WAL file:         000000010000000000000003
`

const archivedWALName = "000000010000000000000003"

// instanceClientMock is a minimal remote.InstanceClient implementation that
// returns canned pg_controldata and partial-WAL-archive responses. The other
// InstanceClient methods are inherited from the embedded nil interface and
// panic if called.
type instanceClientMock struct {
	remote.InstanceClient
	controlData  string
	archivedWAL  string
	archiveCalls int
	controlErr   error
}

func (m *instanceClientMock) GetPgControlDataFromInstance(
	_ context.Context,
	_ *corev1.Pod,
) (string, error) {
	return m.controlData, m.controlErr
}

func (m *instanceClientMock) ArchivePartialWAL(_ context.Context, _ *corev1.Pod) (string, error) {
	m.archiveCalls++
	return m.archivedWAL, nil
}

var _ = Describe("reconcileDemotionToken", func() {
	const primaryPodName = "cluster-a-1"

	// expectedToken is the token generateDemotionToken computes from fakeControlData.
	var expectedToken string
	BeforeEach(func() {
		var err error
		expectedToken, err = utils.ParsePgControldataOutput(fakeControlData).CreatePromotionToken()
		Expect(err).ToNot(HaveOccurred())
		Expect(expectedToken).ToNot(BeEmpty())
	})

	buildCluster := func(storedToken string) *apiv1.Cluster {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster-a"},
			Spec: apiv1.ClusterSpec{
				ReplicaCluster: &apiv1.ReplicaClusterConfiguration{
					Primary: "cluster-b",
					Source:  "cluster-b",
				},
			},
			Status: apiv1.ClusterStatus{
				CurrentPrimary: primaryPodName,
				DemotionToken:  storedToken,
			},
		}
		return cluster
	}

	instancesStatus := postgres.PostgresqlStatusList{
		Items: []postgres.PostgresqlStatus{
			{
				Pod: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: primaryPodName}},
			},
		},
	}

	It("populates the demotion token when it is not set yet", func(ctx SpecContext) {
		cluster := buildCluster("")
		cli := fake.NewClientBuilder().
			WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
			WithObjects(cluster).
			WithStatusSubresource(cluster).
			Build()
		instanceClient := &instanceClientMock{controlData: fakeControlData, archivedWAL: archivedWALName}

		_, err := reconcileDemotionToken(ctx, cli, cluster, instanceClient, instancesStatus)
		Expect(err).ToNot(HaveOccurred())
		Expect(cluster.Status.DemotionToken).To(Equal(expectedToken))
		// A fresh token triggers exactly one partial WAL archive.
		Expect(instanceClient.archiveCalls).To(Equal(1))
	})

	// Regression for #11074: when generateDemotionToken short-circuits with an
	// empty "no change" token, reconcileDemotionToken must not overwrite the
	// already-stored token. This happens whenever the reconcile is requeued
	// after the token was first written but before the transition metadata is
	// cleaned up (e.g. a failing webhook call on the cleanup patch).
	It("does not wipe an already-set demotion token on a no-change reconcile", func(ctx SpecContext) {
		cluster := buildCluster(expectedToken)
		cli := fake.NewClientBuilder().
			WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
			WithObjects(cluster).
			WithStatusSubresource(cluster).
			Build()
		instanceClient := &instanceClientMock{controlData: fakeControlData, archivedWAL: archivedWALName}

		_, err := reconcileDemotionToken(ctx, cli, cluster, instanceClient, instancesStatus)
		Expect(err).ToNot(HaveOccurred())
		Expect(cluster.Status.DemotionToken).To(Equal(expectedToken))
		// The partial WAL is archived only when a fresh token is generated.
		Expect(instanceClient.archiveCalls).To(BeZero())
	})
})

var _ = Describe("Reconcile of a replica cluster whose primary is silent", func() {
	const primaryPodName = "cluster-a-1"

	// buildCluster returns a cluster that has just been switched to replica
	// cluster mode: no transition condition is set yet, and the last known
	// state of its primary is whatever the caller passes.
	buildCluster := func(lastKnown map[apiv1.PodName]apiv1.InstanceReportedState) *apiv1.Cluster {
		return &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster-a", Namespace: "default"},
			Spec: apiv1.ClusterSpec{
				Instances: 1,
				ReplicaCluster: &apiv1.ReplicaClusterConfiguration{
					Primary: "cluster-b",
					Source:  "cluster-b",
				},
			},
			Status: apiv1.ClusterStatus{
				CurrentPrimary:         primaryPodName,
				TargetPrimary:          primaryPodName,
				InstancesReportedState: lastKnown,
			},
		}
	}

	lastKnownPrimary := map[apiv1.PodName]apiv1.InstanceReportedState{
		primaryPodName: {IsPrimary: true, TimeLineID: 1, IP: "10.0.0.1"},
	}

	// silentPrimary is the status list of a pod that is still probed, still
	// kubelet-ready, but whose /pg/status call failed: every observed field,
	// IsPrimary included, is left at its zero value.
	silentPrimary := postgres.PostgresqlStatusList{
		Items: []postgres.PostgresqlStatus{
			{
				Pod:        &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: primaryPodName}},
				IsPodReady: true,
				Error:      errors.New("connection refused"),
			},
		},
	}

	buildClient := func(cluster *apiv1.Cluster) client.Client {
		return fake.NewClientBuilder().
			WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
			WithObjects(cluster).
			WithStatusSubresource(cluster).
			Build()
	}

	// expectTransitionStarted asserts on the persisted object, since fencing
	// and the conditions are written through the client.
	expectTransitionStarted := func(ctx context.Context, cli client.Client, started bool) {
		var persisted apiv1.Cluster
		Expect(cli.Get(ctx, client.ObjectKey{Name: "cluster-a", Namespace: "default"}, &persisted)).To(Succeed())
		Expect(persisted.IsInstanceFenced(primaryPodName)).To(Equal(started))
		Expect(conditions.IsDesignatedPrimaryTransitionRequested(&persisted)).To(Equal(started))
	}

	It("starts the transition on the last known state of a silent primary", func(ctx SpecContext) {
		cluster := buildCluster(lastKnownPrimary)
		cli := buildClient(cluster)

		res, err := Reconcile(ctx, cli, cluster, nil, silentPrimary)
		Expect(err).ToNot(HaveOccurred())
		Expect(res).ToNot(BeNil())
		expectTransitionStarted(ctx, cli, true)
	})

	It("keeps starting the transition for a primary that does report", func(ctx SpecContext) {
		cluster := buildCluster(lastKnownPrimary)
		cli := buildClient(cluster)
		reporting := postgres.PostgresqlStatusList{
			Items: []postgres.PostgresqlStatus{
				{
					Pod:       &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: primaryPodName}},
					IsPrimary: true,
				},
			},
		}

		res, err := Reconcile(ctx, cli, cluster, nil, reporting)
		Expect(err).ToNot(HaveOccurred())
		Expect(res).ToNot(BeNil())
		expectTransitionStarted(ctx, cli, true)
	})

	It("does nothing when the silent instance was never observed as a primary", func(ctx SpecContext) {
		// This is a cluster created as a replica cluster: its designated
		// primary runs with a standby.signal file, so it is always observed as
		// a replica. A failing probe must not be read as a primary to demote.
		cluster := buildCluster(map[apiv1.PodName]apiv1.InstanceReportedState{
			primaryPodName: {IsPrimary: false, TimeLineID: 1, IP: "10.0.0.1"},
		})
		cli := buildClient(cluster)

		res, err := Reconcile(ctx, cli, cluster, nil, silentPrimary)
		Expect(err).ToNot(HaveOccurred())
		Expect(res).To(BeNil())
		expectTransitionStarted(ctx, cli, false)
	})

	It("does nothing when the silent instance has no last known state at all", func(ctx SpecContext) {
		cluster := buildCluster(nil)
		cli := buildClient(cluster)

		res, err := Reconcile(ctx, cli, cluster, nil, silentPrimary)
		Expect(err).ToNot(HaveOccurred())
		Expect(res).To(BeNil())
		expectTransitionStarted(ctx, cli, false)
	})

	It("does not start a second transition after the cluster has been demoted", func(ctx SpecContext) {
		// A completed transition leaves a demotion token behind and removes its
		// conditions, so a probe failing right afterwards must not fence the
		// whole cluster again: the last known state still says primary until
		// the demoted instance reports as a replica.
		cluster := buildCluster(lastKnownPrimary)
		cluster.Status.DemotionToken = "a-previously-generated-token"
		cli := buildClient(cluster)

		res, err := Reconcile(ctx, cli, cluster, nil, silentPrimary)
		Expect(err).ToNot(HaveOccurred())
		Expect(res).To(BeNil())
		expectTransitionStarted(ctx, cli, false)
	})

	It("does nothing when the last known primary is no longer among the probed instances", func(ctx SpecContext) {
		// The pod is being deleted or recreated, so it is filtered out of the
		// status list. There is no PostgreSQL left to stop by fencing it.
		cluster := buildCluster(lastKnownPrimary)
		cli := buildClient(cluster)
		otherInstanceOnly := postgres.PostgresqlStatusList{
			Items: []postgres.PostgresqlStatus{
				{
					Pod:        &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "cluster-a-2"}},
					IsPodReady: true,
					Error:      errors.New("connection refused"),
				},
			},
		}

		res, err := Reconcile(ctx, cli, cluster, nil, otherInstanceOnly)
		Expect(err).ToNot(HaveOccurred())
		Expect(res).To(BeNil())
		expectTransitionStarted(ctx, cli, false)
	})
})
