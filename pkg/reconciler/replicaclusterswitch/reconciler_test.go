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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/webserver/client/remote"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
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
