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

package volumesnapshot

import (
	"errors"

	volumesnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("parseVolumeSnapshotInfo", func() {
	It("should not fail when the VolumeSnapshot CR have not been handled by the External Snapshotter operator", func() {
		info := parseVolumeSnapshotInfo(&volumesnapshotv1.VolumeSnapshot{})
		Expect(info).To(BeEquivalentTo(volumeSnapshotInfo{
			error:       nil,
			provisioned: false,
			ready:       false,
		}))
	})

	It("should gracefully handle snapshot errors", func() {
		volumeSnapshot := &volumesnapshotv1.VolumeSnapshot{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "snapshot",
				Namespace: "default",
			},
			Status: &volumesnapshotv1.VolumeSnapshotStatus{
				Error: &volumesnapshotv1.VolumeSnapshotError{
					Time:    ptr.To(metav1.Now()),
					Message: nil,
				},
			},
		}
		info := parseVolumeSnapshotInfo(volumeSnapshot)

		Expect(info.error).To(HaveOccurred())
		Expect(info.ready).To(BeFalse())
		Expect(info.provisioned).To(BeFalse())

		var err *volumeSnapshotError
		Expect(errors.As(info.error, &err)).To(BeTrue())
		Expect(err.InternalError).To(BeEquivalentTo(*volumeSnapshot.Status.Error))
		Expect(err.Name).To(BeEquivalentTo("snapshot"))
		Expect(err.Namespace).To(BeEquivalentTo("default"))
		Expect(err.isRetryable()).To(BeFalse())
	})

	It("should detect retryable errors", func() {
		volumeSnapshot := &volumesnapshotv1.VolumeSnapshot{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "snapshot",
				Namespace: "default",
			},
			Status: &volumesnapshotv1.VolumeSnapshotStatus{
				Error: &volumesnapshotv1.VolumeSnapshotError{
					Time: ptr.To(metav1.Now()),
					Message: ptr.To(
						"the object has been modified; please apply your changes to the latest version and try again"),
				},
			},
		}
		info := parseVolumeSnapshotInfo(volumeSnapshot)

		Expect(info.error).To(HaveOccurred())
		Expect(info.error.isRetryable()).To(BeTrue())
		Expect(info.ready).To(BeFalse())
		Expect(info.provisioned).To(BeFalse())

		var err *volumeSnapshotError
		Expect(errors.As(info.error, &err)).To(BeTrue())
		Expect(err.InternalError).To(BeEquivalentTo(*volumeSnapshot.Status.Error))
		Expect(err.Name).To(BeEquivalentTo("snapshot"))
		Expect(err.Namespace).To(BeEquivalentTo("default"))
	})

	When("BoundVolumeSnapshotContentName is nil", func() {
		It("should detect that a VolumeSnapshot is not provisioned", func() {
			volumeSnapshot := &volumesnapshotv1.VolumeSnapshot{
				Status: &volumesnapshotv1.VolumeSnapshotStatus{
					Error:                          nil,
					BoundVolumeSnapshotContentName: nil,
				},
			}
			info := parseVolumeSnapshotInfo(volumeSnapshot)
			Expect(info.provisioned).To(BeFalse())
		})
	})

	When("BoundVolumeSnapshotContentName is not nil", func() {
		It("should detect that a VolumeSnapshot is not provisioned", func() {
			volumeSnapshot := &volumesnapshotv1.VolumeSnapshot{
				Status: &volumesnapshotv1.VolumeSnapshotStatus{
					ReadyToUse:                     ptr.To(false),
					Error:                          nil,
					BoundVolumeSnapshotContentName: ptr.To(""),
				},
			}
			info := parseVolumeSnapshotInfo(volumeSnapshot)
			Expect(info.provisioned).To(BeFalse())
			Expect(info.ready).To(BeFalse())
		})

		It("should detect that a VolumeSnapshot is not provisioned", func() {
			volumeSnapshot := &volumesnapshotv1.VolumeSnapshot{
				Status: &volumesnapshotv1.VolumeSnapshotStatus{
					ReadyToUse:                     ptr.To(false),
					Error:                          nil,
					BoundVolumeSnapshotContentName: ptr.To("content-name"),
				},
			}
			info := parseVolumeSnapshotInfo(volumeSnapshot)
			Expect(info.provisioned).To(BeFalse())
			Expect(info.ready).To(BeFalse())
		})

		It("should detect that a VolumeSnapshot is provisioned", func() {
			volumeSnapshot := &volumesnapshotv1.VolumeSnapshot{
				Status: &volumesnapshotv1.VolumeSnapshotStatus{
					ReadyToUse:                     ptr.To(false),
					Error:                          nil,
					BoundVolumeSnapshotContentName: ptr.To("content-name"),
					CreationTime:                   ptr.To(metav1.Now()),
				},
			}
			info := parseVolumeSnapshotInfo(volumeSnapshot)
			Expect(info.provisioned).To(BeTrue())
			Expect(info.ready).To(BeFalse())
		})
	})

	When("ReadyToUse is nil", func() {
		It("should detect that a VolumeSnapshot is not ready to use", func() {
			volumeSnapshot := &volumesnapshotv1.VolumeSnapshot{
				Status: &volumesnapshotv1.VolumeSnapshotStatus{
					ReadyToUse:                     nil,
					Error:                          nil,
					BoundVolumeSnapshotContentName: ptr.To("content-name"),
					CreationTime:                   ptr.To(metav1.Now()),
				},
			}
			info := parseVolumeSnapshotInfo(volumeSnapshot)
			Expect(info.provisioned).To(BeTrue())
			Expect(info.ready).To(BeFalse())
		})
	})

	When("ReadyToUse is not nil", func() {
		It("should detect that a VolumeSnapshot is not ready to use", func() {
			volumeSnapshot := &volumesnapshotv1.VolumeSnapshot{
				Status: &volumesnapshotv1.VolumeSnapshotStatus{
					ReadyToUse:                     ptr.To(false),
					Error:                          nil,
					BoundVolumeSnapshotContentName: ptr.To("content-name"),
					CreationTime:                   ptr.To(metav1.Now()),
				},
			}
			info := parseVolumeSnapshotInfo(volumeSnapshot)
			Expect(info.provisioned).To(BeTrue())
			Expect(info.ready).To(BeFalse())
		})

		It("should detect that a VolumeSnapshot is ready to use", func() {
			volumeSnapshot := &volumesnapshotv1.VolumeSnapshot{
				Status: &volumesnapshotv1.VolumeSnapshotStatus{
					ReadyToUse:                     ptr.To(true),
					Error:                          nil,
					BoundVolumeSnapshotContentName: ptr.To("content-name"),
					CreationTime:                   ptr.To(metav1.Now()),
				},
			}
			info := parseVolumeSnapshotInfo(volumeSnapshot)
			Expect(info.provisioned).To(BeTrue())
			Expect(info.ready).To(BeTrue())
		})
	})
})
