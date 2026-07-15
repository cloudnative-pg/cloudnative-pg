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
	"strings"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/manager/walrestore"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	clusterasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/cluster"
	objectstoreasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/objectstore"
	testUtils "github.com/cloudnative-pg/cloudnative-pg/tests/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/exec"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/objectstore"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/secrets"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/yaml"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// This e2e test is to test the wal-restore handling when maxParallel (specified as "3" in this testing) is specified in
// wal section under backup for wal archive storing/recovering. To facilitate controlling the testing, we directly forge
// wals on the object storage by copying and renaming an existing wal file.

var _ = Describe("Wal-restore in parallel", Label(tests.LabelBackupRestore), func() {
	const (
		level          = tests.High
		PgWalPath      = specs.PgWalPath
		SpoolDirectory = walrestore.SpoolDirectory
	)

	var namespace string
	var primary, standby, latestWAL string
	var walFile1, walFile2, walFile3, walFile4, walFile5, walFile6, walFile7, walFile8 string

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
		if !(IsKind() || IsK3D()) {
			Skip("This test only runs on kind or k3d clusters")
		}
	})

	It("Wal-restore in parallel using the object store for backup", func() {
		// This is a set of tests using an object storage server deployed in
		// the same namespace as the cluster. Since each cluster is installed
		// in its own namespace, they can share the configuration file

		const (
			clusterWithObjectStoreSampleFile = fixturesDir +
				"/backup/object_store/cluster-with-backup-object-store-with-wal-max-parallel.yaml.template"
		)

		const namespacePrefix = "pg-backup-object-store-wal-max-parallel"
		clusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, clusterWithObjectStoreSampleFile)
		Expect(err).ToNot(HaveOccurred())

		namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
		Expect(err).ToNot(HaveOccurred())

		By("creating the credentials for the object store", func() {
			_, err = secrets.CreateObjectStorageSecret(
				env.Ctx,
				env.Client,
				namespace,
				"backup-storage-creds",
				objectstore.AccessKeyID,
				objectstore.SecretAccessKey,
			)
			Expect(err).ToNot(HaveOccurred())
		})

		By("create the certificates for the object store", func() {
			err := objectStoreEnv.CreateCaSecret(env, namespace)
			Expect(err).ToNot(HaveOccurred())
		})

		// Create the cluster and assert it be ready
		clusterasserts.AssertCreateCluster(env, testTimeouts, namespace, clusterName, clusterWithObjectStoreSampleFile)

		// Get the primary
		pod, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		primary = pod.GetName()

		// Get the standby
		podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		for _, po := range podList.Items {
			if po.Name != primary {
				// Only one standby in this specific testing
				standby = po.GetName()
				break
			}
		}
		Expect(err).ToNot(HaveOccurred())

		// Make sure both Wal-archive and the object store work
		// Create a WAL on the primary and check if it arrives at the object store, within a short time
		By("archiving WALs and verifying they exist", func() {
			pod, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			primary := pod.GetName()
			latestWAL = objectstoreasserts.SwitchWalAndGetLatestArchive(env, namespace, primary)
			latestWALPath := objectstore.GetFilePath(clusterName, latestWAL+".gz")
			Eventually(func() (int, error) {
				// WALs are compressed with gzip in the fixture
				return objectstore.CountFiles(objectStoreEnv, latestWALPath)
			}, RetryTimeout).Should(BeEquivalentTo(1),
				fmt.Sprintf("verify the existence of WAL %v in the object store", latestWALPath))
		})

		By("forging 5 wals on the object store by copying and renaming an existing archive file", func() {
			walFile1 = "0000000100000000000000F1"
			walFile2 = "0000000100000000000000F2"
			walFile3 = "0000000100000000000000F3"
			walFile4 = "0000000100000000000000F4"
			walFile5 = "0000000100000000000000F5"
			Expect(testUtils.ForgeArchiveWalOnObjectStore(
				objectStoreEnv.Namespace, clusterName, objectStoreEnv.ClientPodRef(), latestWAL,
				walFile1)).
				ShouldNot(HaveOccurred())
			Expect(testUtils.ForgeArchiveWalOnObjectStore(
				objectStoreEnv.Namespace, clusterName, objectStoreEnv.ClientPodRef(), latestWAL,
				walFile2)).
				ShouldNot(HaveOccurred())
			Expect(testUtils.ForgeArchiveWalOnObjectStore(
				objectStoreEnv.Namespace, clusterName, objectStoreEnv.ClientPodRef(), latestWAL,
				walFile3)).
				ShouldNot(HaveOccurred())
			Expect(testUtils.ForgeArchiveWalOnObjectStore(
				objectStoreEnv.Namespace, clusterName, objectStoreEnv.ClientPodRef(), latestWAL,
				walFile4)).
				ShouldNot(HaveOccurred())
			Expect(testUtils.ForgeArchiveWalOnObjectStore(
				objectStoreEnv.Namespace, clusterName, objectStoreEnv.ClientPodRef(), latestWAL,
				walFile5)).
				ShouldNot(HaveOccurred())
		})

		By("asserting the spool directory is empty on the standby", func() {
			if !testUtils.TestDirectoryEmpty(namespace, standby, SpoolDirectory) {
				purgeSpoolDirectoryCmd := "rm " + SpoolDirectory + "/*"
				_, _, err := exec.CommandInInstancePod(
					env.Ctx, env.Client, env.Interface, env.RestClientConfig,
					exec.PodLocator{
						Namespace: namespace,
						PodName:   standby,
					}, nil,
					purgeSpoolDirectoryCmd)
				Expect(err).ShouldNot(HaveOccurred())
			}
		})

		// Invoke the wal-restore command through exec requesting the #1 file.
		// Expected outcome:
		// 		exit code 0, #1 is in the output location, #2 and #3 are in the spool directory.
		// 		The flag is unset.
		By("invoking the wal-restore command requesting #1 wal", func() {
			_, _, err := exec.CommandInInstancePod(
				env.Ctx, env.Client, env.Interface, env.RestClientConfig,
				exec.PodLocator{
					Namespace: namespace,
					PodName:   standby,
				}, nil,
				"/controller/manager", "wal-restore", walFile1, PgWalPath+"/"+walFile1)
			Expect(err).ToNot(HaveOccurred(), "exit code should be 0")
			Eventually(func() bool { return testUtils.TestFileExist(namespace, standby, PgWalPath, walFile1) }).
				WithTimeout(RetryTimeout).
				Should(BeTrue(),
					"#1 wal is in the output location")
			Eventually(func() bool { return testUtils.TestFileExist(namespace, standby, SpoolDirectory, walFile2) }).
				WithTimeout(RetryTimeout).
				Should(BeTrue(),
					"#2 wal is in the spool directory")
			Eventually(func() bool { return testUtils.TestFileExist(namespace, standby, SpoolDirectory, walFile3) }).
				WithTimeout(RetryTimeout).
				Should(BeTrue(),
					"#3 wal is in the spool directory")
			Eventually(func() bool {
				return testUtils.TestFileExist(namespace, standby, SpoolDirectory,
					"end-of-wal-stream")
			}).
				WithTimeout(RetryTimeout).
				Should(BeFalse(),
					"end-of-wal-stream flag is unset")
		})

		// Invoke the wal-restore command through exec requesting the #2 file.
		// Expected outcome:
		// 		exit code 0, #2 is in the output location, #3 is in the spool directory.
		// 		The flag is unset.
		By("invoking the wal-restore command requesting #2 wal", func() {
			_, _, err := exec.CommandInInstancePod(
				env.Ctx, env.Client, env.Interface, env.RestClientConfig,
				exec.PodLocator{
					Namespace: namespace,
					PodName:   standby,
				}, nil,
				"/controller/manager", "wal-restore", walFile2, PgWalPath+"/"+walFile2)
			Expect(err).ToNot(HaveOccurred(), "exit code should be 0")
			Eventually(func() bool { return testUtils.TestFileExist(namespace, standby, PgWalPath, walFile2) }).
				WithTimeout(RetryTimeout).
				Should(BeTrue(),
					"#2 wal is in the output location")
			Eventually(func() bool { return testUtils.TestFileExist(namespace, standby, SpoolDirectory, walFile3) }).
				WithTimeout(RetryTimeout).
				Should(BeTrue(),
					"#3 wal is in the spool directory")
			Eventually(func() bool {
				return testUtils.TestFileExist(namespace, standby, SpoolDirectory,
					"end-of-wal-stream")
			}).
				WithTimeout(RetryTimeout).
				Should(BeFalse(),
					"end-of-wal-stream flag is unset")
		})

		// Invoke the wal-restore command through exec requesting the #3 file.
		// Expected outcome:
		// 		exit code 0, #3 is in the output location, spool directory is empty.
		// 		The flag is unset.
		By("invoking the wal-restore command requesting #3 wal", func() {
			_, _, err := exec.CommandInInstancePod(
				env.Ctx, env.Client, env.Interface, env.RestClientConfig,
				exec.PodLocator{
					Namespace: namespace,
					PodName:   standby,
				}, nil,
				"/controller/manager", "wal-restore", walFile3, PgWalPath+"/"+walFile3)
			Expect(err).ToNot(HaveOccurred(), "exit code should be 0")
			Eventually(func() bool { return testUtils.TestFileExist(namespace, standby, PgWalPath, walFile3) }).
				WithTimeout(RetryTimeout).
				Should(BeTrue(),
					"#3 wal is in the output location")
			Eventually(func() bool { return testUtils.TestDirectoryEmpty(namespace, standby, SpoolDirectory) }).
				WithTimeout(RetryTimeout).
				Should(BeTrue(),
					"spool directory is empty, end-of-wal-stream flag is unset")
		})

		// Invoke the wal-restore command through exec requesting the #4 file.
		// Expected outcome:
		// 		exit code 0, #4 is in the output location, #5 is in the spool directory.
		// 		The flag is set because #6 file not present.
		By("invoking the wal-restore command requesting #4 wal", func() {
			_, _, err := exec.CommandInInstancePod(
				env.Ctx, env.Client, env.Interface, env.RestClientConfig,
				exec.PodLocator{
					Namespace: namespace,
					PodName:   standby,
				}, nil,
				"/controller/manager", "wal-restore", walFile4, PgWalPath+"/"+walFile4)
			Expect(err).ToNot(HaveOccurred())
			Eventually(func() bool { return testUtils.TestFileExist(namespace, standby, PgWalPath, walFile4) }).
				WithTimeout(RetryTimeout).
				Should(BeTrue(),
					"#4 wal is in the output location")
			Eventually(func() bool { return testUtils.TestFileExist(namespace, standby, SpoolDirectory, walFile5) }).
				WithTimeout(RetryTimeout).
				Should(BeTrue(),
					"#5 wal is in the spool directory")
			Eventually(func() bool {
				return testUtils.TestFileExist(namespace, standby, SpoolDirectory,
					"end-of-wal-stream")
			}).
				WithTimeout(RetryTimeout).
				Should(BeTrue(),
					"end-of-wal-stream flag is set for #6 wal is not present")
		})

		// Generate a new wal file; the archive also contains WAL #6.
		By("forging a new wal file, the #6 wal", func() {
			walFile6 = "0000000100000000000000F6"
			Expect(testUtils.ForgeArchiveWalOnObjectStore(
				objectStoreEnv.Namespace, clusterName, objectStoreEnv.ClientPodRef(), latestWAL,
				walFile6)).
				ShouldNot(HaveOccurred())
		})

		// Invoke the wal-restore command through exec requesting the #5 file.
		// Expected outcome:
		//		exit code 0, #5 is in the output location, no files in the spool directory. The flag is still present.
		By("invoking the wal-restore command requesting #5 wal", func() {
			_, _, err := exec.CommandInInstancePod(
				env.Ctx, env.Client, env.Interface, env.RestClientConfig,
				exec.PodLocator{
					Namespace: namespace,
					PodName:   standby,
				}, nil,
				"/controller/manager", "wal-restore", walFile5, PgWalPath+"/"+walFile5)
			Expect(err).ToNot(HaveOccurred(), "exit code should be 0")
			Eventually(func() bool { return testUtils.TestFileExist(namespace, standby, PgWalPath, walFile5) }).
				WithTimeout(RetryTimeout).
				Should(BeTrue(),
					"#5 wal is in the output location")
			Eventually(func() bool { return testUtils.TestFileExist(namespace, standby, SpoolDirectory, "00000001*") }).
				WithTimeout(RetryTimeout).
				Should(BeFalse(),
					"no wal files exist in the spool directory")
			Eventually(func() bool {
				return testUtils.TestFileExist(namespace, standby, SpoolDirectory,
					"end-of-wal-stream")
			}).
				WithTimeout(RetryTimeout).
				Should(BeTrue(),
					"end-of-wal-stream flag is still there")
		})

		// Invoke the wal-restore command through exec requesting the #6 file.
		// Expected outcome:
		//		exit code 1, output location untouched, no files in the spool directory. The flag is unset.
		By("invoking the wal-restore command requesting #6 wal", func() {
			_, _, err := exec.CommandInInstancePod(
				env.Ctx, env.Client, env.Interface, env.RestClientConfig,
				exec.PodLocator{
					Namespace: namespace,
					PodName:   standby,
				}, nil,
				"/controller/manager", "wal-restore", walFile6, PgWalPath+"/"+walFile6)
			Expect(err).To(HaveOccurred(),
				"exit code should 1 since #6 wal is not in the output location or spool directory and flag is set")
			Eventually(func() bool { return testUtils.TestFileExist(namespace, standby, PgWalPath, walFile6) }).
				WithTimeout(RetryTimeout).
				Should(BeFalse(),
					"#6 wal is not in the output location")
			Eventually(func() bool { return testUtils.TestDirectoryEmpty(namespace, standby, SpoolDirectory) }).
				WithTimeout(RetryTimeout).
				Should(BeTrue(),
					"spool directory is empty, end-of-wal-stream flag is unset")
		})

		// Invoke the wal-restore command through exec requesting the #6 file again.
		// Expected outcome:
		//		exit code 0, #6 is in the output location, no files in the spool directory.
		//		The flag is present again because #7 and #8 are unavailable.
		By("invoking the wal-restore command requesting #6 wal again", func() {
			_, _, err := exec.CommandInInstancePod(
				env.Ctx, env.Client, env.Interface, env.RestClientConfig,
				exec.PodLocator{
					Namespace: namespace,
					PodName:   standby,
				}, nil,
				"/controller/manager", "wal-restore", walFile6, PgWalPath+"/"+walFile6)
			Expect(err).ToNot(HaveOccurred(), "exit code should be 0")
			Eventually(func() bool { return testUtils.TestFileExist(namespace, standby, PgWalPath, walFile6) }).
				WithTimeout(RetryTimeout).
				Should(BeTrue(),
					"#6 wal is in the output location")
			Eventually(func() bool { return testUtils.TestFileExist(namespace, standby, SpoolDirectory, "00000001*") }).
				WithTimeout(RetryTimeout).
				Should(BeFalse(),
					"no wals in the spool directory")
			Eventually(func() bool {
				return testUtils.TestFileExist(namespace, standby, SpoolDirectory,
					"end-of-wal-stream")
			}).
				WithTimeout(RetryTimeout).
				Should(BeTrue(),
					"end-of-wal-stream flag is set for #7 and #8 wal is not present")
		})

		// Generate a new wal file; the archive also contains WAL #7.
		By("forging a new wal file, the #7 wal", func() {
			walFile7 = "0000000100000000000000F7"
			Expect(testUtils.ForgeArchiveWalOnObjectStore(
				objectStoreEnv.Namespace, clusterName, objectStoreEnv.ClientPodRef(), latestWAL,
				walFile7)).
				ShouldNot(HaveOccurred())
		})

		// listSpoolContents returns the exact content of the spool directory,
		// one file name per line. The TestFileExist/TestDirectoryEmpty helpers
		// cannot express "the spool contains no WAL files": kubectl exec runs
		// the command with no shell, so glob patterns are never expanded.
		listSpoolContents := func() (string, error) {
			stdout, _, err := exec.CommandInInstancePod(
				env.Ctx, env.Client, env.Interface, env.RestClientConfig,
				exec.PodLocator{
					Namespace: namespace,
					PodName:   standby,
				}, nil,
				"ls", "-A", SpoolDirectory)
			return strings.TrimSpace(stdout), err
		}

		// Invoke the wal-restore command in rewind mode requesting the #7 file,
		// with the end-of-wal-stream flag still set.
		// Expected outcome:
		//		exit code 0, #7 is in the output location, nothing is prefetched
		//		into the spool directory, and the flag is neither consumed nor unset.
		By("invoking the wal-restore command in rewind mode with a stale end-of-wal-stream flag", func() {
			_, _, err := exec.CommandInInstancePod(
				env.Ctx, env.Client, env.Interface, env.RestClientConfig,
				exec.PodLocator{
					Namespace: namespace,
					PodName:   standby,
				}, nil,
				"/controller/manager", "wal-restore", "--rewind", walFile7, PgWalPath+"/"+walFile7)
			Expect(err).ToNot(HaveOccurred(),
				"exit code should be 0: in rewind mode a stale flag must not abort the restore")
			Eventually(func() bool { return testUtils.TestFileExist(namespace, standby, PgWalPath, walFile7) }).
				WithTimeout(RetryTimeout).
				Should(BeTrue(),
					"#7 wal is in the output location")
			Eventually(listSpoolContents).
				WithTimeout(RetryTimeout).
				Should(Equal("end-of-wal-stream"),
					"the spool directory contains the untouched end-of-wal-stream flag "+
						"and no prefetched wals")
		})

		// Invoke the wal-restore command through exec requesting the #8 file.
		// Expected outcome:
		//		exit code 1, the flag is consumed and unset.
		By("consuming the end-of-wal-stream flag with a regular invocation", func() {
			walFile8 = "0000000100000000000000F8"
			_, _, err := exec.CommandInInstancePod(
				env.Ctx, env.Client, env.Interface, env.RestClientConfig,
				exec.PodLocator{
					Namespace: namespace,
					PodName:   standby,
				}, nil,
				"/controller/manager", "wal-restore", walFile8, PgWalPath+"/"+walFile8)
			Expect(err).To(HaveOccurred(), "exit code should be 1 since the flag is set")
			Eventually(listSpoolContents).
				WithTimeout(RetryTimeout).
				Should(BeEmpty(),
					"end-of-wal-stream flag is consumed and unset")
		})

		// Generate a new wal file; the archive also contains WAL #8.
		By("forging a new wal file, the #8 wal", func() {
			Expect(testUtils.ForgeArchiveWalOnObjectStore(
				objectStoreEnv.Namespace, clusterName, objectStoreEnv.ClientPodRef(), latestWAL,
				walFile8)).
				ShouldNot(HaveOccurred())
		})

		// Invoke the wal-restore command in rewind mode requesting the #8 file,
		// while #9 and #10 are not in the archive.
		// Expected outcome:
		//		exit code 0, #8 is in the output location, no files in the spool directory.
		//		The flag is not set, while a regular invocation would have set it.
		By("invoking the wal-restore command in rewind mode does not set the flag", func() {
			_, _, err := exec.CommandInInstancePod(
				env.Ctx, env.Client, env.Interface, env.RestClientConfig,
				exec.PodLocator{
					Namespace: namespace,
					PodName:   standby,
				}, nil,
				"/controller/manager", "wal-restore", "--rewind", walFile8, PgWalPath+"/"+walFile8)
			Expect(err).ToNot(HaveOccurred(), "exit code should be 0")
			Eventually(func() bool { return testUtils.TestFileExist(namespace, standby, PgWalPath, walFile8) }).
				WithTimeout(RetryTimeout).
				Should(BeTrue(),
					"#8 wal is in the output location")
			Eventually(listSpoolContents).
				WithTimeout(RetryTimeout).
				Should(BeEmpty(),
					"the spool directory is empty: no prefetched wals, and no "+
						"end-of-wal-stream flag set by the rewind mode")
		})
	})
})
