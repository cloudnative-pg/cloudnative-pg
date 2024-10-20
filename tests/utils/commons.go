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

package utils

import (
	"fmt"

	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/run"
)

// ForgeArchiveWalOnMinio instead of using `switchWalCmd` to generate a real WAL archive, directly forges a WAL archive
// file on Minio by copying and renaming an existing WAL archive file for the sake of more control of testing. To make
// sure the forged one won't be a real WAL archive, we let the sequence in newWALName to be big enough so that it can't
// be a real WAL archive name in an idle postgresql.
func ForgeArchiveWalOnMinio(namespace, clusterName, miniClientPodName, existingWALName, newWALName string) error {
	// Forge a WAL archive by copying and renaming the 1st WAL archive
	minioWALBasePath := "minio/" + clusterName + "/" + clusterName + "/wals/0000000100000000"
	existingWALPath := minioWALBasePath + "/" + existingWALName + ".gz"
	newWALNamePath := minioWALBasePath + "/" + newWALName
	forgeWALOnMinioCmd := "mc cp " + existingWALPath + " " + newWALNamePath
	_, _, err := run.UncheckedRetry(fmt.Sprintf(
		"kubectl exec -n %v %v -- %v",
		namespace,
		miniClientPodName,
		forgeWALOnMinioCmd))

	return err
}

// TestFileExist tests if a file specified with `fileName` exist under directory `directoryPath`, on pod `podName` in
// namespace `namespace`
func TestFileExist(namespace, podName, directoryPath, fileName string) bool {
	filePath := directoryPath + "/" + fileName
	testFileExistCommand := "test -f " + filePath
	_, _, err := run.Unchecked(fmt.Sprintf(
		"kubectl exec -n %v %v -- %v",
		namespace,
		podName,
		testFileExistCommand))

	return err == nil
}

// TestDirectoryEmpty tests if a directory `directoryPath` exists on pod `podName` in namespace `namespace`
func TestDirectoryEmpty(namespace, podName, directoryPath string) bool {
	testDirectoryEmptyCommand := "test \"$(ls -A" + directoryPath + ")\""
	_, _, err := run.Unchecked(fmt.Sprintf(
		"kubectl exec -n %v %v -- %v",
		namespace,
		podName,
		testDirectoryEmptyCommand))

	return err == nil
}
