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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PVCs used by instance", func() {
	clusterName := "cluster-pvc-instance"
	instanceName := clusterName + "-1"

	cluster := &apiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterName,
		},
		Spec: apiv1.ClusterSpec{
			WalStorage: &apiv1.StorageConfiguration{},
		},
	}

	It("true if the pvc belongs to the instance name", func() {
		res := BelongToInstance(cluster, instanceName, instanceName)
		Expect(res).To(BeTrue())

		res = BelongToInstance(cluster, instanceName, instanceName+"-wal")
		Expect(res).To(BeTrue())
	})

	It("fails when trying to get a pvc that doesn't belong to the instance", func() {
		res := BelongToInstance(cluster, instanceName, instanceName+"-nil")
		Expect(res).To(BeFalse())
	})
})

var _ = Describe("GetTerminatingInstancePVCName", func() {
	const clusterName = "cluster-terminating-pvc"
	instanceName := clusterName + "-1"

	cluster := &apiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: clusterName},
		Spec: apiv1.ClusterSpec{
			WalStorage: &apiv1.StorageConfiguration{},
		},
	}

	pvc := func(name string, terminating bool) corev1.PersistentVolumeClaim {
		p := corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: name}}
		if terminating {
			p.DeletionTimestamp = ptr.To(metav1.Now())
		}
		return p
	}

	It("returns empty for an empty list", func() {
		Expect(GetTerminatingInstancePVCName(cluster, instanceName, nil)).To(BeEmpty())
	})

	It("returns empty when no expected PVC is terminating", func() {
		pvcs := []corev1.PersistentVolumeClaim{
			pvc(instanceName, false),
			pvc(instanceName+"-wal", false),
		}
		Expect(GetTerminatingInstancePVCName(cluster, instanceName, pvcs)).To(BeEmpty())
	})

	It("returns the name of a terminating data PVC the instance expects", func() {
		pvcs := []corev1.PersistentVolumeClaim{pvc(instanceName, true)}
		Expect(GetTerminatingInstancePVCName(cluster, instanceName, pvcs)).To(Equal(instanceName))
	})

	It("returns the name of a terminating WAL PVC the instance expects", func() {
		pvcs := []corev1.PersistentVolumeClaim{pvc(instanceName+"-wal", true)}
		Expect(GetTerminatingInstancePVCName(cluster, instanceName, pvcs)).To(Equal(instanceName + "-wal"))
	})

	It("ignores a terminating PVC of a different instance", func() {
		pvcs := []corev1.PersistentVolumeClaim{pvc(clusterName+"-2-wal", true)}
		Expect(GetTerminatingInstancePVCName(cluster, instanceName, pvcs)).To(BeEmpty())
	})

	// A PVC the instance will NOT remount (e.g. a tablespace that has been
	// dropped from the spec) must not hold up recreation, even if terminating.
	It("ignores a terminating PVC the instance no longer expects", func() {
		pvcs := []corev1.PersistentVolumeClaim{pvc(instanceName+"-tbs-removed", true)}
		Expect(GetTerminatingInstancePVCName(cluster, instanceName, pvcs)).To(BeEmpty())
	})

	// The #10985 scenario: the data PVC has finished terminating and been
	// recreated fresh, while the WAL PVC of the same instance is still
	// terminating.
	It("detects a terminating WAL PVC even when the data PVC was recreated", func() {
		pvcs := []corev1.PersistentVolumeClaim{
			pvc(instanceName, false),
			pvc(instanceName+"-wal", true),
		}
		Expect(GetTerminatingInstancePVCName(cluster, instanceName, pvcs)).To(Equal(instanceName + "-wal"))
	})
})

var _ = Describe("instance with tablespace test", func() {
	clusterName := "cluster-tbs-pvc-instance"
	instanceName := clusterName + "-1"

	cluster := &apiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterName,
		},
		Spec: apiv1.ClusterSpec{
			StorageConfiguration: apiv1.StorageConfiguration{},
			WalStorage:           &apiv1.StorageConfiguration{},
			Tablespaces: []apiv1.TablespaceConfiguration{
				{
					Name: "tbs1",
					Storage: apiv1.StorageConfiguration{
						Size: "1Gi",
					},
				},
				{
					Name: "tbs2",
					Storage: apiv1.StorageConfiguration{
						Size: "1Gi",
					},
				},
				{
					Name: "tbs3",
					Storage: apiv1.StorageConfiguration{
						Size: "1Gi",
					},
				},
			},
		},
	}

	It("Get all the expected pvc out", func() {
		expectedPVCs := getExpectedPVCsFromCluster(cluster, instanceName)
		Expect(expectedPVCs).Should(HaveLen(5))
		for _, pvc := range expectedPVCs {
			Expect(pvc.name).Should(Equal(pvc.calculator.GetName(instanceName)))
		}
	})
})
