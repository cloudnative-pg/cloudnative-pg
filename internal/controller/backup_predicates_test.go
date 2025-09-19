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
	volumesnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("backup_controller volumeSnapshots predicates", func() {
	Context("volumeSnapshotHasBackuplabel and relative predicate", func() {
		It("returns false for a volumesnapshot without the backup label", func() {
			snapshot := volumesnapshotv1.VolumeSnapshot{
				ObjectMeta: metav1.ObjectMeta{},
			}

			Expect(volumeSnapshotHasBackuplabel(&snapshot)).To(BeFalse())
			Expect(volumeSnapshotsPredicate.Create(event.CreateEvent{
				Object: &snapshot,
			})).To(BeFalse())
			Expect(volumeSnapshotsPredicate.Delete(event.DeleteEvent{
				Object: &snapshot,
			})).To(BeFalse())
			Expect(volumeSnapshotsPredicate.Generic(event.GenericEvent{
				Object: &snapshot,
			})).To(BeFalse())
			Expect(volumeSnapshotsPredicate.Update(event.UpdateEvent{
				ObjectNew: &snapshot,
			})).To(BeFalse())
		})

		It("returns true for a volumesnapshot with the backup label", func() {
			snapshot := volumesnapshotv1.VolumeSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						utils.BackupNameLabelName: "test",
					},
				},
			}

			Expect(volumeSnapshotHasBackuplabel(&snapshot)).To(BeTrue())
			Expect(volumeSnapshotsPredicate.Create(event.CreateEvent{
				Object: &snapshot,
			})).To(BeTrue())
			Expect(volumeSnapshotsPredicate.Delete(event.DeleteEvent{
				Object: &snapshot,
			})).To(BeTrue())
			Expect(volumeSnapshotsPredicate.Generic(event.GenericEvent{
				Object: &snapshot,
			})).To(BeTrue())
			Expect(volumeSnapshotsPredicate.Update(event.UpdateEvent{
				ObjectNew: &snapshot,
			})).To(BeTrue())
		})
	})

	Context("volumeSnapshotHasBackuplabel and relative mappers", func() {
		It("correctly maps volume snapshots to backups", func(ctx SpecContext) {
			snapshot := volumesnapshotv1.VolumeSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "snapshot-1",
					Namespace: "default",
					Labels: map[string]string{
						utils.BackupNameLabelName: "backup",
					},
				},
			}

			var reconciler BackupReconciler
			requests := reconciler.mapVolumeSnapshotsToBackups()(ctx, &snapshot)
			Expect(requests).To(HaveLen(1))
			Expect(requests[0].Namespace).To(Equal("default"))
			Expect(requests[0].Name).To(Equal("backup"))
		})
	})
})
