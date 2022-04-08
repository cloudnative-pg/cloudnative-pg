/*
Copyright 2019-2022 The CloudNativePG Contributors

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

// Package maintenance implements the kubectl-cnp maintenance sub-command
package maintenance

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/cheynewallace/tabby"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"

	v1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

// Maintenance command implementation
func Maintenance(ctx context.Context,
	allNamespaces, reusePVC, confirmationRequired bool,
	clusterName string,
	setInProgressTo bool,
) error {
	clusters := tabby.New()
	clusters.AddLine("The following are the new values for the clusters")
	clusters.AddHeader(
		"Namespace",
		"Cluster Name",
		"Maintenance",
		"reusePVC")

	var clusterList v1.ClusterList
	var err error
	if allNamespaces || clusterName == "" {
		clusterList, err = getClusters(ctx, allNamespaces)
	} else {
		clusterList, err = getCluster(ctx, clusterName)
	}
	if err != nil {
		return err
	}

	if len(clusterList.Items) == 0 {
		if clusterName != "" {
			return fmt.Errorf("cluster '%v' couldn't be found", clusterName)
		}
		return fmt.Errorf("no cluster could be listed or no permission to list clusters")
	}

	for _, item := range clusterList.Items {
		clusters.AddLine(
			item.Namespace,
			item.Name,
			fmt.Sprintf("%v => %v", item.IsNodeMaintenanceWindowInProgress(), setInProgressTo),
			fmt.Sprintf("%v => %v", item.IsReusePVCEnabled(), reusePVC),
		)
	}

	clusters.Print()
	if confirmationRequired {
		proceed := askToProceed()
		if !proceed {
			return nil
		}
	}

	for _, item := range clusterList.Items {
		err := patchNodeMaintenanceWindow(ctx, item, setInProgressTo, reusePVC)
		if err != nil {
			return fmt.Errorf("unable to set progress to cluster %v in namespace %v", item.Name, item.Namespace)
		}
	}

	return nil
}

func askToProceed() bool {
	fmt.Printf("Do you want to proceed? [y/n]: ")
	reader := bufio.NewReader(os.Stdin)
	answer, err := reader.ReadString('\n')
	if err != nil {
		return false
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	if answer == "y" || answer == "yes" {
		return true
	}
	return false
}

func getClusters(ctx context.Context, allNamespaces bool) (v1.ClusterList, error) {
	var clusterList v1.ClusterList
	var opts []client.ListOption
	if !allNamespaces {
		opts = append(opts, client.InNamespace(plugin.Namespace))
	}
	err := plugin.Client.List(ctx, &clusterList, opts...)
	return clusterList, err
}

func getCluster(ctx context.Context, clusterName string) (v1.ClusterList, error) {
	var clusterList v1.ClusterList
	var cluster v1.Cluster
	err := plugin.Client.Get(ctx, client.ObjectKey{Namespace: plugin.Namespace, Name: clusterName}, &cluster)
	if err == nil {
		clusterList.Items = append(clusterList.Items, cluster)
	}
	return clusterList, err
}

func patchNodeMaintenanceWindow(
	ctx context.Context,
	cluster v1.Cluster,
	inProgress, reusePVC bool,
) error {
	maintenanceCluster := cluster.DeepCopy()

	if maintenanceCluster.Spec.NodeMaintenanceWindow == nil {
		maintenanceCluster.Spec.NodeMaintenanceWindow = &v1.NodeMaintenanceWindow{}
	}
	maintenanceCluster.Spec.NodeMaintenanceWindow.InProgress = inProgress
	maintenanceCluster.Spec.NodeMaintenanceWindow.ReusePVC = &reusePVC

	err := plugin.Client.Patch(ctx, maintenanceCluster, client.MergeFrom(&cluster))
	if err != nil {
		return err
	}
	return nil
}
