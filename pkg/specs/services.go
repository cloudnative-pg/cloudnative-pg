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

package specs

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/servicespec"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

func buildInstanceServicePorts() []corev1.ServicePort {
	return []corev1.ServicePort{
		{
			Name:       PostgresContainerName,
			Protocol:   corev1.ProtocolTCP,
			TargetPort: intstr.FromString(PostgresPortName),
			Port:       postgres.ServerPort,
		},
	}
}

// CreateClusterAnyService create a service insisting on all the pods
func CreateClusterAnyService(cluster apiv1.Cluster) *corev1.Service {
	version, _ := cluster.GetPostgresqlMajorVersion()

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.GetServiceAnyName(),
			Namespace: cluster.Namespace,
			Labels: map[string]string{
				utils.ClusterLabelName:                cluster.Name,
				utils.KubernetesAppLabelName:          utils.AppName,
				utils.KubernetesAppInstanceLabelName:  cluster.Name,
				utils.KubernetesAppVersionLabelName:   fmt.Sprint(version),
				utils.KubernetesAppComponentLabelName: utils.DatabaseComponentName,
				utils.KubernetesAppManagedByLabelName: utils.ManagerName,
			},
		},
		Spec: corev1.ServiceSpec{
			Type:                     corev1.ServiceTypeClusterIP,
			PublishNotReadyAddresses: true,
			Ports:                    buildInstanceServicePorts(),
			Selector: map[string]string{
				utils.ClusterLabelName: cluster.Name,
				utils.PodRoleLabelName: string(utils.PodRoleInstance),
			},
		},
	}
}

// CreateClusterReadService create a service insisting on all the ready pods
func CreateClusterReadService(cluster apiv1.Cluster) *corev1.Service {
	version, _ := cluster.GetPostgresqlMajorVersion()

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.GetServiceReadName(),
			Namespace: cluster.Namespace,
			Labels: map[string]string{
				utils.ClusterLabelName:                cluster.Name,
				utils.KubernetesAppLabelName:          utils.AppName,
				utils.KubernetesAppInstanceLabelName:  cluster.Name,
				utils.KubernetesAppVersionLabelName:   fmt.Sprint(version),
				utils.KubernetesAppComponentLabelName: utils.DatabaseComponentName,
				utils.KubernetesAppManagedByLabelName: utils.ManagerName,
			},
		},
		Spec: corev1.ServiceSpec{
			Type:  corev1.ServiceTypeClusterIP,
			Ports: buildInstanceServicePorts(),
			Selector: map[string]string{
				utils.ClusterLabelName: cluster.Name,
				utils.PodRoleLabelName: string(utils.PodRoleInstance),
			},
		},
	}
}

// CreateClusterReadOnlyService create a service insisting on all the ready pods
func CreateClusterReadOnlyService(cluster apiv1.Cluster) *corev1.Service {
	version, _ := cluster.GetPostgresqlMajorVersion()

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.GetServiceReadOnlyName(),
			Namespace: cluster.Namespace,
			Labels: map[string]string{
				utils.ClusterLabelName:                cluster.Name,
				utils.KubernetesAppLabelName:          utils.AppName,
				utils.KubernetesAppInstanceLabelName:  cluster.Name,
				utils.KubernetesAppVersionLabelName:   fmt.Sprint(version),
				utils.KubernetesAppComponentLabelName: utils.DatabaseComponentName,
				utils.KubernetesAppManagedByLabelName: utils.ManagerName,
			},
		},
		Spec: corev1.ServiceSpec{
			Type:  corev1.ServiceTypeClusterIP,
			Ports: buildInstanceServicePorts(),
			Selector: map[string]string{
				utils.ClusterLabelName:             cluster.Name,
				utils.ClusterInstanceRoleLabelName: ClusterRoleLabelReplica,
			},
		},
	}
}

