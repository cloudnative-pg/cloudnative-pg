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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
)

// getOlmResource gets the desired resource using the Dynamic Client
// to avoid code dependencies on the OLM libraries
func getOlmResource(
	ctx context.Context, dynamicClient dynamic.Interface, namespace, item string,
) (client.ObjectList, error) {
	resource := schema.GroupVersionResource{
		Group:    "operators.coreos.com",
		Version:  "v1alpha1",
		Resource: item,
	}

	list, err := dynamicClient.Resource(resource).Namespace(namespace).
		List(ctx, metav1.ListOptions{LabelSelector: labelOperatorKeyPrefix + "openshift-operators"})
	if err != nil {
		return nil, fmt.Errorf("could note get resource: %v, %v", resource, err)
	}

	list, err = dynamicClient.Resource(resource).Namespace(namespace).
		List(ctx, metav1.ListOptions{LabelSelector: labelOperatorKeyPrefix + plugin.Namespace})
	if err != nil {
		return nil, fmt.Errorf("could note get resource: %v, %v", resource, err)
	}
	return list, err
}

// olmOperatorReport contains the operator data in Oolm
// to be printed by the `report operator` plugin
type olmOperatorReport map[string]client.ObjectList

// getOlmReport builds the olm operator report
func getOlmReport(ctx context.Context, namespace string) (olmOperatorReport, error) {
	items := []string{"clusterserviceversions", "installplans", "subscriptions"}
	operatorReport := make(olmOperatorReport)

	client, err := dynamic.NewForConfig(plugin.Config)
	if err != nil {
		return nil, fmt.Errorf("could not get dynamic client: %w", err)
	}

	for _, item := range items {
		resource, err := getOlmResource(ctx, client, namespace, item)
		if err != nil {
			return nil, fmt.Errorf("could not build report. Failed on item %s: %w", item, err)
		}
		operatorReport[item] = resource
	}
	return operatorReport, nil
}

// writeToZip makes a new section in the ZIP file, and adds in it various
// Kubernetes object manifests
func (olmReport olmOperatorReport) writeToZip(
	zipper *zip.Writer, format plugin.OutputFormat, folder string,
) error {
	newFolder := filepath.Join(folder, "olm")
	_, err := zipper.Create(newFolder + "/")
	if err != nil {
		return err
	}

	for key, value := range olmReport {
		err = addContentToZip(value, key, newFolder, format, zipper)
		if err != nil {
			return err
		}
	}
	return nil
}
