/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package e2e

import (
	"fmt"
	"os"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/storage/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/EnterpriseDB/cloud-native-postgresql/tests"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

// Test case for validating storage expansion
// with different storage providers in different k8s environments
var _ = Describe("Verify storage", func() {
	const (
		sampleFile  = fixturesDir + "/storage_expansion/cluster-storage-expansion.yaml"
		clusterName = "storage-expansion"
		level       = tests.Lowest
	)
	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})
	// Initializing a global namespace variable to be used in each test case
	var namespace string
	// Gathering default storage class requires to check whether the value
	// of 'allowVolumeExpansion' is true or false
	defaultStorageClass := os.Getenv("E2E_DEFAULT_STORAGE_CLASS")

	JustAfterEach(func() {
		if CurrentSpecReport().Failed() {
			env.DumpClusterEnv(namespace, clusterName,
				"out/"+CurrentSpecReport().LeafNodeText+".log")
		}
	})

	AfterEach(func() {
		err := env.DeleteNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
	})

	Context("can be expanded", func() {
		BeforeEach(func() {
			// Initializing namespace variable to be used in test case
			namespace = "storage-expansion-true"
			// Extracting bool value of AllowVolumeExpansion
			allowExpansion := GetStorageAllowExpansion(defaultStorageClass)
			if (allowExpansion == nil) || (*allowExpansion == false) {
				Skip(fmt.Sprintf("AllowedVolumeExpansion is false on %v", defaultStorageClass))
			}
		})

		It("expands PVCs via online resize", func() {
			// Creating namespace
			err := env.CreateNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())
			// Creating a cluster with three nodes
			AssertCreateCluster(namespace, clusterName, sampleFile, env)
			OnlineResizePVC(namespace, clusterName)
		})
	})

	Context("can not be expanded", func() {
		BeforeEach(func() {
			// Initializing namespace variable to be used in test case
			namespace = "storage-expansion-false"
			// Extracting bool value of AllowVolumeExpansion
			allowExpansion := GetStorageAllowExpansion(defaultStorageClass)
			if (allowExpansion != nil) && (*allowExpansion == true) {
				Skip(fmt.Sprintf("AllowedVolumeExpansion is true on %v", defaultStorageClass))
			}
		})

		It("expands PVCs via offline resize", func() {
			// Creating namespace
			err := env.CreateNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())
			AssertCreateCluster(namespace, clusterName, sampleFile, env)
			By("update cluster for resizeInUseVolumes as false", func() {
				// Updating cluster with 'resizeInUseVolumes' sets to 'false' in storage.
				// Check if operator does not return error
				_, _, err = tests.Run("kubectl patch cluster " + clusterName + " -n " + namespace +
					" -p '{\"spec\":{\"storage\":{\"resizeInUseVolumes\":false}}}' --type=merge")
				Expect(err).ToNot(HaveOccurred())
			})
			OfflineResizePVC(namespace, clusterName, 600)
		})
	})
})

// OnlineResizePVC is for verifying if storage can be automatically expanded, or not
func OnlineResizePVC(namespace, clusterName string) {
	pvc := &corev1.PersistentVolumeClaimList{}
	By("verify PVC before expansion", func() {
		// Verifying the first stage of deployment to compare it further with expanded value
		err := env.Client.List(env.Ctx, pvc, client.InNamespace(namespace))
		Expect(err).ToNot(HaveOccurred())
		// Iterating through PVC list to assure its default size
		for _, pvClaim := range pvc.Items {
			Expect(pvClaim.Status.Capacity.Storage().String()).To(BeEquivalentTo("1Gi"))
		}
	})
	By("expanding Cluster storage", func() {
		// Patching cluster to expand storage size from 1Gi to 2Gi
		_, _, err := tests.Run("kubectl patch cluster " + clusterName + " -n " + namespace +
			" -p '{\"spec\":{\"storage\":{\"size\":\"2Gi\"}}}' --type=merge")
		Expect(err).ToNot(HaveOccurred())
	})
	By("verifying Cluster storage is expanded", func() {
		// Gathering and verifying the new size of PVC after update on cluster
		Eventually(func() int {
			// Variable counter to store the updated total of expanded PVCs. It should be equal to three
			updateCount := 0
			// Gathering PVC list
			err := env.Client.List(env.Ctx, pvc, client.InNamespace(namespace))
			Expect(err).ToNot(HaveOccurred())
			// Iterating through PVC list to compare with expanded size
			for _, pvClaim := range pvc.Items {
				// Size comparison
				if pvClaim.Status.Capacity.Storage().String() == "2Gi" {
					updateCount++
				}
			}
			return updateCount
		}, 300).Should(BeEquivalentTo(3))
	})
}