// CreateClusterReadWriteService create a service insisting on the primary pod
func CreateClusterReadWriteService(cluster apiv1.Cluster) *corev1.Service {
	version, _ := cluster.GetPostgresqlMajorVersion()

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.GetServiceReadWriteName(),
			Namespace: cluster.Namespace,
			Labels: map[string]string{
				utils.ClusterLabelName:                cluster.Name,
				utils.KubernetesAppLabelName:          utils.AppName,
				utils.KubernetesAppInstanceLabelName:  cluster.Name,
				utils.KubernetesAppVersionLabelName:   fmt.Sprint(version),
				utils.KubernetesAppComponentLabelName: utils.DatabaseComponentName,
				utils.KubernetesAppManagedByLabelName: utils.ManagerName,
			},
		},
		Spec: corev1.ServiceSpec{
			Type:  corev1.ServiceTypeClusterIP,
			Ports: buildInstanceServicePorts(),
			Selector: map[string]string{
				utils.ClusterLabelName:             cluster.Name,
				utils.ClusterInstanceRoleLabelName: ClusterRoleLabelPrimary,
			},
		},
	}
}

// BuildManagedServices creates a list of Kubernetes Services based on the
// additional managed services specified in the Cluster's ManagedServices configuration.
// Returns:
// - []corev1.Service: a slice of Service objects created from the managed services configuration.
// - error: an error if the creation of any service fails, otherwise nil.
//
// Example usage:
//
//	services, err := BuildManagedServices(cluster)
//
//	if err != nil {
//	    // handle error
//	}
//
//	for idx := range services {
//	    // use the created services
//	}
func BuildManagedServices(cluster apiv1.Cluster) ([]corev1.Service, error) {
	if cluster.Spec.Managed == nil || cluster.Spec.Managed.Services == nil {
		return nil, nil
	}

	managedServices := cluster.Spec.Managed.Services
	if len(managedServices.Additional) == 0 {
		return nil, nil
	}

	services := make([]corev1.Service, len(managedServices.Additional))

	for i := range managedServices.Additional {
		serviceConfiguration := managedServices.Additional[i]
		defaultService, err := buildDefaultService(cluster, serviceConfiguration)
		if err != nil {
			return nil, err
		}
		builder := servicespec.NewFrom(&serviceConfiguration.ServiceTemplate).
			WithServiceType(defaultService.Spec.Type, false).
			WithLabel(utils.IsManagedLabelName, "true").
			WithAnnotation(utils.UpdateStrategyAnnotation, string(serviceConfiguration.UpdateStrategy)).
			SetSelectors(defaultService.Spec.Selector)

		for idx := range defaultService.Spec.Ports {
			// we preserve the user settings over the default configuration, issue: #6389
			builder = builder.WithServicePortNoOverwrite(&defaultService.Spec.Ports[idx])
		}

		for key, value := range defaultService.Labels {
			builder = builder.WithLabel(key, value)
		}

		for key, value := range defaultService.Annotations {
			builder = builder.WithAnnotation(key, value)
		}

		serviceTemplate := builder.Build()
		services[i] = corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:        serviceTemplate.ObjectMeta.Name,
				Namespace:   cluster.Namespace,
				Labels:      serviceTemplate.ObjectMeta.Labels,
				Annotations: serviceTemplate.ObjectMeta.Annotations,
			},
			Spec: serviceTemplate.Spec,
		}
		cluster.SetInheritedDataAndOwnership(&services[i].ObjectMeta)
	}

	return services, nil
}

func buildDefaultService(cluster apiv1.Cluster, serviceConf apiv1.ManagedService) (*corev1.Service, error) {
	switch serviceConf.SelectorType {
	case apiv1.ServiceSelectorTypeRO:
		return CreateClusterReadOnlyService(cluster), nil
	case apiv1.ServiceSelectorTypeRW:
		return CreateClusterReadWriteService(cluster), nil
	case apiv1.ServiceSelectorTypeR:
		return CreateClusterReadService(cluster), nil
	default:
		return nil, fmt.Errorf("unknown service type: %s", serviceConf.SelectorType)
	}
}
