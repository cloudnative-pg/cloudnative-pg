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

package e2e

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/config"
	clusterasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/cluster"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	podutils "github.com/cloudnative-pg/cloudnative-pg/tests/utils/pods"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/run"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/storage"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Test case for validating storage expansion
// with different storage providers in different k8s environments
var _ = Describe("Verify storage", Label(tests.LabelStorage), func() {
	const (
		sampleFile  = fixturesDir + "/storage_expansion/cluster-storage-expansion.yaml.template"
		clusterName = "storage-expansion"
		level       = tests.Lowest
	)
	// Initializing a global namespace variable to be used in each test case
	var namespace, namespacePrefix string

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	// Gathering default storage class requires to check whether the value
	// of 'allowVolumeExpansion' is true or false
	defaultStorageClass := config.Current().Storage.StorageClass

	Context("can be expanded", func() {
		BeforeEach(func() {
			// Initializing namespace variable to be used in test case
			namespacePrefix = "storage-expansion-true"
			// Extracting bool value of AllowVolumeExpansion
			allowExpansion, err := storage.GetStorageAllowExpansion(
				env.Ctx, env.Client,
				defaultStorageClass,
			)
			Expect(err).ToNot(HaveOccurred())
			if (allowExpansion == nil) || (*allowExpansion == false) {
				Skip(fmt.Sprintf("AllowedVolumeExpansion is false on %v", defaultStorageClass))
			}
		})

		It("expands PVCs via online resize", func() {
			var err error
			// Creating namespace
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			// Creating a cluster with three nodes
			clusterasserts.AssertCreateCluster(env, testTimeouts, namespace, clusterName, sampleFile)
			OnlineResizePVC(namespace, clusterName)
		})
	})

	Context("can not be expanded", func() {
		BeforeEach(func() {
			// Initializing namespace variable to be used in test case
			namespacePrefix = "storage-expansion-false"
			// Extracting bool value of AllowVolumeExpansion
			allowExpansion, err := storage.GetStorageAllowExpansion(
				env.Ctx, env.Client,
				defaultStorageClass,
			)
			Expect(err).ToNot(HaveOccurred())
			if (allowExpansion != nil) && (*allowExpansion == true) {
				Skip(fmt.Sprintf("AllowedVolumeExpansion is true on %v", defaultStorageClass))
			}
		})
		It("expands PVCs via offline resize", func() {
			var err error
			// Creating namespace
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			clusterasserts.AssertCreateCluster(env, testTimeouts, namespace, clusterName, sampleFile)
			By("update cluster for resizeInUseVolumes as false", func() {
				// Updating cluster with 'resizeInUseVolumes' sets to 'false' in storage.
				// Check if operator does not return error
				Eventually(func() error {
					_, _, err = run.Unchecked("kubectl patch cluster " + clusterName + " -n " + namespace +
						" -p '{\"spec\":{\"storage\":{\"resizeInUseVolumes\":false}}}' --type=merge")
					if err != nil {
						return err
					}
					return nil
				}, 60, 5).Should(Succeed())
			})
			OfflineResizePVC(namespace, clusterName, 600)
		})
	})
})

// OnlineResizePVC is for verifying if storage can be automatically expanded, or not
func OnlineResizePVC(namespace, clusterName string) {
	walStorageEnabled, err := storage.IsWalStorageEnabled(
		env.Ctx, env.Client,
		namespace, clusterName,
	)
	Expect(err).ToNot(HaveOccurred())

	pvc := &corev1.PersistentVolumeClaimList{}
	By("verify PVC before expansion", func() {
		// Verifying the first stage of deployment to compare it further with expanded value
		err := env.Client.List(env.Ctx, pvc, ctrlclient.InNamespace(namespace))
		Expect(err).ToNot(HaveOccurred())
		// Iterating through PVC list to assure its default size
		for _, pvClaim := range pvc.Items {
			Expect(pvClaim.Status.Capacity.Storage().String()).To(BeEquivalentTo("1Gi"))
		}
	})
	By("expanding Cluster storage", func() {
		// Patching cluster to expand storage size from 1Gi to 2Gi
		storageType := []string{"storage"}
		if walStorageEnabled {
			storageType = append(storageType, "walStorage")
		}
		for _, s := range storageType {
			cmd := fmt.Sprintf(
				"kubectl patch cluster %v -n %v -p '{\"spec\":{\"%v\":{\"size\":\"2Gi\"}}}' --type=merge",
				clusterName,
				namespace,
				s,
			)
			Eventually(func() error {
				_, _, err := run.Unchecked(cmd)
				return err
			}, 60, 5).Should(Succeed())
		}
	})
	By("verifying Cluster storage is expanded", func() {
		// Gathering and verifying the new size of PVC after update on cluster
		expectedCount := 3
		if walStorageEnabled {
			expectedCount = 6
		}
		Eventually(func(g Gomega) {
			// Variable counter to store the updated total of expanded PVCs. It should be equal to three
			updateCount := 0
			// Gathering PVC list
			err := env.Client.List(env.Ctx, pvc, ctrlclient.InNamespace(namespace))
			g.Expect(err).ToNot(HaveOccurred())
			// Iterating through PVC list to compare with expanded size
			for _, pvClaim := range pvc.Items {
				// Size comparison
				if pvClaim.Status.Capacity.Storage().String() == "2Gi" {
					updateCount++
				}
			}
			g.Expect(updateCount).To(BeEquivalentTo(expectedCount))
		}, 300).Should(Succeed())
	})
}

