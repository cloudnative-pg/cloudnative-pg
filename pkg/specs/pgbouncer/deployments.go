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

// Package pgbouncer contains the specification of the K8s resources
// generated by the CloudNativePG operator related to pgbouncer poolers
package pgbouncer

import (
	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	config "github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/podspec"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils/hash"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// DefaultPgbouncerImage is the name of the pgbouncer image used by default
	DefaultPgbouncerImage = "ghcr.io/cloudnative-pg/pgbouncer:1.24.0"
)

// Deployment creates the deployment of Odyssey, given
// the configurations we have in the pooler specifications
func Deployment(pooler *apiv1.Pooler, cluster *apiv1.Cluster) (*appsv1.Deployment, error) {
	operatorImageName := config.Current.OperatorImageName

	poolerHash, err := computeTemplateHash(pooler, operatorImageName)
	if err != nil {
		return nil, err
	}

	image := "cr.yandex/crpiskgukqn7io35108q/odyssey:dev-adugin"
	const odysseyPort int32 = 6432

	podTemplate := podspec.NewFrom(pooler.Spec.Template).
		WithLabel(utils.PgbouncerNameLabel, pooler.Name).
		WithLabel(utils.ClusterLabelName, cluster.Name).
		WithLabel(utils.PodRoleLabelName, string(utils.PodRolePooler)).
		WithLabel("app", "odyssey").
		WithContainerImage("odyssey", image, false).
		WithContainerPort("odyssey", &corev1.ContainerPort{
			ContainerPort: odysseyPort,
		}).
		WithVolume(&corev1.Volume{
			Name: "config-volume",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "odyssey-config",
					},
				},
			},
		}).
		WithContainerVolumeMount("odyssey", &corev1.VolumeMount{
			Name:      "config-volume",
			MountPath: "/etc/odyssey/odyssey.conf",
			SubPath:   "odyssey.conf",
		}, false).
		Build()

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pooler.Name,
			Namespace: pooler.Namespace,
			Labels: map[string]string{
				utils.ClusterLabelName:   cluster.Name,
				utils.PgbouncerNameLabel: pooler.Name,
				utils.PodRoleLabelName:   string(utils.PodRolePooler),
			},
			Annotations: map[string]string{
				utils.PoolerSpecHashAnnotationName: poolerHash,
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: pooler.Spec.Instances,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					utils.PgbouncerNameLabel: pooler.Name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: podTemplate.ObjectMeta.Annotations,
					Labels:      podTemplate.ObjectMeta.Labels,
				},
				Spec: podTemplate.Spec,
			},
			Strategy: getDeploymentStrategy(pooler.Spec.DeploymentStrategy),
		},
	}, nil
}

func computeTemplateHash(pooler *apiv1.Pooler, operatorImageName string) (string, error) {
	type deploymentHash struct {
		poolerSpec                      apiv1.PoolerSpec
		operatorImageName               string
		isPodSpecReconciliationDisabled bool
	}

	return hash.ComputeHash(deploymentHash{
		poolerSpec:                      pooler.Spec,
		operatorImageName:               operatorImageName,
		isPodSpecReconciliationDisabled: utils.IsPodSpecReconciliationDisabled(&pooler.ObjectMeta),
	})
}

func getDeploymentStrategy(strategy *appsv1.DeploymentStrategy) appsv1.DeploymentStrategy {
	if strategy != nil {
		return *strategy.DeepCopy()
	}
	return appsv1.DeploymentStrategy{}
}
