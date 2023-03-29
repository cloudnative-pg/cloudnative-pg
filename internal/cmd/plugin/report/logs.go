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

package report

import (
	"archive/zip"
	"context"
	"fmt"
	"path/filepath"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils/logs"
)

const jobMatcherLabel = "job-name"

var podLogOptions = &corev1.PodLogOptions{
	Timestamps: true, // NOTE: when activated, lines are no longer JSON
}

// streamPodLogsToZip streams the pod logs to a new section in the ZIP
func streamPodLogsToZip(
	ctx context.Context,
	pods []corev1.Pod,
	dirName, name string,
	zipper *zip.Writer,
) error {
	logsDir := filepath.Join(dirName, name)
	if _, err := zipper.Create(logsDir + "/"); err != nil {
		return fmt.Errorf("could not add '%s' to zip: %w", logsDir, err)
	}

	for i := range pods {
		pod := pods[i]
		path := filepath.Join(logsDir, fmt.Sprintf("%s-logs.jsonl", pod.Name))
		writer, zipperErr := zipper.Create(path)
		if zipperErr != nil {
			return fmt.Errorf("could not add '%s' to zip: %w", path, zipperErr)
		}

		streamPodLogs := &logs.StreamPodLog{
			Pod:      &pod,
			Writer:   writer,
			Options:  podLogOptions,
			Previous: true,
		}
		fmt.Fprint(streamPodLogs.Writer, "\n====== Begin of Previous Log =====\n")
		_ = streamPodLogs.StreamPodLogs(ctx)
		fmt.Fprint(streamPodLogs.Writer, "\n====== End of Previous Log =====\n")

		streamPodLogs.Previous = false
		if err := streamPodLogs.StreamPodLogs(ctx); err != nil {
			return err
		}
	}

	return nil
}

// streamClusterLogsToZip streams the logs from the pods in the cluster, one by
// one, each in a new file, within  a folder
func streamClusterLogsToZip(ctx context.Context, clusterName, namespace string,
	dirname string, zipper *zip.Writer,
) error {
	logsdir := filepath.Join(dirname, "logs")
	_, err := zipper.Create(logsdir + "/")
	if err != nil {
		return fmt.Errorf("could not add '%s' to zip: %w", logsdir, err)
	}

	matchClusterName := client.MatchingLabels{
		utils.ClusterLabelName: clusterName,
	}

	var podList corev1.PodList
	err = plugin.Client.List(ctx, &podList, matchClusterName, client.InNamespace(namespace))
	if err != nil {
		return fmt.Errorf("could not get cluster pods: %w", err)
	}
	streamPodLogs := &logs.StreamPodLog{
		Options:  podLogOptions,
		Previous: false,
	}

	for _, pod := range podList.Items {
		writer, err := zipper.Create(filepath.Join(logsdir, pod.Name) + ".jsonl")
		if err != nil {
			return fmt.Errorf("could not add '%s' to zip: %w",
				filepath.Join(logsdir, pod.Name), err)
		}
		podPointer := pod
		streamPodLogs.Pod = &podPointer
		streamPodLogs.Writer = writer

		err = streamPodLogs.StreamPodLogs(ctx)
		if err != nil {
			return err
		}
	}

	return nil
}

// streamClusterJobLogsToZip checks for jobs in the cluster, and streams
// the logs from the pods created by those jobs, one by one, each in a new file
func streamClusterJobLogsToZip(ctx context.Context, clusterName, namespace string,
	dirname string, zipper *zip.Writer,
) error {
	logsdir := filepath.Join(dirname, "job-logs")
	_, err := zipper.Create(logsdir + "/")
	if err != nil {
		return fmt.Errorf("could not add '%s' to zip: %w", logsdir, err)
	}

	matchClusterName := client.MatchingLabels{
		utils.ClusterLabelName: clusterName,
	}

	var jobList batchv1.JobList
	err = plugin.Client.List(ctx, &jobList, matchClusterName, client.InNamespace(namespace))
	if err != nil {
		return fmt.Errorf("could not get cluster jobs: %w", err)
	}

	for _, job := range jobList.Items {
		matchJobName := client.MatchingLabels{
			jobMatcherLabel: job.Name,
		}
		var podList corev1.PodList
		err = plugin.Client.List(ctx, &podList, matchJobName, client.InNamespace(namespace))
		if err != nil {
			return fmt.Errorf("could not get pods for job '%s': %w", job.Name, err)
		}

		streamPodLogs := &logs.StreamPodLog{
			Options:  podLogOptions,
			Previous: false,
		}
		for _, pod := range podList.Items {
			writer, err := zipper.Create(filepath.Join(logsdir, pod.Name) + ".jsonl")
			if err != nil {
				return fmt.Errorf("could not add '%s' to zip: %w",
					filepath.Join(logsdir, pod.Name), err)
			}
			podPointer := pod
			streamPodLogs.Pod = &podPointer
			streamPodLogs.Writer = writer
			err = streamPodLogs.StreamPodLogs(ctx)
			if err != nil {
				return err
			}
		}
	}

	return nil
}
