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

package persistentvolumeclaim

import (
	volumesnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Restoring a snapshot taken without the WAL storage", func() {
	const (
		namespace    = "default"
		snapshotName = "backup-example"
	)
	var cluster *apiv1.Cluster

	pgDataSnapshot := func(walVolumeSnapshotSkipped bool) *volumesnapshotv1.VolumeSnapshot {
		snapshot := &volumesnapshotv1.VolumeSnapshot{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      snapshotName,
				Labels:    map[string]string{utils.BackupNameLabelName: snapshotName},
				Annotations: map[string]string{
					utils.PvcRoleLabelName: string(utils.PVCRolePgData),
				},
			},
		}
		if walVolumeSnapshotSkipped {
			snapshot.Annotations[utils.WalVolumeSnapshotSkippedAnnotationName] = "true"
		}
		return snapshot
	}

	// expectPVCs checks that PGDATA is restored from the snapshot, while the
	// WAL PVC is created empty
	expectPVCs := func(ctx SpecContext, c client.Client) {
		var pvcList corev1.PersistentVolumeClaimList
		Expect(c.List(ctx, &pvcList)).To(Succeed())
		Expect(pvcList.Items).To(HaveLen(2))
		for _, pvc := range pvcList.Items {
			switch utils.PVCRole(pvc.Labels[utils.PvcRoleLabelName]) {
			case utils.PVCRolePgData:
				Expect(pvc.Spec.DataSource).To(HaveValue(HaveField("Name", snapshotName)))
			case utils.PVCRolePgWal:
				Expect(pvc.Spec.DataSource).To(BeNil())
			default:
				Fail("unexpected PVC " + pvc.Name)
			}
		}
	}

	BeforeEach(func() {
		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "cluster-example"},
			Spec: apiv1.ClusterSpec{
				StorageConfiguration: apiv1.StorageConfiguration{Size: "1Gi"},
				WalStorage:           &apiv1.StorageConfiguration{Size: "1Gi"},
			},
		}
	})

	It("creates an empty WAL PVC for a Backup marked as taken without the WAL storage",
		func(ctx SpecContext) {
			backup := &apiv1.Backup{
				Spec: apiv1.BackupSpec{Method: apiv1.BackupMethodVolumeSnapshot},
				Status: apiv1.BackupStatus{
					Phase: apiv1.BackupPhaseCompleted,
					BackupSnapshotStatus: apiv1.BackupSnapshotStatus{
						Elements: []apiv1.BackupSnapshotElementStatus{
							{Name: snapshotName, Type: string(utils.PVCRolePgData)},
						},
						WalVolumeSnapshotSkipped: true,
					},
				},
			}
			c := fake.NewClientBuilder().WithScheme(scheme.BuildWithAllKnownScheme()).Build()

			source := GetCandidateStorageSourceForPrimary(cluster, backup)
			Expect(CreateInstancePVCs(ctx, c, cluster, source, 1)).To(Succeed())
			expectPVCs(ctx, c)
		})

	It("creates an empty WAL PVC when recovering from a PGDATA snapshot alone", func(ctx SpecContext) {
		cluster.Spec.Bootstrap = &apiv1.BootstrapConfiguration{
			Recovery: &apiv1.BootstrapRecovery{
				Source: "origin",
				VolumeSnapshots: &apiv1.DataSource{
					Storage: corev1.TypedLocalObjectReference{
						APIGroup: ptr.To(volumesnapshotv1.GroupName),
						Kind:     apiv1.VolumeSnapshotKind,
						Name:     snapshotName,
					},
				},
			},
		}
		c := fake.NewClientBuilder().WithScheme(scheme.BuildWithAllKnownScheme()).Build()

		source := GetCandidateStorageSourceForPrimary(cluster, nil)
		Expect(CreateInstancePVCs(ctx, c, cluster, source, 1)).To(Succeed())
		expectPVCs(ctx, c)
	})

	DescribeTable("verifies the WAL storage source is only omitted for a marked snapshot",
		func(ctx SpecContext, walVolumeSnapshotSkipped bool, expectedErrors int) {
			c := fake.NewClientBuilder().
				WithScheme(scheme.BuildWithAllKnownScheme()).
				WithObjects(pgDataSnapshot(walVolumeSnapshotSkipped)).
				Build()
			dataSource := apiv1.DataSource{
				Storage: corev1.TypedLocalObjectReference{
					APIGroup: ptr.To(volumesnapshotv1.GroupName),
					Kind:     apiv1.VolumeSnapshotKind,
					Name:     snapshotName,
				},
			}

			status, err := VerifyDataSourceCoherence(ctx, c, namespace, &dataSource, true)
			Expect(err).ToNot(HaveOccurred())
			Expect(status.Errors).To(HaveLen(expectedErrors))
		},
		Entry("accepting a snapshot taken without the WAL storage", true, 0),
		Entry("rejecting a snapshot taken with the WAL storage", false, 1),
	)
})
