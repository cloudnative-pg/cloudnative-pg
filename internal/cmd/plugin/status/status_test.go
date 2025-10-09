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

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("getPrimaryStartTime", func() {
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
			Expect(getPrimaryStartTime(cluster)).To(Equal(""))
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

			expected := fmt.Sprintf("%s (uptime %s)", currentPrimaryTimestamp.Round(time.Second), uptime)
			Expect(getPrimaryStartTimeIdempotent(cluster, now)).To(Equal(expected))
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
			Expect(getPrimaryStartTime(cluster)).To(ContainSubstring("error"))
		})
	})
})

var _ = Describe("printBackupStatus", func() {
	var cluster *apiv1.Cluster

	BeforeEach(func() {
		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "default",
			},
		}
	})

	Context("when no backup is configured", func() {
		It("should identify no backup configuration", func() {
			hasLegacyBackup := cluster.Spec.Backup != nil
			hasPluginBackup := cluster.GetEnabledWALArchivePluginName() != ""

			Expect(hasLegacyBackup).To(BeFalse())
			Expect(hasPluginBackup).To(BeFalse())
		})
	})

	Context("when legacy backup is configured", func() {
		BeforeEach(func() {
			cluster.Spec.Backup = &apiv1.BackupConfiguration{}
		})

		It("should identify legacy backup configuration", func() {
			hasLegacyBackup := cluster.Spec.Backup != nil
			hasPluginBackup := cluster.GetEnabledWALArchivePluginName() != ""

			Expect(hasLegacyBackup).To(BeTrue())
			Expect(hasPluginBackup).To(BeFalse())
		})
	})

	Context("when plugin backup is configured", func() {
		BeforeEach(func() {
			enabled := true
			isWALArchiver := true
			cluster.Spec.Plugins = []apiv1.PluginConfiguration{
				{
					Name:          "barman-cloud",
					Enabled:       &enabled,
					IsWALArchiver: &isWALArchiver,
					Parameters: map[string]string{
						"provider": "s3",
					},
				},
			}
		})

		It("should identify plugin backup configuration", func() {
			hasLegacyBackup := cluster.Spec.Backup != nil
			hasPluginBackup := cluster.GetEnabledWALArchivePluginName() != ""

			Expect(hasLegacyBackup).To(BeFalse())
			Expect(hasPluginBackup).To(BeTrue())
			Expect(cluster.GetEnabledWALArchivePluginName()).To(Equal("barman-cloud"))
		})
	})

	Context("when both legacy and plugin backup are configured", func() {
		BeforeEach(func() {
			enabled := true
			isWALArchiver := true
			cluster.Spec.Backup = &apiv1.BackupConfiguration{}
			cluster.Spec.Plugins = []apiv1.PluginConfiguration{
				{
					Name:          "barman-cloud",
					Enabled:       &enabled,
					IsWALArchiver: &isWALArchiver,
				},
			}
		})

		It("should identify both configurations", func() {
			hasLegacyBackup := cluster.Spec.Backup != nil
			hasPluginBackup := cluster.GetEnabledWALArchivePluginName() != ""

			Expect(hasLegacyBackup).To(BeTrue())
			Expect(hasPluginBackup).To(BeTrue())
		})
	})
})
