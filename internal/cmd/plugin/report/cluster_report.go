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
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// clusterReport contains the data to be printed by the `report cluster` plugin
type clusterReport struct {
	cluster     cnpgv1.Cluster
	clusterPods corev1.PodList
	clusterJobs batchv1.JobList
	events      corev1.EventList
}

// writeToZip makes a new section in the ZIP file, and adds in it various
// Kubernetes object manifests
func (cr clusterReport) writeToZip(zipper *zip.Writer, format plugin.OutputFormat, folder string) error {
	objects := []struct {
		content interface{}
		name    string
	}{
		{content: cr.cluster, name: "cluster"},
		{content: cr.clusterPods, name: "cluster-pods"},
		{content: cr.clusterJobs, name: "cluster-jobs"},
		{content: cr.events, name: "events"},
	}

	newFolder := filepath.Join(folder, "manifests")
	_, err := zipper.Create(newFolder + "/")
	if err != nil {
		return err
	}

	for _, object := range objects {
		err := addContentToZip(object.content, object.name, newFolder, format, zipper)
		if err != nil {
			return err
		}
	}

	return nil
}

// cluster implements the "report cluster" subcommand
// Produces a zip file containing
//   - cluster pod and job definitions
//   - cluster resource (same content as `kubectl get cluster -o yaml`)
//   - events in the cluster namespace
//   - logs from the cluster pods (optional - activated with `includeLogs`)
//   - logs from the cluster jobs (optional - activated with `includeLogs`)
func cluster(ctx context.Context, clusterName, namespace string, format plugin.OutputFormat,
	file string, includeLogs, logTimeStamp bool, timestamp time.Time,
) error {
	var events corev1.EventList
	err := plugin.Client.List(ctx, &events, client.InNamespace(namespace))
	if err != nil {
		return fmt.Errorf("could not get events: %w", err)
	}

	var cluster cnpgv1.Cluster
	err = plugin.Client.Get(ctx,
		types.NamespacedName{Namespace: namespace, Name: clusterName},
		&cluster)
	if err != nil {
		return fmt.Errorf("could not get cluster: %w", err)
	}

	matchClusterName := client.MatchingLabels{
		utils.ClusterLabelName: clusterName,
	}

	var pods corev1.PodList
	err = plugin.Client.List(ctx, &pods, matchClusterName, client.InNamespace(namespace))
	if err != nil {
		return fmt.Errorf("could not get cluster pods: %w", err)
	}

	var jobs batchv1.JobList
	err = plugin.Client.List(ctx, &jobs, matchClusterName, client.InNamespace(namespace))
	if err != nil {
		return fmt.Errorf("could not get cluster jobs: %w", err)
	}

	rep := clusterReport{
		events:      events,
		cluster:     cluster,
		clusterPods: pods,
		clusterJobs: jobs,
	}

	reportZipper := func(zipper *zip.Writer, dirname string) error {
		return rep.writeToZip(zipper, format, dirname)
	}

	sections := []zipFileWriter{reportZipper}

	if includeLogs {
		logsZipper := func(zipper *zip.Writer, dirname string) error {
			return streamClusterLogsToZip(ctx, clusterName, plugin.Namespace, dirname, logTimeStamp, zipper)
		}

		jobLogsZipper := func(zipper *zip.Writer, dirname string) error {
			return streamClusterJobLogsToZip(ctx, clusterName, plugin.Namespace, dirname, logTimeStamp, zipper)
		}

		sections = append(sections, logsZipper, jobLogsZipper)
	}

	err = writeZippedReport(sections, file, reportName("cluster", timestamp, clusterName))
	if err != nil {
		return fmt.Errorf("could not write report: %w", err)
	}

	fmt.Printf("Successfully written report to \"%s\" (format: \"%s\")\n", file, format)

	return nil
}
