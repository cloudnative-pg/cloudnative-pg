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
	"time"

	"github.com/avast/retry-go/v4"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ForgeArchiveWalOnMinio instead of using `switchWalCmd` to generate a real WAL archive, directly forges a WAL archive
// file on Minio by copying and renaming an existing WAL archive file for the sake of more control of testing. To make
// sure the forged one won't be a real WAL archive, we let the sequence in newWALName to be big enough so that it can't
// be a real WAL archive name in an idle postgresql.
func ForgeArchiveWalOnMinio(namespace, clusterName, miniClientPodName, existingWALName, newWALName string) error {
	// Forge a WAL archive by copying and renaming the 1st WAL archive
	minioWALBasePath := "minio/cluster-backups/" + clusterName + "/wals/0000000100000000"
	existingWALPath := minioWALBasePath + "/" + existingWALName + ".gz"
	newWALNamePath := minioWALBasePath + "/" + newWALName
	forgeWALOnMinioCmd := "mc cp " + existingWALPath + " " + newWALNamePath
	_, _, err := RunUncheckedRetry(fmt.Sprintf(
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
	_, _, err := RunUnchecked(fmt.Sprintf(
		"kubectl exec -n %v %v -- %v",
		namespace,
		podName,
		testFileExistCommand))

	return err == nil
}

// TestDirectoryEmpty tests if a directory `directoryPath` exists on pod `podName` in namespace `namespace`
func TestDirectoryEmpty(namespace, podName, directoryPath string) bool {
	testDirectoryEmptyCommand := "test \"$(ls -A" + directoryPath + ")\""
	_, _, err := RunUnchecked(fmt.Sprintf(
		"kubectl exec -n %v %v -- %v",
		namespace,
		podName,
		testDirectoryEmptyCommand))

	return err == nil
}

// CreateObject create object in the Kubernetes cluster
func CreateObject(env *TestingEnvironment, object client.Object, opts ...client.CreateOption) error {
	err := retry.Do(
		func() error {
			err := env.Client.Create(env.Ctx, object, opts...)
			if err != nil {
				return err
			}
			return nil
		},
		retry.Delay(PollingTime*time.Second),
		retry.Attempts(RetryAttempts),
		retry.DelayType(retry.FixedDelay),
		retry.RetryIf(func(err error) bool { return !apierrs.IsAlreadyExists(err) }),
	)
	return err
}

// DeleteObject delete object in the Kubernetes cluster
func DeleteObject(env *TestingEnvironment, object client.Object, opts ...client.DeleteOption) error {
	err := retry.Do(
		func() error {
			return env.Client.Delete(env.Ctx, object, opts...)
		},
		retry.Delay(PollingTime*time.Second),
		retry.Attempts(RetryAttempts),
		retry.DelayType(retry.FixedDelay),
		retry.RetryIf(func(err error) bool { return !apierrs.IsNotFound(err) }),
	)
	return err
}

// GetObjectList retrieves list of objects for a given namespace and list options
func GetObjectList(env *TestingEnvironment, objectList client.ObjectList, opts ...client.ListOption) error {
	err := retry.Do(
		func() error {
			err := env.Client.List(env.Ctx, objectList, opts...)
			if err != nil {
				return err
			}
			return nil
		},
		retry.Delay(PollingTime*time.Second),
		retry.Attempts(RetryAttempts),
		retry.DelayType(retry.FixedDelay),
	)
	return err
}

// GetObject retrieves an objects for the given object key from the Kubernetes Cluster
func GetObject(env *TestingEnvironment, objectKey client.ObjectKey, object client.Object) error {
	err := retry.Do(
		func() error {
			err := env.Client.Get(env.Ctx, objectKey, object)
			if err != nil {
				return err
			}
			return nil
		},
		retry.Delay(PollingTime*time.Second),
		retry.Attempts(RetryAttempts),
		retry.DelayType(retry.FixedDelay),
	)
	return err
}
