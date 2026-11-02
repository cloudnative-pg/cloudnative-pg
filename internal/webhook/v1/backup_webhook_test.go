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

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Backup webhook validate", func() {
	var v *BackupCustomValidator
	BeforeEach(func() {
		v = &BackupCustomValidator{}
	})

	It("doesn't complain if VolumeSnapshot CRD is present", func() {
		backup := &apiv1.Backup{
			Spec: apiv1.BackupSpec{
				Method: apiv1.BackupMethodVolumeSnapshot,
			},
		}
		utils.SetVolumeSnapshot(true)
		result := v.validate(backup)
		Expect(result).To(BeEmpty())
	})

	It("complains if VolumeSnapshot CRD is not present", func() {
		backup := &apiv1.Backup{
			Spec: apiv1.BackupSpec{
				Method: apiv1.BackupMethodVolumeSnapshot,
			},
		}
		utils.SetVolumeSnapshot(false)
		result := v.validate(backup)
		Expect(result).To(HaveLen(1))
		Expect(result[0].Field).To(Equal("spec.method"))
	})

	It("complains if online is set on a barman backup", func() {
		backup := &apiv1.Backup{
			Spec: apiv1.BackupSpec{
				Method: apiv1.BackupMethodBarmanObjectStore,
				Online: ptr.To(true),
			},
		}
		result := v.validate(backup)
		Expect(result).To(HaveLen(1))
		Expect(result[0].Field).To(Equal("spec.online"))
	})

	It("complains if onlineConfiguration is set on a barman backup", func() {
		backup := &apiv1.Backup{
			Spec: apiv1.BackupSpec{
				Method:              apiv1.BackupMethodBarmanObjectStore,
				OnlineConfiguration: &apiv1.OnlineConfiguration{},
			},
		}
		result := v.validate(backup)
		Expect(result).To(HaveLen(1))
		Expect(result[0].Field).To(Equal("spec.onlineConfiguration"))
	})

	It("returns error if BackupVolumeSnapshotDeadlineAnnotationName is not an integer", func() {
		backup := &apiv1.Backup{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					utils.BackupVolumeSnapshotDeadlineAnnotationName: "not-an-integer",
				},
			},
		}
		result := v.validate(backup)
		Expect(result).To(HaveLen(1))
		Expect(result[0].Field).To(Equal("metadata.annotations." + utils.BackupVolumeSnapshotDeadlineAnnotationName))
		Expect(result[0].Error()).To(ContainSubstring("must be an integer"))
	})

	It("does not return error if BackupVolumeSnapshotDeadlineAnnotationName is an integer", func() {
		backup := &apiv1.Backup{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					utils.BackupVolumeSnapshotDeadlineAnnotationName: "123",
				},
			},
		}
		result := v.validate(backup)
		Expect(result).To(BeEmpty())
	})

	It("does not return error if BackupVolumeSnapshotDeadlineAnnotationName is not set", func() {
		backup := &apiv1.Backup{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{},
			},
		}
		result := v.validate(backup)
		Expect(result).To(BeEmpty())
	})
})

var _ = Describe("Backup in-tree Barman Cloud warnings", func() {
	v := &BackupCustomValidator{}

	DescribeTable("on create and update",
		func(ctx SpecContext, spec apiv1.BackupSpec, warned bool) {
			backup := &apiv1.Backup{Spec: spec}
			utils.SetVolumeSnapshot(true)

			createWarnings, err := v.ValidateCreate(ctx, backup)
			Expect(err).ToNot(HaveOccurred())
			updateWarnings, err := v.ValidateUpdate(ctx, backup, backup)
			Expect(err).ToNot(HaveOccurred())

			if warned {
				Expect(createWarnings).To(HaveLen(1))
				Expect(createWarnings[0]).To(ContainSubstring("removed in CloudNativePG 1.32.0"))
				Expect(updateWarnings).To(Equal(createWarnings))
			} else {
				Expect(createWarnings).To(BeEmpty())
				Expect(updateWarnings).To(BeEmpty())
			}
		},
		Entry("barmanObjectStore",
			apiv1.BackupSpec{Method: apiv1.BackupMethodBarmanObjectStore}, true),
		Entry("volumeSnapshot",
			apiv1.BackupSpec{Method: apiv1.BackupMethodVolumeSnapshot}, false),
		Entry("plugin",
			apiv1.BackupSpec{
				Method:              apiv1.BackupMethodPlugin,
				PluginConfiguration: &apiv1.BackupPluginConfiguration{Name: "barman-cloud.cloudnative-pg.io"},
			}, false),
	)

	It("returns the warning together with a validation error", func(ctx SpecContext) {
		backup := &apiv1.Backup{Spec: apiv1.BackupSpec{
			Method: apiv1.BackupMethodBarmanObjectStore,
			Online: ptr.To(true),
		}}

		createWarnings, err := v.ValidateCreate(ctx, backup)
		Expect(err).To(HaveOccurred())
		Expect(createWarnings).To(HaveLen(1))
		Expect(createWarnings[0]).To(ContainSubstring("removed in CloudNativePG 1.32.0"))

		updateWarnings, err := v.ValidateUpdate(ctx, backup, backup)
		Expect(err).To(HaveOccurred())
		Expect(updateWarnings).To(Equal(createWarnings))
	})
})
