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
	corev1 "k8s.io/api/core/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("pvc role test", func() {
	It("return expected value for pgData", func() {
		instanceName := "instance1"
		backupName := "backup1"
		expectedLabel := map[string]string{
			utils.PvcRoleLabelName:      string(utils.PVCRolePgData),
			utils.InstanceNameLabelName: instanceName,
		}
		cluster := apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				StorageConfiguration: apiv1.StorageConfiguration{
					Size: "5Gi",
				},
			},
		}

		dataSource := corev1.TypedLocalObjectReference{
			Name: "test",
		}
		storageSource := StorageSource{
			DataSource: dataSource,
		}

		role := NewPgDataCalculator()
		Expect(role.GetRoleName()).To(BeEquivalentTo(utils.PVCRolePgData))
		Expect(role.GetName(instanceName)).To(BeIdenticalTo(instanceName))
		Expect(role.GetLabels(instanceName)).To(BeEquivalentTo(expectedLabel))
		Expect(role.GetInitialStatus()).To(BeIdenticalTo(StatusInitializing))
		Expect(role.GetSnapshotName(backupName)).To(BeIdenticalTo(backupName))

		Expect(role.GetStorageConfiguration(&cluster)).To(BeEquivalentTo(
			apiv1.StorageConfiguration{
				Size: "5Gi",
			}))
		Expect(role.GetSource(nil)).To(BeNil())
		Expect(role.GetSource(&storageSource)).To(BeEquivalentTo(&dataSource))
	})

	It("return expected value for pgWal", func() {
		instanceName := "instance1"
		backupName := "backup1"
		expectedLabel := map[string]string{
			utils.PvcRoleLabelName:      string(utils.PVCRolePgWal),
			utils.InstanceNameLabelName: instanceName,
		}
		cluster := apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				WalStorage: &apiv1.StorageConfiguration{
					Size: "5Gi",
				},
			},
		}

		walSource := corev1.TypedLocalObjectReference{
			Name: "test",
		}
		storageSource := StorageSource{
			WALSource: &walSource,
		}

		role := NewPgWalCalculator()
		Expect(role.GetRoleName()).To(BeEquivalentTo(utils.PVCRolePgWal))
		Expect(role.GetName(instanceName)).To(BeIdenticalTo(instanceName + apiv1.WalArchiveVolumeSuffix))
		Expect(role.GetLabels(instanceName)).To(BeEquivalentTo(expectedLabel))
		Expect(role.GetInitialStatus()).To(BeIdenticalTo(StatusReady))
		Expect(role.GetSnapshotName(backupName)).To(BeIdenticalTo(backupName + "-wal"))

		Expect(role.GetStorageConfiguration(&cluster)).To(BeEquivalentTo(
			apiv1.StorageConfiguration{
				Size: "5Gi",
			}))
		Expect(role.GetSource(nil)).To(BeNil())
		Expect(role.GetSource(&StorageSource{})).Error().Should(HaveOccurred())
		Expect(role.GetSource(&storageSource)).To(BeEquivalentTo(&walSource))
	})

	It("return expected value for pgTablespace", func() {
		instanceName := "instance1"
		backupName := "backup1"
		tbsName := "tbs1"
		expectedLabel := map[string]string{
			utils.PvcRoleLabelName:        string(utils.PVCRolePgTablespace),
			utils.InstanceNameLabelName:   instanceName,
			utils.TablespaceNameLabelName: tbsName,
		}
		cluster := apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Tablespaces: []apiv1.TablespaceConfiguration{
					{
						Name: "tbs1",
						Storage: apiv1.StorageConfiguration{
							Size: "5Gi",
						},
					},
				},
			},
		}

		storageSource1 := StorageSource{
			TablespaceSource: map[string]corev1.TypedLocalObjectReference{
				"tbs1": {
					Name: "test",
				},
			},
		}
		storageSource2 := StorageSource{
			TablespaceSource: map[string]corev1.TypedLocalObjectReference{
				"tbs2": {
					Name: "test2",
				},
			},
		}

		role := NewPgTablespaceCalculator(tbsName)
		Expect(role.GetRoleName()).To(BeEquivalentTo(utils.PVCRolePgTablespace))
		Expect(role.GetName(instanceName)).To(BeIdenticalTo(instanceName + apiv1.TablespaceVolumeInfix + tbsName))
		Expect(role.GetLabels(instanceName)).To(BeEquivalentTo(expectedLabel))
		Expect(role.GetInitialStatus()).To(BeIdenticalTo(StatusReady))
		Expect(role.GetSnapshotName(backupName)).To(BeIdenticalTo(backupName + apiv1.TablespaceVolumeInfix + tbsName))

		Expect(role.GetStorageConfiguration(&cluster)).To(BeEquivalentTo(
			apiv1.StorageConfiguration{
				Size: "5Gi",
			}))
		Expect(role.GetSource(nil)).To(BeNil())
		Expect(role.GetSource(&storageSource2)).Error().Should(HaveOccurred())
		Expect(role.GetSource(&storageSource1)).To(BeEquivalentTo(&corev1.TypedLocalObjectReference{Name: "test"}))
	})
})
