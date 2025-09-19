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
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Volume Snapshot validation", func() {
	It("Complains with warnings when the labels are missing", func() {
		snapshot := volumesnapshotv1.VolumeSnapshot{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{},
			},
		}

		var status ValidationStatus
		status.validateVolumeMetadata("pgdata", &snapshot.ObjectMeta, NewPgDataCalculator())
		Expect(status).To(Equal(ValidationStatus{
			Warnings: []ValidationMessage{
				{
					ObjectName: "pgdata",
					Message:    "Empty PVC role annotation",
				},
				{
					ObjectName: "pgdata",
					Message:    "Empty backup name label",
				},
			},
		}))
		Expect(status.ContainsErrors()).To(BeFalse())
		Expect(status.ContainsWarnings()).To(BeTrue())
	})

	It("Fails when the snapshot doesn't exist", func() {
		var status ValidationStatus
		status.validateVolumeMetadata("pgdata", nil, NewPgDataCalculator())
		Expect(status).To(Equal(ValidationStatus{
			Errors: []ValidationMessage{
				{
					ObjectName: "pgdata",
					Message:    "the volume doesn't exist",
				},
			},
		}))
		Expect(status.ContainsErrors()).To(BeTrue())
	})

	It("Fails when the snapshot have the pvcRole annotation, but the value is not correct", func() {
		snapshot := volumesnapshotv1.VolumeSnapshot{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					utils.PvcRoleLabelName: "test",
				},
			},
		}

		var status ValidationStatus
		status.validateVolumeMetadata("pgdata", &snapshot.ObjectMeta, NewPgDataCalculator())
		Expect(status).To(Equal(ValidationStatus{
			Errors: []ValidationMessage{
				{
					ObjectName: "pgdata",
					Message:    "Expected role 'PG_DATA', found 'test'",
				},
			},
			Warnings: []ValidationMessage{
				{
					ObjectName: "pgdata",
					Message:    "Empty backup name label",
				},
			},
		}))
		Expect(status.ContainsErrors()).To(BeTrue())
	})

	It("Verifies the coherence of multiple volumeSnapshot backups", func(ctx SpecContext) {
		snapshots := volumesnapshotv1.VolumeSnapshotList{
			Items: []volumesnapshotv1.VolumeSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pgdata",
						Namespace: "default",
						Labels: map[string]string{
							utils.BackupNameLabelName: "backup-one",
						},
						Annotations: map[string]string{
							utils.PvcRoleLabelName: string(utils.PVCRolePgData),
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pgwal",
						Namespace: "default",
						Labels: map[string]string{
							utils.BackupNameLabelName: "backup-two",
						},
						Annotations: map[string]string{
							utils.PvcRoleLabelName: string(utils.PVCRolePgWal),
						},
					},
				},
			},
		}
		dataSource := apiv1.DataSource{
			Storage: corev1.TypedLocalObjectReference{
				APIGroup: ptr.To(volumesnapshotv1.GroupName),
				Kind:     "VolumeSnapshot",
				Name:     "pgdata",
			},
			WalStorage: &corev1.TypedLocalObjectReference{
				APIGroup: ptr.To(volumesnapshotv1.GroupName),
				Kind:     "VolumeSnapshot",
				Name:     "pgwal",
			},
		}
		mockClient := fake.NewClientBuilder().
			WithScheme(scheme.BuildWithAllKnownScheme()).
			WithLists(&snapshots).
			Build()

		status, err := VerifyDataSourceCoherence(ctx, mockClient, "default", &dataSource)
		Expect(err).ToNot(HaveOccurred())
		Expect(status).To(Equal(ValidationStatus{
			Errors: []ValidationMessage{
				{
					ObjectName: "pgdata",
					Message:    "Non coherent backup names: 'backup-one' and 'backup-two'",
				},
			},
		}))
	})

	It("doesn't complain if the snapshots are correct", func(ctx SpecContext) {
		snapshots := volumesnapshotv1.VolumeSnapshotList{
			Items: []volumesnapshotv1.VolumeSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pgdata",
						Namespace: "default",
						Labels: map[string]string{
							utils.BackupNameLabelName: "backup-one",
						},
						Annotations: map[string]string{
							utils.PvcRoleLabelName: string(utils.PVCRolePgData),
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pgwal",
						Namespace: "default",
						Labels: map[string]string{
							utils.BackupNameLabelName: "backup-one",
						},
						Annotations: map[string]string{
							utils.PvcRoleLabelName: string(utils.PVCRolePgWal),
						},
					},
				},
			},
		}
		dataSource := apiv1.DataSource{
			Storage: corev1.TypedLocalObjectReference{
				APIGroup: ptr.To(volumesnapshotv1.GroupName),
				Kind:     "VolumeSnapshot",
				Name:     "pgdata",
			},
			WalStorage: &corev1.TypedLocalObjectReference{
				APIGroup: ptr.To(volumesnapshotv1.GroupName),
				Kind:     "VolumeSnapshot",
				Name:     "pgwal",
			},
		}
		mockClient := fake.NewClientBuilder().
			WithScheme(scheme.BuildWithAllKnownScheme()).
			WithLists(&snapshots).
			Build()

		status, err := VerifyDataSourceCoherence(ctx, mockClient, "default", &dataSource)
		Expect(err).ToNot(HaveOccurred())
		Expect(status.ContainsErrors()).To(BeFalse())
	})

	It("doesn't complain if we only have the pgdata snapshot", func(ctx SpecContext) {
		snapshots := volumesnapshotv1.VolumeSnapshotList{
			Items: []volumesnapshotv1.VolumeSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pgdata",
						Namespace: "default",
						Labels: map[string]string{
							utils.BackupNameLabelName: "backup-one",
						},
						Annotations: map[string]string{
							utils.PvcRoleLabelName: string(utils.PVCRolePgData),
						},
					},
				},
			},
		}
		dataSource := apiv1.DataSource{
			Storage: corev1.TypedLocalObjectReference{
				APIGroup: ptr.To(volumesnapshotv1.GroupName),
				Kind:     "VolumeSnapshot",
				Name:     "pgdata",
			},
		}
		mockClient := fake.NewClientBuilder().
			WithScheme(scheme.BuildWithAllKnownScheme()).
			WithLists(&snapshots).
			Build()

		status, err := VerifyDataSourceCoherence(ctx, mockClient, "default", &dataSource)
		Expect(err).ToNot(HaveOccurred())
		Expect(status.ContainsErrors()).To(BeFalse())
	})

	It("complains if we referenced a snapshot which we don't have", func(ctx SpecContext) {
		snapshots := volumesnapshotv1.VolumeSnapshotList{
			Items: []volumesnapshotv1.VolumeSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pgdata",
						Namespace: "default",
						Labels: map[string]string{
							utils.BackupNameLabelName: "backup-one",
						},
						Annotations: map[string]string{
							utils.PvcRoleLabelName: string(utils.PVCRolePgData),
						},
					},
				},
			},
		}
		dataSource := apiv1.DataSource{
			Storage: corev1.TypedLocalObjectReference{
				APIGroup: ptr.To(volumesnapshotv1.GroupName),
				Kind:     "VolumeSnapshot",
				Name:     "pgdata",
			},
			WalStorage: &corev1.TypedLocalObjectReference{
				APIGroup: ptr.To(volumesnapshotv1.GroupName),
				Kind:     "VolumeSnapshot",
				Name:     "pgwal",
			},
		}
		mockClient := fake.NewClientBuilder().
			WithScheme(scheme.BuildWithAllKnownScheme()).
			WithLists(&snapshots).
			Build()

		status, err := VerifyDataSourceCoherence(ctx, mockClient, "default", &dataSource)
		Expect(err).ToNot(HaveOccurred())
		Expect(status.ContainsErrors()).To(BeTrue())
		Expect(status).To(Equal(ValidationStatus{
			Errors: []ValidationMessage{
				{
					ObjectName: "pgwal",
					Message:    "the volume doesn't exist",
				},
			},
		}))
	})
})
