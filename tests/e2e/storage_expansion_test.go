/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package e2e

import (
	"fmt"
	"os"

	"github.com/EnterpriseDB/cloud-native-postgresql/tests"
	"github.com/EnterpriseDB/cloud-native-postgresql/tests/utils"

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
			allowExpansion := utils.GetStorageAllowExpansion(defaultStorageClass, env)
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
			allowExpansion := utils.GetStorageAllowExpansion(defaultStorageClass, env)
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
				_, _, err = utils.Run("kubectl patch cluster " + clusterName + " -n " + namespace +
					" -p '{\"spec\":{\"storage\":{\"resizeInUseVolumes\":false}}}' --type=merge")
				Expect(err).ToNot(HaveOccurred())
			})
			OfflineResizePVC(namespace, clusterName, 600)
		})
	})
})
