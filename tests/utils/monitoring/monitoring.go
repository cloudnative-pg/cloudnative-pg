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

// Package monitoring provides a functions to handle the podmonitor CRD
package monitoring

import (
	"context"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/objects"
)

// GetPodMonitor gathers the current PodMonitor in a namespace
func GetPodMonitor(
	ctx context.Context,
	crudClient client.Client,
	namespace, name string,
) (*monitoringv1.PodMonitor, error) {
	podMonitor := &monitoringv1.PodMonitor{}
	namespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}

	err := objects.GetObject(ctx, crudClient, namespacedName, podMonitor)
	if err != nil {
		return nil, err
	}
	return podMonitor, nil
}
