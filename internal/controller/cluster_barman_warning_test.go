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
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("warnNativeBarmanCloudClusters", func() {
	var (
		cli      client.Client
		recorder *record.FakeRecorder
	)

	BeforeEach(func() {
		barmanObjectStore := &apiv1.BarmanObjectStoreConfiguration{DestinationPath: "s3://bucket/path"}
		cli = fake.NewClientBuilder().
			WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
			WithIndex(&apiv1.Cluster{}, nativeBarmanCloudClusterKey, nativeBarmanCloudIndexFunc).
			WithObjects(
				&apiv1.Cluster{
					ObjectMeta: metav1.ObjectMeta{Name: "backup", Namespace: "default"},
					Spec: apiv1.ClusterSpec{
						Backup: &apiv1.BackupConfiguration{BarmanObjectStore: barmanObjectStore},
					},
				},
				&apiv1.Cluster{
					ObjectMeta: metav1.ObjectMeta{Name: "external", Namespace: "default"},
					Spec: apiv1.ClusterSpec{
						ExternalClusters: []apiv1.ExternalCluster{{Name: "origin", BarmanObjectStore: barmanObjectStore}},
					},
				},
				&apiv1.Cluster{
					ObjectMeta: metav1.ObjectMeta{Name: "snapshot", Namespace: "default"},
					Spec: apiv1.ClusterSpec{
						Backup: &apiv1.BackupConfiguration{VolumeSnapshot: &apiv1.VolumeSnapshotConfiguration{}},
					},
				},
				&apiv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "plain", Namespace: "default"}},
			).
			Build()
		recorder = record.NewFakeRecorder(10)
	})

	It("paces an event for every cluster using the native Barman Cloud support", func(ctx SpecContext) {
		start := time.Now()
		warnNativeBarmanCloudClusters(ctx, cli, recorder)

		Expect(time.Since(start)).To(BeNumerically(">=", 2*nativeBarmanCloudEventInterval))
		Expect(recorder.Events).To(HaveLen(2))
		Expect(recorder.Events).To(Receive(HavePrefix("Warning NativeBarmanCloudDeprecated ")))
		Expect(recorder.Events).To(Receive(HavePrefix("Warning NativeBarmanCloudDeprecated ")))
	})

	It("stops when the context is cancelled", func(ctx SpecContext) {
		cancelledCtx, cancel := context.WithCancel(ctx)
		cancel()
		warnNativeBarmanCloudClusters(cancelledCtx, cli, recorder)

		Expect(recorder.Events).To(BeEmpty())
	})
})
