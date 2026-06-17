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

// Package objectstore provides Ginkgo/Gomega assertions for WAL archiving
// to the object store. Callers that also import tests/utils/objectstore
// should alias one of the two to avoid the package name collision.
package objectstore

import (
	"fmt"
	"strings"
	"time"

	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/environment"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/exec"
	objectstoreutils "github.com/cloudnative-pg/cloudnative-pg/tests/utils/objectstore"
	pgutils "github.com/cloudnative-pg/cloudnative-pg/tests/utils/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"

	. "github.com/onsi/ginkgo/v2" //nolint
	. "github.com/onsi/gomega"    //nolint
)

// CheckPointAndSwitchWalOnPrimary triggers a checkpoint and switches WAL on
// the primary pod, returning the name of the latest archived WAL file.
func CheckPointAndSwitchWalOnPrimary(env *environment.TestingEnvironment, namespace, clusterName string) string {
	GinkgoHelper()
	var latestWAL string
	By("trigger checkpoint and switch wal on primary", func() {
		pod, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		primary := pod.GetName()
		latestWAL = SwitchWalAndGetLatestArchive(env, namespace, primary)
	})
	return latestWAL
}

// AssertArchiveWalOnObjectStore triggers a WAL switch and verifies the
// resulting WAL file is uploaded to the object store within the
// WalsInObjectStore timeout.
func AssertArchiveWalOnObjectStore(
	env *environment.TestingEnvironment,
	testTimeouts map[timeouts.Timeout]int,
	storeEnv *objectstoreutils.Env,
	namespace, clusterName, serverName string,
) {
	GinkgoHelper()
	var latestWALPath string
	// Create a WAL on the primary and check if it arrives at the object store, within a short time
	By("archiving WALs and verifying they exist", func() {
		pod, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		primary := pod.GetName()
		latestWAL := SwitchWalAndGetLatestArchive(env, namespace, primary)
		latestWALPath = objectstoreutils.GetFilePath(serverName, latestWAL+".gz")
	})

	By(fmt.Sprintf("verify the existence of WAL %v in the object store", latestWALPath), func() {
		Eventually(func() (int, error) {
			// WALs are compressed with gzip in the fixture
			return objectstoreutils.CountFiles(storeEnv, latestWALPath)
		}, testTimeouts[timeouts.WalsInObjectStore]).Should(BeEquivalentTo(1))
	})
}

// SwitchWalAndGetLatestArchive triggers a new WAL and returns the name of
// the latest WAL file produced.
func SwitchWalAndGetLatestArchive(env *environment.TestingEnvironment, namespace, podName string) string {
	_, _, err := exec.QueryInInstancePodWithTimeout(
		env.Ctx, env.Client, env.Interface, env.RestClientConfig,
		exec.PodLocator{
			Namespace: namespace,
			PodName:   podName,
		},
		pgutils.PostgresDBName,
		"CHECKPOINT",
		300*time.Second,
	)
	Expect(err).ToNot(HaveOccurred(),
		"failed to trigger a new wal while executing 'SwitchWalAndGetLatestArchive'")

	out, _, err := exec.QueryInInstancePod(
		env.Ctx, env.Client, env.Interface, env.RestClientConfig,
		exec.PodLocator{
			Namespace: namespace,
			PodName:   podName,
		},
		pgutils.PostgresDBName,
		"SELECT pg_catalog.pg_walfile_name(pg_switch_wal())",
	)
	Expect(err).ToNot(
		HaveOccurred(),
		"failed to get latest wal file name while executing 'SwitchWalAndGetLatestArchive",
	)

	return strings.TrimSpace(out)
}
