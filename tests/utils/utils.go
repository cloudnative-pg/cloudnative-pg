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

package utils

import (
	"bytes"
	"context"
	"fmt"
	"text/tabwriter"

	"github.com/cheynewallace/tabby"
	batchv1 "k8s.io/api/batch/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	utils2 "github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/run"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/storage"
)

// PrintClusterResources prints a summary of the cluster pods, jobs, pvcs etc.
func PrintClusterResources(ctx context.Context, crudClient client.Client, namespace, clusterName string) string {
	cluster, err := clusterutils.Get(ctx, crudClient, namespace, clusterName)
	if err != nil {
		return fmt.Sprintf("Error while Getting Object %v", err)
	}

	buffer := &bytes.Buffer{}
	w := tabwriter.NewWriter(buffer, 0, 0, 4, ' ', 0)
	clusterInfo := tabby.NewCustom(w)
	clusterInfo.AddLine("Timeout while waiting for cluster ready, dumping more cluster information for analysis...")
	clusterInfo.AddLine()
	clusterInfo.AddLine()
	clusterInfo.AddLine("Cluster information:")
	clusterInfo.AddLine("Name", cluster.GetName())
	clusterInfo.AddLine("Namespace", cluster.GetNamespace())
	clusterInfo.AddLine()
	clusterInfo.AddHeader("Items", "Values")
	clusterInfo.AddLine("Spec.Instances", cluster.Spec.Instances)
	clusterInfo.AddLine("Wal storage", cluster.ShouldCreateWalArchiveVolume())
	clusterInfo.AddLine("Cluster phase", cluster.Status.Phase)
	clusterInfo.AddLine("Phase reason", cluster.Status.PhaseReason)
	clusterInfo.AddLine("Cluster target primary", cluster.Status.TargetPrimary)
	clusterInfo.AddLine("Cluster current primary", cluster.Status.CurrentPrimary)
	clusterInfo.AddLine()

	podList, _ := clusterutils.ListPods(ctx, crudClient, cluster.GetNamespace(), cluster.GetName())

	clusterInfo.AddLine("Cluster Pods information:")
	clusterInfo.AddLine("Ready pod number: ", utils2.CountReadyPods(podList.Items))
	clusterInfo.AddLine()
	clusterInfo.AddHeader("Items", "Values")
	for _, pod := range podList.Items {
		clusterInfo.AddLine("Pod name", pod.Name)
		clusterInfo.AddLine("Pod phase", pod.Status.Phase)
		if cluster.Status.InstancesReportedState != nil {
			if instanceReportState, ok := cluster.Status.InstancesReportedState[apiv1.PodName(pod.Name)]; ok {
				clusterInfo.AddLine("Is Primary", instanceReportState.IsPrimary)
				clusterInfo.AddLine("TimeLineID", instanceReportState.TimeLineID)
				clusterInfo.AddLine("---", "---")
			}
		} else {
			clusterInfo.AddLine("InstanceReportState not reported", "")
		}
	}

	clusterInfo.AddLine("Jobs information:")
	clusterInfo.AddLine()
	clusterInfo.AddHeader("Items", "Values")
	jobList := &batchv1.JobList{}
	_ = crudClient.List(
		ctx, jobList, client.InNamespace(namespace),
	)
	for _, job := range jobList.Items {
		clusterInfo.AddLine("Job name", job.Name)
		clusterInfo.AddLine("Job status", fmt.Sprintf("%#v", job.Status))
	}

	pvcList, _ := storage.GetPVCList(ctx, crudClient, cluster.GetNamespace())
	clusterInfo.AddLine()
	clusterInfo.AddLine("Cluster PVC information: (dumping all pvc under the namespace)")
	clusterInfo.AddLine("Available Cluster PVCCount", cluster.Status.PVCCount)
	clusterInfo.AddLine()
	clusterInfo.AddHeader("Items", "Values")
	for _, pvc := range pvcList.Items {
		clusterInfo.AddLine("PVC name", pvc.Name)
		clusterInfo.AddLine("PVC phase", pvc.Status.Phase)
		clusterInfo.AddLine("---", "---")
	}

	snapshotList, _ := storage.GetSnapshotList(ctx, crudClient, cluster.Namespace)
	clusterInfo.AddLine()
	clusterInfo.AddLine("Cluster Snapshot information: (dumping all snapshot under the namespace)")
	clusterInfo.AddLine()
	clusterInfo.AddHeader("Items", "Values")
	for _, snapshot := range snapshotList.Items {
		clusterInfo.AddLine("Snapshot name", snapshot.Name)
		if snapshot.Status.ReadyToUse != nil {
			clusterInfo.AddLine("Snapshot ready to use", *snapshot.Status.ReadyToUse)
		} else {
			clusterInfo.AddLine("Snapshot ready to use", "false")
		}
		clusterInfo.AddLine("---", "---")
	}

	// do not remove, this is needed to ensure that the writer cache is always flushed.
	clusterInfo.Print()

	return buffer.String()
}

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
