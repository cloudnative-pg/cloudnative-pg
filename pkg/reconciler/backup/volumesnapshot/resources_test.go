/*
Copyright The CloudNativePG Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package volumesnapshot

import (
	"errors"

	storagesnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("parseVolumeSnapshotInfo", func() {
	It("should not fail when the VolumeSnapshot CR have not been handled by the External Snapshotter operator", func() {
		info := parseVolumeSnapshotInfo(&storagesnapshotv1.VolumeSnapshot{})
		Expect(info).To(BeEquivalentTo(volumeSnapshotInfo{
			error:       nil,
			provisioned: false,
			ready:       false,
			retryCount:  0,
		}))
	})

	It("should track retry count from annotations", func() {
		volumeSnapshot := &storagesnapshotv1.VolumeSnapshot{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					RetryCountAnnotation: "3",
				},
			},
		}
		info := parseVolumeSnapshotInfo(volumeSnapshot)
		Expect(info.retryCount).To(Equal(3))
	})

	It("should gracefully handle snapshot errors", func() {
		volumeSnapshot := &storagesnapshotv1.VolumeSnapshot{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "snapshot",
				Namespace: "default",
			},
			Status: &storagesnapshotv1.VolumeSnapshotStatus{
				Error: &storagesnapshotv1.VolumeSnapshotError{
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
		Expect(err.isRetryable(nil, 0)).To(BeFalse())
	})

	It("should detect retryable errors", func() {
		volumeSnapshot := &storagesnapshotv1.VolumeSnapshot{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "snapshot",
				Namespace: "default",
			},
			Status: &storagesnapshotv1.VolumeSnapshotStatus{
				Error: &storagesnapshotv1.VolumeSnapshotError{
					Time: ptr.To(metav1.Now()),
					Message: ptr.To(
						"the object has been modified; please apply your changes to the latest version and try again"),
				},
			},
		}
		info := parseVolumeSnapshotInfo(volumeSnapshot)

		Expect(info.error).To(HaveOccurred())
		Expect(info.error.isRetryable(nil, 0)).To(BeTrue())
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
			volumeSnapshot := &storagesnapshotv1.VolumeSnapshot{
				Status: &storagesnapshotv1.VolumeSnapshotStatus{
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
			volumeSnapshot := &storagesnapshotv1.VolumeSnapshot{
				Status: &storagesnapshotv1.VolumeSnapshotStatus{
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
			volumeSnapshot := &storagesnapshotv1.VolumeSnapshot{
				Status: &storagesnapshotv1.VolumeSnapshotStatus{
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
			volumeSnapshot := &storagesnapshotv1.VolumeSnapshot{
				Status: &storagesnapshotv1.VolumeSnapshotStatus{
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
			volumeSnapshot := &storagesnapshotv1.VolumeSnapshot{
				Status: &storagesnapshotv1.VolumeSnapshotStatus{
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
			volumeSnapshot := &storagesnapshotv1.VolumeSnapshot{
				Status: &storagesnapshotv1.VolumeSnapshotStatus{
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
			volumeSnapshot := &storagesnapshotv1.VolumeSnapshot{
				Status: &storagesnapshotv1.VolumeSnapshotStatus{
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

	_ = Describe("VolumeSnapshotRetryConfiguration", func() {
		var (
			retryConfig  *apiv1.VolumeSnapshotRetryConfiguration
			errorMsg     string
			vsError      *volumeSnapshotError
			creationTime metav1.Time
		)

		BeforeEach(func() {
			creationTime = metav1.Now()
			errorMsg = "context deadline exceeded"
			retryConfig = &apiv1.VolumeSnapshotRetryConfiguration{
				Deadline:   "5m",
				MaxRetries: 3,
			}

			vsError = &volumeSnapshotError{
				InternalError: storagesnapshotv1.VolumeSnapshotError{
					Message: &errorMsg,
					Time:    &creationTime,
				},
				Name:      "test-snapshot",
				Namespace: "test-namespace",
			}
		})

		Context("retry behavior with configuration", func() {
			It("allows retries when within deadline and under max retries", func() {
				isRetryable := vsError.isRetryable(retryConfig, 0)
				Expect(isRetryable).To(BeTrue())
			})

			It("doesn't retry when max retries is reached", func() {
				isRetryable := vsError.isRetryable(retryConfig, 3)
				Expect(isRetryable).To(BeFalse())
			})
		})

		Context("with default values", func() {
			It("retries when configuration is nil", func() {
				// If no config is specified, the error message is still checked
				errorMsg = "context deadline exceeded"
				vsError = &volumeSnapshotError{
					InternalError: storagesnapshotv1.VolumeSnapshotError{
						Message: &errorMsg,
					},
				}

				// Should retry based on the error message pattern
				isRetryable := vsError.isRetryable(nil, 0)
				Expect(isRetryable).To(BeTrue())

				// Change to a non-retryable message
				errorMsg = "access denied"
				nonRetryableError := &volumeSnapshotError{
					InternalError: storagesnapshotv1.VolumeSnapshotError{
						Message: &errorMsg,
					},
				}

				// Should not retry
				isRetryable = nonRetryableError.isRetryable(nil, 0)
				Expect(isRetryable).To(BeFalse())
			})
		})
	})
})
