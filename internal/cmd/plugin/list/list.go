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

package list

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cheynewallace/tabby"
	v1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
	apiLabels "k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// FIXME(till): could unify some of this with the maintenance plugin
func List(ctx context.Context, allNamespaces bool, labels string, output plugin.OutputFormat) error {
	clusterList, err := getClusters(ctx, allNamespaces, labels)
	if err != nil {
		return err
	}

	if len(clusterList.Items) == 0 {
		return fmt.Errorf("no clusters were found or no permission to list clusters")
	}

	if output == plugin.OutputFormatJSON {
		j, _ := json.MarshalIndent(clusterList.Items, "", "")
		fmt.Println(j)
		return nil
	}

	clusters := tabby.New()
	clusters.AddLine("The following clusters are created")
	clusters.AddHeader(
		"Namespace",
		"Cluster Name",
		"Labels",
	)

	for _, item := range clusterList.Items {
		clusters.AddLine(
			item.Namespace,
			item.Name,
			item.Labels,
		)
	}

	clusters.Print()
	return nil
}

func getClusters(ctx context.Context, allNamespaces bool, labels string) (v1.ClusterList, error) {
	var clusterList v1.ClusterList
	var opts []client.ListOption
	if !allNamespaces {
		opts = append(opts, client.InNamespace(plugin.Namespace))
	}

	if labels != "" {
		selector, err := apiLabels.Parse(labels)
		if err != nil {
			return clusterList, err
		}
		opts = append(opts, client.MatchingLabelsSelector{Selector: selector})
	}

	err := plugin.Client.List(ctx, &clusterList, opts...)
	return clusterList, err
}
