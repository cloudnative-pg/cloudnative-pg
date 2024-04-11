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

package specs

import (
	"context"
	"fmt"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/client-go/discovery"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type PodMonitorManager interface {
	IsPodMonitorEnabled() bool
	BuildPodMonitor() *monitoringv1.PodMonitor
}

type PodMonitorManagerController struct {
	Manager   PodMonitorManager
	Ctx       context.Context
	Discovery discovery.DiscoveryInterface
	Client    client.Client
}

func (p PodMonitorManagerController) AssertPodMonitorFunctionality() (bool, error) {
	havePodMonitorCRD, err := utils.PodMonitorExist(p.Discovery)
	if err != nil {
		return false, err
	}

	if !havePodMonitorCRD && p.Manager.IsPodMonitorEnabled() {
		return false, nil
	}

	return true, nil
}

func (p PodMonitorManagerController) CreateOrPatchPodMonitor() error {
	contextLogger := log.FromContext(p.Ctx)

	havePodMonitorCRD, err := p.AssertPodMonitorFunctionality()
	if err != nil {
		return err
	} else if !havePodMonitorCRD {
		contextLogger.Warning("PodMonitor CRD not present. Cannot create the PodMonitor object")
		return nil
	}

	// Build expected PodMonitor
	expectedPodMonitor := p.Manager.BuildPodMonitor()
	expectedPodMonitorString := MarshalPodMonitor(*expectedPodMonitor)
	contextLogger.Debug(fmt.Sprintf("Expected PodMonitor is: %s", expectedPodMonitorString))

	// We get the current pod monitor
	podMonitor := &monitoringv1.PodMonitor{}
	if err := p.Client.Get(
		p.Ctx,
		client.ObjectKeyFromObject(expectedPodMonitor),
		podMonitor,
	); err != nil {
		if !apierrs.IsNotFound(err) {
			return fmt.Errorf("while getting the podmonitor: %w", err)
		}
		podMonitor = nil
	}

	if !p.Manager.IsPodMonitorEnabled() && podMonitor != nil {
		// `PodMonitor` is disabled but it still exists
		contextLogger.Info("Deleting PodMonitor")
		if err := p.Client.Delete(p.Ctx, podMonitor); err != nil {
			if !apierrs.IsNotFound(err) {
				return err
			}
		}
	} else if p.Manager.IsPodMonitorEnabled() && podMonitor == nil {
		// `PodMonitor` is enabled, but it still not yet reconciled
		contextLogger.Info("Creating PodMonitor")
		if err := p.Client.Create(p.Ctx, expectedPodMonitor); err != nil {
			return err
		}
	} else if podMonitor != nil {
		// PodMonitor exists, and we fall back to not monitoring status change
		origPodMonitor := podMonitor.DeepCopy()
		podMonitor.Spec = expectedPodMonitor.Spec
		// We don't override the current labels/annotations given that there could be data that isn't managed by us
		utils.MergeObjectsMetadata(podMonitor, expectedPodMonitor)

		// If there's no changes we are done
		if reflect.DeepEqual(origPodMonitor, podMonitor) {
			return nil
		}

		// Patch the PodMonitor, so we always reconcile it with the cluster changes
		contextLogger.Debug("Patching PodMonitor")
		return p.Client.Patch(p.Ctx, podMonitor, client.MergeFrom(origPodMonitor))
	}

	// No operation needed
	return nil
}

func MarshalPodMonitor(monitor monitoringv1.PodMonitor) string {
	data, err := json.Marshal(monitor)
	if err != nil {
		return ""
	}

	returnData := fmt.Sprintf("%s", data)
	return returnData
}