func OfflineResizePVC(namespace, clusterName string, timeout int) {
	walStorageEnabled, err := storage.IsWalStorageEnabled(
		env.Ctx, env.Client,
		namespace, clusterName,
	)
	Expect(err).ToNot(HaveOccurred())

	By("verify PVC size before expansion", func() {
		// Gathering PVC list for future use of comparison and deletion after storage expansion
		pvc := &corev1.PersistentVolumeClaimList{}
		err := env.Client.List(env.Ctx, pvc, ctrlclient.InNamespace(namespace))
		Expect(err).ToNot(HaveOccurred())
		// Iterating through PVC list to verify the default size for future comparison
		for _, pvClaim := range pvc.Items {
			Expect(pvClaim.Status.Capacity.Storage().String()).To(BeEquivalentTo("1Gi"))
		}
	})
	By("expanding Cluster storage", func() {
		// Expanding cluster storage
		storageType := []string{"storage"}
		if walStorageEnabled {
			storageType = append(storageType, "walStorage")
		}
		for _, s := range storageType {
			cmd := fmt.Sprintf(
				"kubectl patch cluster %v -n %v -p '{\"spec\":{\"%v\":{\"size\":\"2Gi\"}}}' --type=merge",
				clusterName,
				namespace,
				s,
			)
			Eventually(func() error {
				_, _, err := run.Unchecked(cmd)
				return err
			}, 60, 5).Should(Succeed())
		}
	})
	By("deleting Pod and PVCs, first replicas then the primary", func() {
		// Gathering cluster primary
		currentPrimary, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		currentPrimaryWalStorageName := currentPrimary.Name + "-wal"
		quickDelete := &ctrlclient.DeleteOptions{
			GracePeriodSeconds: &quickDeletionPeriod,
		}

		podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		Expect(len(podList.Items), err).To(BeEquivalentTo(3))

		// Iterating through PVC list for deleting pod and PVC for storage expansion
		for _, p := range podList.Items {
			// Comparing cluster pods to not be primary to ensure cluster is healthy.
			// Primary will be eventually deleted
			if !specs.IsPodPrimary(p) {
				// Deleting PVC
				_, _, err = run.Run(
					"kubectl delete pvc " + p.Name + " -n " + namespace + " --wait=false",
				)
				Expect(err).ToNot(HaveOccurred())
				// Deleting WalStorage PVC if needed
				if walStorageEnabled {
					_, _, err = run.Run(
						"kubectl delete pvc " + p.Name + "-wal" + " -n " + namespace + " --wait=false",
					)
					Expect(err).ToNot(HaveOccurred())
				}
				// Deleting standby and replica pods
				err = podutils.Delete(env.Ctx, env.Client, namespace, p.Name, quickDelete)
				Expect(err).ToNot(HaveOccurred())
			}
		}
		clusterasserts.AssertClusterIsReady(env, namespace, clusterName, timeout)

		// Deleting primary pvc
		_, _, err = run.Run(
			"kubectl delete pvc " + currentPrimary.Name + " -n " + namespace + " --wait=false",
		)
		Expect(err).ToNot(HaveOccurred())
		// Deleting Primary WalStorage PVC if needed
		if walStorageEnabled {
			_, _, err = run.Run(
				"kubectl delete pvc " + currentPrimaryWalStorageName + " -n " + namespace + " --wait=false",
			)
			Expect(err).ToNot(HaveOccurred())
		}
		// Deleting primary pod
		err = podutils.Delete(env.Ctx, env.Client, namespace, currentPrimary.Name, quickDelete)
		Expect(err).ToNot(HaveOccurred())
	})

	clusterasserts.AssertClusterIsReady(env, namespace, clusterName, timeout)
	By("verifying Cluster storage is expanded", func() {
		// Gathering PVC list for comparison
		pvcList, err := storage.GetPVCList(env.Ctx, env.Client, namespace)
		Expect(err).ToNot(HaveOccurred())
		// Gathering PVC size and comparing with expanded value
		expectedCount := 3
		if walStorageEnabled {
			expectedCount = 6
		}
		Eventually(func() int {
			// Bool value to ensure every pod in cluster expanded, will be eventually compared as true
			count := 0
			// Iterating through PVC list for comparison
			for _, pvClaim := range pvcList.Items {
				// Comparing to expanded value.
				// Once all pods will be expanded, count will be equal to three
				if pvClaim.Status.Capacity.Storage().String() == "2Gi" {
					count++
				}
			}
			return count
		}, 30).Should(BeEquivalentTo(expectedCount))
	})
}
