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
	"errors"
	"fmt"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"k8s.io/apimachinery/pkg/labels"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// GetPodMonitor gathers the current PodMonitor for some specified criteria
func (env TestingEnvironment) GetPodMonitor(labelSelector map[string]string) (*monitoringv1.PodMonitor, error) {
	podMonitor := &monitoringv1.PodMonitorList{}
	labelSelectorList := labels.SelectorFromSet(labelSelector)

	err := GetObjectList(&env, podMonitor, ctrlclient.MatchingLabelsSelector{Selector: labelSelectorList})
	if err != nil {
		return nil, err
	}

	if len(podMonitor.Items) == 0 {
		return nil, errors.New(fmt.Sprintf("PodMonitor not found: %v", podMonitor.Items))
	}
	return podMonitor.Items[0], nil
}