func OfflineResizePVC(namespace, clusterName string, timeout int) {
	By("verify PVC size before expansion", func() {
		// Gathering PVC list for future use of comparison and deletion after storage expansion
		pvc := &corev1.PersistentVolumeClaimList{}
		err := env.Client.List(env.Ctx, pvc, client.InNamespace(namespace))
		Expect(err).ToNot(HaveOccurred())
		// Iterating through PVC list to verify the default size for future comparison
		for _, pvClaim := range pvc.Items {
			Expect(pvClaim.Status.Capacity.Storage().String()).To(BeEquivalentTo("1Gi"))
		}
	})
	By("expanding Cluster storage", func() {
		// Expanding cluster storage
		_, _, err := tests.Run("kubectl patch cluster " + clusterName + " -n " + namespace +
			" -p '{\"spec\":{\"storage\":{\"size\":\"2Gi\"}}}' --type=merge")
		Expect(err).ToNot(HaveOccurred())
	})
	By("deleting Pod and pPVC", func() {
		// Gathering cluster primary
		currentPrimary, err := env.GetClusterPrimary(namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		zero := int64(0)
		forceDelete := &client.DeleteOptions{
			GracePeriodSeconds: &zero,
		}
		// Gathering PVC list to be deleted
		pvc := &corev1.PersistentVolumeClaimList{}
		err = env.Client.List(env.Ctx, pvc, client.InNamespace(namespace))
		Expect(err).ToNot(HaveOccurred())
		// Iterating through PVC list for deleting pod and PVC for storage expansion
		for _, pvClaimNew := range pvc.Items {
			// Comparing cluster pods to not be primary to ensure cluster is healthy.
			// Primary will be eventually deleted
			if pvClaimNew.Name != currentPrimary.Name {
				// Deleting PVC
				_, _, err = tests.Run("kubectl delete pvc " + pvClaimNew.Name + " -n " + namespace + " --wait=false")
				Expect(err).ToNot(HaveOccurred())
				// Deleting standby and replica pods
				err = env.DeletePod(namespace, pvClaimNew.Name, forceDelete)
				Expect(err).ToNot(HaveOccurred())
				// Ensuring cluster is healthy with three pods
				AssertClusterIsReady(namespace, clusterName, timeout, env)
			}
		}
		// Deleting primary pvc
		_, _, err = tests.Run("kubectl delete pvc " + currentPrimary.Name + " -n " + namespace + " --wait=false")
		Expect(err).ToNot(HaveOccurred())
		// Deleting primary pod
		err = env.DeletePod(namespace, currentPrimary.Name, forceDelete)
		Expect(err).ToNot(HaveOccurred())
	})
	// Ensuring cluster is healthy, after failover of the primary pod and new pod is recreated
	AssertClusterIsReady(namespace, clusterName, timeout, env)
	By("verifying Cluster storage is expanded", func() {
		// Gathering PVC list for comparison
		pvcList, err := env.GetPVCList(namespace)
		Expect(err).ToNot(HaveOccurred())
		// Gathering PVC size and comparing with expanded value
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
		}, 30).Should(BeEquivalentTo(3))
	})
}

func GetStorageAllowExpansion(defaultStorageClass string) *bool {
	storageClass := &v1.StorageClass{}
	err := env.Client.Get(env.Ctx, client.ObjectKey{Name: defaultStorageClass}, storageClass)
	Expect(err).ToNot(HaveOccurred())
	// Return storage class 'AllowVolumeExpansion' value for expansion
	return storageClass.AllowVolumeExpansion
}
