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

package status

import (
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("getPrimaryPromotionTime", func() {
	var cluster *apiv1.Cluster

	Context("when CurrentPrimaryTimestamp is empty", func() {
		BeforeEach(func() {
			cluster = &apiv1.Cluster{
				Status: apiv1.ClusterStatus{
					CurrentPrimaryTimestamp: "",
				},
			}
		})

		It("should return an empty string", func() {
			Expect(getPrimaryPromotionTime(cluster)).To(Equal(""))
		})
	})

	Context("when CurrentPrimaryTimestamp is valid", func() {
		It("should return the formatted timestamp", func() {
			now := time.Now()
			uptime := 1 * time.Hour
			currentPrimaryTimestamp := now.Add(-uptime)

			cluster = &apiv1.Cluster{
				Status: apiv1.ClusterStatus{
					CurrentPrimaryTimestamp: currentPrimaryTimestamp.Format(metav1.RFC3339Micro),
				},
			}

			expected := fmt.Sprintf("%s (%s)", currentPrimaryTimestamp.Round(time.Second), uptime)
			Expect(getPrimaryPromotionTimeIdempotent(cluster, now)).To(Equal(expected))
		})
	})

	Context("when CurrentPrimaryTimestamp is invalid", func() {
		BeforeEach(func() {
			cluster = &apiv1.Cluster{
				Status: apiv1.ClusterStatus{
					CurrentPrimaryTimestamp: "invalid timestamp",
				},
			}
		})

		It("should return the error message", func() {
			Expect(getPrimaryPromotionTime(cluster)).To(ContainSubstring("error"))
		})
	})
})

var _ = Describe("getWalArchivingStatus", func() {
	It("should return 'Disabled' when WAL archiving is disabled", func() {
		result := getWalArchivingStatus(false, "", true)
		Expect(result).To(ContainSubstring("Disabled"))
	})

	It("should return 'OK' when archiving is working", func() {
		result := getWalArchivingStatus(true, "", false)
		Expect(result).To(ContainSubstring("OK"))
	})

	It("should return 'Failing' when there is a failed WAL", func() {
		result := getWalArchivingStatus(false, "000000010000000000000001", false)
		Expect(result).To(ContainSubstring("Failing"))
	})

	It("should return 'OK' even when there is a failed WAL if archiving is working", func() {
		result := getWalArchivingStatus(true, "000000010000000000000001", false)
		Expect(result).To(ContainSubstring("OK"))
	})

	It("should return 'Starting Up' when archiving hasn't started yet", func() {
		result := getWalArchivingStatus(false, "", false)
		Expect(result).To(ContainSubstring("Starting Up"))
	})

	It("should prioritize 'Disabled' over other statuses", func() {
		// Even if archiving is working, disabled should take precedence
		result := getWalArchivingStatus(true, "", true)
		Expect(result).To(ContainSubstring("Disabled"))
	})
})

var _ = Describe("hasContinuousBackupConfigured", func() {
	It("returns false when neither backup nor WAL archiver plugin is configured", func() {
		cluster := &apiv1.Cluster{}
		Expect(hasContinuousBackupConfigured(cluster)).To(BeFalse())
	})

	It("returns true when native .spec.backup is set", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Backup: &apiv1.BackupConfiguration{},
			},
		}
		Expect(hasContinuousBackupConfigured(cluster)).To(BeTrue())
	})

	It("returns true when the barman-cloud plugin is enabled", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Plugins: []apiv1.PluginConfiguration{
					{Name: "barman-cloud.cloudnative-pg.io"},
				},
			},
		}
		Expect(hasContinuousBackupConfigured(cluster)).To(BeTrue())
	})

	It("returns true when a third-party WAL archiver plugin is enabled", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Plugins: []apiv1.PluginConfiguration{
					{
						Name:          "wal-archiver.example.com",
						Enabled:       ptr.To(true),
						IsWALArchiver: ptr.To(true),
					},
				},
			},
		}
		Expect(hasContinuousBackupConfigured(cluster)).To(BeTrue())
		Expect(cluster.GetEnabledWALArchivePluginName()).To(Equal("wal-archiver.example.com"))
	})

	It("returns false when a plugin is present but is not a WAL archiver", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Plugins: []apiv1.PluginConfiguration{
					{
						Name:          "wal-archiver.example.com",
						Enabled:       ptr.To(true),
						IsWALArchiver: ptr.To(false),
					},
				},
			},
		}
		Expect(hasContinuousBackupConfigured(cluster)).To(BeFalse())
	})

	It("returns false for a nil cluster", func() {
		Expect(hasContinuousBackupConfigured(nil)).To(BeFalse())
	})
})

var _ = Describe("getPluginStatusByName", func() {
	It("returns the reported status for a matching plugin", func() {
		cluster := &apiv1.Cluster{
			Status: apiv1.ClusterStatus{
				PluginStatus: []apiv1.PluginStatus{
					{Name: "wal-archiver.example.com", Status: `{"ok":true}`},
				},
			},
		}
		Expect(getPluginStatusByName(cluster, "wal-archiver.example.com")).To(Equal(`{"ok":true}`))
	})

	It("returns an empty string when the plugin has no status", func() {
		cluster := &apiv1.Cluster{
			Status: apiv1.ClusterStatus{
				PluginStatus: []apiv1.PluginStatus{
					{Name: "wal-archiver.example.com"},
				},
			},
		}
		Expect(getPluginStatusByName(cluster, "wal-archiver.example.com")).To(Equal(""))
	})

	It("returns an empty string when the plugin is not present", func() {
		cluster := &apiv1.Cluster{}
		Expect(getPluginStatusByName(cluster, "missing")).To(Equal(""))
	})
})

var _ = Describe("isBarmanCloudPluginEnabled", func() {
	It("returns true and parameters when the plugin is enabled", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Plugins: []apiv1.PluginConfiguration{
					{
						Name: "barman-cloud.cloudnative-pg.io",
						Parameters: map[string]string{
							"barmanObjectName": "minio-store",
						},
					},
				},
			},
		}
		enabled, params := isBarmanCloudPluginEnabled(cluster)
		Expect(enabled).To(BeTrue())
		Expect(params["barmanObjectName"]).To(Equal("minio-store"))
	})

	It("returns false when the plugin is explicitly disabled", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Plugins: []apiv1.PluginConfiguration{
					{
						Name:    "barman-cloud.cloudnative-pg.io",
						Enabled: ptr.To(false),
					},
				},
			},
		}
		enabled, params := isBarmanCloudPluginEnabled(cluster)
		Expect(enabled).To(BeFalse())
		Expect(params).To(BeNil())
	})
})
