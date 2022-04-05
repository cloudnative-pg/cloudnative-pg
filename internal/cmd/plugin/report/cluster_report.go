/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package report

import (
	"archive/zip"
	"context"
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	cnpv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/plugin"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"
)

// clusterReport contains the data to be printed by the `report cluster` plugin
type clusterReport struct {
	cluster     cnpv1.Cluster
	clusterPods corev1.PodList
	clusterJobs batchv1.JobList
	events      corev1.EventList
}

// writeToZip adds the elements of the report to a zip as inner files
func (cr clusterReport) writeToZip(zipper *zip.Writer, format plugin.OutputFormat) error {
	objects := []struct {
		content interface{}
		name    string
	}{
		{content: cr.cluster, name: "cluster"},
		{content: cr.clusterPods, name: "cluster-pods"},
		{content: cr.clusterJobs, name: "cluster-jobs"},
		{content: cr.events, name: "events"},
	}

	for _, object := range objects {
		err := addContentToZip(object.content, object.name, zipper, format)
		if err != nil {
			return err
		}
	}

	return nil
}

// Cluster implements the "report cluster" subcommand
// Produces a zip file containing
//  - cluster pod and job definitions
//  - cluster resource (same content as `kubectl get cluster -o yaml`)
//  - events in the cluster namespace
func Cluster(ctx context.Context, clusterName, namespace string, format plugin.OutputFormat,
	file string,
) error {
	var events corev1.EventList
	err := plugin.Client.List(ctx, &events, client.InNamespace(namespace))
	if err != nil {
		return fmt.Errorf("could not get events: %w", err)
	}

	var cluster cnpv1.Cluster
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

	err = writeZippedReport(rep, format, file)
	if err != nil {
		return fmt.Errorf("could not write report: %w", err)
	}

	fmt.Printf("Successfully written report to \"%s\" (format: \"%s\")\n", file, format)

	return nil
}
