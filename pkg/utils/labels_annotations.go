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
	"reflect"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/api/v1/resources"
)

type annotationStatus string

const (
	annotationStatusDisabled annotationStatus = "disabled"
	annotationStatusEnabled  annotationStatus = "enabled"
)

// PodRole describes the Role of a given pod
type PodRole string

const (
	// PodRoleInstance the label value indicating an instance
	PodRoleInstance PodRole = "instance"
	// PodRolePooler the label value indicating a pooler instance
	PodRolePooler PodRole = "pooler"
)

// LabelClusterName labels the object with the cluster name
func LabelClusterName(object *metav1.ObjectMeta, name string) {
	if object.Labels == nil {
		object.Labels = make(map[string]string)
	}

	object.Labels[resources.ClusterLabelName] = name
}

// SetOperatorVersion set inside a certain object metadata the annotation
// containing the version of the operator that generated the object
func SetOperatorVersion(object *metav1.ObjectMeta, version string) {
	if object.Annotations == nil {
		object.Annotations = make(map[string]string)
	}

	object.Annotations[resources.OperatorVersionAnnotationName] = version
}

// InheritanceController controls if a label or an annotation should be
// inherited
type InheritanceController interface {
	// IsAnnotationInherited checks if a certain annotation should be
	// inherited
	IsAnnotationInherited(name string) bool

	// IsLabelInherited checks if a certain label should be
	// inherited
	IsLabelInherited(name string) bool
}

// InheritAnnotations puts into the object metadata the passed annotations if
// the annotations are supposed to be inherited. The passed configuration is
// used to determine whenever a certain annotation is inherited or not
func InheritAnnotations(
	object *metav1.ObjectMeta,
	annotations map[string]string,
	fixedAnnotations map[string]string,
	controller InheritanceController,
) {
	if object.Annotations == nil {
		object.Annotations = make(map[string]string)
	}

	for key, value := range fixedAnnotations {
		object.Annotations[key] = value
	}

	for key, value := range annotations {
		if controller.IsAnnotationInherited(key) {
			object.Annotations[key] = value
		}
	}
}

// InheritLabels puts into the object metadata the passed labels if
// the labels are supposed to be inherited. The passed configuration is
// used to determine whenever a certain label is inherited or not
func InheritLabels(
	object *metav1.ObjectMeta,
	labels map[string]string,
	fixedLabels map[string]string,
	controller InheritanceController,
) {
	if object.Labels == nil {
		object.Labels = make(map[string]string)
	}

	for key, value := range fixedLabels {
		object.Labels[key] = value
	}

	for key, value := range labels {
		if controller.IsLabelInherited(key) {
			object.Labels[key] = value
		}
	}
}

func getAnnotationAppArmor(spec *corev1.PodSpec, annotations map[string]string) map[string]string {
	containsContainerWithName := func(name string, containers ...corev1.Container) bool {
		for _, container := range containers {
			if container.Name == name {
				return true
			}
		}

		return false
	}

	appArmorAnnotations := make(map[string]string)
	for annotation, value := range annotations {
		if strings.HasPrefix(annotation, resources.AppArmorAnnotationPrefix) {
			appArmorSplit := strings.SplitN(annotation, "/", 2)
			if len(appArmorSplit) < 2 {
				continue
			}

			containerName := appArmorSplit[1]
			if containsContainerWithName(containerName, append(spec.Containers, spec.InitContainers...)...) {
				appArmorAnnotations[annotation] = value
			}
		}
	}
	return appArmorAnnotations
}

// IsAnnotationAppArmorPresent checks if one of the annotations is an AppArmor annotation
func IsAnnotationAppArmorPresent(spec *corev1.PodSpec, annotations map[string]string) bool {
	annotation := getAnnotationAppArmor(spec, annotations)
	return len(annotation) != 0
}

// IsAnnotationAppArmorPresentInObject checks if the AppArmor annotations are present or not in the given Object
func IsAnnotationAppArmorPresentInObject(
	object *metav1.ObjectMeta,
	spec *corev1.PodSpec,
	annotations map[string]string,
) bool {
	objAnnotations := getAnnotationAppArmor(spec, object.Annotations)
	appArmorAnnotations := getAnnotationAppArmor(spec, annotations)
	return reflect.DeepEqual(objAnnotations, appArmorAnnotations)
}

// AnnotateAppArmor adds an annotation to the pod
func AnnotateAppArmor(object *metav1.ObjectMeta, spec *corev1.PodSpec, annotations map[string]string) {
	if object.Annotations == nil {
		object.Annotations = make(map[string]string)
	}
	appArmorAnnotations := getAnnotationAppArmor(spec, annotations)
	for annotation, value := range appArmorAnnotations {
		object.Annotations[annotation] = value
	}
}

// IsReconciliationDisabled checks if the reconciliation loop is disabled on the given resource
func IsReconciliationDisabled(object *metav1.ObjectMeta) bool {
	return object.Annotations[resources.ReconciliationLoopAnnotationName] == string(annotationStatusDisabled)
}

// IsEmptyWalArchiveCheckEnabled returns a boolean indicating if we should run the logic that checks if the WAL archive
// storage is empty
func IsEmptyWalArchiveCheckEnabled(object *metav1.ObjectMeta) bool {
	return object.Annotations[resources.SkipEmptyWalArchiveCheck] != string(annotationStatusEnabled)
}

func mergeMap(receiver, giver map[string]string) map[string]string {
	for key, value := range giver {
		receiver[key] = value
	}
	return receiver
}

// MergeMap transfers the content of a giver map to a receiver
// ensure the receiver is not nil before call this method
func MergeMap(receiver, giver map[string]string) {
	_ = mergeMap(receiver, giver)
}

// GetInstanceRole tries to fetch the ClusterRoleLabelName andClusterInstanceRoleLabelName value from a given labels map
func GetInstanceRole(labels map[string]string) (string, bool) {
	if value := labels[resources.ClusterRoleLabelName]; value != "" {
		return value, true
	}
	if value := labels[resources.ClusterInstanceRoleLabelName]; value != "" {
		return value, true
	}

	return "", false
}

// SetInstanceRole sets both ClusterRoleLabelName and ClusterInstanceRoleLabelName on the given ObjectMeta
func SetInstanceRole(meta metav1.ObjectMeta, role string) {
	if meta.Labels == nil {
		meta.Labels = map[string]string{}
	}
	meta.Labels[resources.ClusterRoleLabelName] = role
	meta.Labels[resources.ClusterInstanceRoleLabelName] = role
}

// MergeObjectsMetadata is capable of merging the labels and annotations of two objects metadata
func MergeObjectsMetadata(receiver client.Object, giver client.Object) {
	if receiver.GetLabels() == nil {
		receiver.SetLabels(map[string]string{})
	}
	if receiver.GetAnnotations() == nil {
		receiver.SetAnnotations(map[string]string{})
	}

	receiver.SetLabels(mergeMap(receiver.GetLabels(), giver.GetLabels()))
	receiver.SetAnnotations(mergeMap(receiver.GetAnnotations(), giver.GetAnnotations()))
}
