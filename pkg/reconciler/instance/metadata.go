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

package instance

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// ReconcileMetadata ensures that the instance metadata is kept up to date
func ReconcileMetadata(
	ctx context.Context,
	cli client.Client,
	cluster *apiv1.Cluster,
	instances corev1.PodList,
) error {
	contextLogger := log.FromContext(ctx)

	for idx := range instances.Items {
		origInstance := instances.Items[idx].DeepCopy()
		instance := &instances.Items[idx]

		modified := updateRoleLabels(ctx, cluster, instance) ||
			updateOperatorLabels(ctx, instance) ||
			updateClusterLabels(ctx, cluster, instance) ||
			updateClusterAnnotations(ctx, cluster, instance)

		if !modified {
			continue
		}

		if err := cli.Patch(ctx, instance, client.MergeFrom(origInstance)); err != nil {
			contextLogger.Error(
				err,
				"while patching instance metadata",
				"instanceName", origInstance.Name,
			)
			return fmt.Errorf("cannot updated metadata on pods: %w", err)
		}
	}

	return nil
}

// updateClusterAnnotations checks if there are annotations specified in the cluster that are
// not present in the pods, and if so applies them.
// We do not support the case of removed annotations from the cluster resource.
//
// Returns true iff the instance needed updating
func updateClusterAnnotations(
	ctx context.Context,
	cluster *apiv1.Cluster,
	instance *corev1.Pod,
) bool {
	contextLogger := log.FromContext(ctx)

	// if all the required annotations are already set and with the correct value,
	// we are done
	if utils.IsAnnotationSubset(instance.Annotations, cluster.Annotations, cluster.GetFixedInheritedAnnotations(),
		configuration.Current) &&
		utils.IsAnnotationAppArmorPresentInObject(&instance.ObjectMeta, cluster.Annotations) {
		contextLogger.Debug(
			"Skipping cluster annotations reconciliation, because they are already present on pod",
			"pod", instance.Name,
			"podAnnotations", instance.Annotations,
			"clusterAnnotations", cluster.Annotations,
		)
		return false
	}

	// otherwise, we add the modified/new annotations to the pod
	contextLogger.Info("Updating cluster annotations on pod", "pod", instance.Name)
	utils.InheritAnnotations(&instance.ObjectMeta, cluster.Annotations,
		cluster.GetFixedInheritedAnnotations(), configuration.Current)
	if utils.IsAnnotationAppArmorPresent(cluster.Annotations) {
		utils.AnnotateAppArmor(&instance.ObjectMeta, cluster.Annotations)
	}

	return true
}

// updateClusterLabels checks if there are labels in the cluster that are
// not present in the pods, and if so applies them.
// We do not support the case of removed labels from the cluster resource.
//
// Returns true iff the instance needed updating
func updateClusterLabels(
	ctx context.Context,
	cluster *apiv1.Cluster,
	instance *corev1.Pod,
) bool {
	contextLogger := log.FromContext(ctx)

	// if all the required labels are already set and with the correct value,
	// there's nothing more to do
	if utils.IsLabelSubset(instance.Labels, cluster.Labels, cluster.GetFixedInheritedLabels(),
		configuration.Current) {
		contextLogger.Debug(
			"Skipping cluster label reconciliation, because they are already present on pod",
			"pod", instance.Name,
			"podLabels", instance.Labels,
			"clusterLabels", cluster.Labels,
		)
		return false
	}

	// otherwise, we add the modified/new labels to the pod
	contextLogger.Info("Updating cluster labels on pod", "pod", instance.Name)
	utils.InheritLabels(&instance.ObjectMeta, cluster.Labels, cluster.GetFixedInheritedLabels(), configuration.Current)
	return true
}

// Make sure that primary and replicas are correctly labelled as such
//
// Returns true iff the instance needed updating
func updateRoleLabels(
	ctx context.Context,
	cluster *apiv1.Cluster,
	instance *corev1.Pod,
) bool {
	contextLogger := log.FromContext(ctx)

	// No current primary, no work to do
	if cluster.Status.CurrentPrimary == "" {
		return false
	}

	if !utils.IsPodActive(*instance) {
		contextLogger.Info("Ignoring not active Pod during label update",
			"pod", instance.Name, "status", instance.Status)
		return false
	}

	podRole, hasRole := instance.ObjectMeta.Labels[specs.ClusterRoleLabelName]

	switch {
	case instance.Name == cluster.Status.CurrentPrimary:
		if !hasRole || podRole != specs.ClusterRoleLabelPrimary {
			contextLogger.Info("Setting primary label", "pod", instance.Name)
			instance.Labels[specs.ClusterRoleLabelName] = specs.ClusterRoleLabelPrimary
			return true
		}

	default:
		if !hasRole || podRole != specs.ClusterRoleLabelReplica {
			contextLogger.Info("Setting replica label", "pod", instance.Name)
			instance.Labels[specs.ClusterRoleLabelName] = specs.ClusterRoleLabelReplica
			return true
		}
	}

	return false
}

// updateOperatorLabels ensures that the instances are labelled as instances,
// and have the correct instance name
//
// Returns true iff the instance needed updating
func updateOperatorLabels(
	ctx context.Context,
	instance *corev1.Pod,
) bool {
	contextLogger := log.FromContext(ctx)

	if instance.Labels == nil {
		instance.Labels = map[string]string{}
	}

	var modified bool
	if instance.Labels[utils.InstanceNameLabelName] != instance.Name {
		contextLogger.Info("Setting instance label name", "pod", instance.Name)
		instance.Labels[utils.InstanceNameLabelName] = instance.Name
		modified = true
	}

	if instance.Labels[utils.PodRoleLabelName] != string(utils.PodRoleInstance) {
		contextLogger.Info("Setting pod role label name", "pod", instance.Name)
		instance.Labels[utils.PodRoleLabelName] = string(utils.PodRoleInstance)
		modified = true
	}

	return modified
}
