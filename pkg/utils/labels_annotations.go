/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package utils

import (
	"reflect"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/EnterpriseDB/cloud-native-postgresql/internal/configuration"
)

const (
	// ClusterLabelName is the name of cluster which the backup CR belongs to
	ClusterLabelName = "k8s.enterprisedb.io/cluster"

	// JobRoleLabelName is the name of the label containing the purpose of the executed job
	JobRoleLabelName = "k8s.enterprisedb.io/jobRole"

	// OperatorVersionAnnotationName is the name of the annotation containing
	// the version of the operator that generated a certain object
	OperatorVersionAnnotationName = "k8s.enterprisedb.io/operatorVersion"

	// AppArmorAnnotationPrefix will be the name of the AppArmor profile to apply
	// This is required for Azure but can be set in other environments
	AppArmorAnnotationPrefix = "container.apparmor.security.beta.kubernetes.io"

	// ReconciliationLoopAnnotationName is the name of the annotation controlling
	// the status of the reconciliation loop for the cluster
	ReconciliationLoopAnnotationName = "k8s.enterprisedb.io/reconciliationLoop"

	// ReconciliationDisabledValue it the value that stops the reconciliation loop
	ReconciliationDisabledValue = "disabled"
)

// LabelClusterName labels the object with the cluster name
func LabelClusterName(object *metav1.ObjectMeta, name string) {
	if object.Labels == nil {
		object.Labels = make(map[string]string)
	}

	object.Labels[ClusterLabelName] = name
}

// LabelJobRole labels a job with its role
func LabelJobRole(object *metav1.ObjectMeta, role string) {
	if object.Labels == nil {
		object.Labels = make(map[string]string)
	}
	object.Labels[JobRoleLabelName] = role
}

// SetOperatorVersion set inside a certain object metadata the annotation
// containing the version of the operator that generated the object
func SetOperatorVersion(object *metav1.ObjectMeta, version string) {
	if object.Annotations == nil {
		object.Annotations = make(map[string]string)
	}

	object.Annotations[OperatorVersionAnnotationName] = version
}

// InheritAnnotations puts into the object metadata the passed annotations if
// the annotations are supposed to be inherited. The passed configuration is
// used to determine whenever a certain annotation is inherited or not
func InheritAnnotations(
	object *metav1.ObjectMeta,
	annotations map[string]string,
	fixedAnnotations map[string]string,
	config *configuration.Data,
) {
	if object.Annotations == nil {
		object.Annotations = make(map[string]string)
	}

	for key, value := range fixedAnnotations {
		object.Annotations[key] = value
	}

	for key, value := range annotations {
		if config.IsAnnotationInherited(key) {
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
	config *configuration.Data,
) {
	if object.Labels == nil {
		object.Labels = make(map[string]string)
	}

	for key, value := range fixedLabels {
		object.Labels[key] = value
	}

	for key, value := range labels {
		if config.IsLabelInherited(key) {
			object.Labels[key] = value
		}
	}
}

func getAnnotationAppArmor(annotations map[string]string) map[string]string {
	appArmorAnnotations := make(map[string]string)
	for annotation, value := range annotations {
		if strings.HasPrefix(annotation, AppArmorAnnotationPrefix) {
			appArmorAnnotations[annotation] = value
		}
	}
	return appArmorAnnotations
}

// IsAnnotationAppArmorPresent checks if one of the annotations is an AppArmor annotation
func IsAnnotationAppArmorPresent(annotations map[string]string) bool {
	annotation := getAnnotationAppArmor(annotations)
	return len(annotation) != 0
}

// IsAnnotationAppArmorPresentInObject checks if the AppArmor annotations are present or not in the given Object
func IsAnnotationAppArmorPresentInObject(object *metav1.ObjectMeta, annotations map[string]string) bool {
	objAnnotations := getAnnotationAppArmor(object.Annotations)
	appArmorAnnotations := getAnnotationAppArmor(annotations)
	return reflect.DeepEqual(objAnnotations, appArmorAnnotations)
}

// AnnotateAppArmor adds an annotation to the pod
func AnnotateAppArmor(object *metav1.ObjectMeta, annotations map[string]string) {
	if object.Annotations == nil {
		object.Annotations = make(map[string]string)
	}
	appArmorAnnotations := getAnnotationAppArmor(annotations)
	for annotation, value := range appArmorAnnotations {
		object.Annotations[annotation] = value
	}
}

// IsReconciliationDisabled checks if the reconciliation loop is disabled on the given resource
func IsReconciliationDisabled(object *metav1.ObjectMeta) bool {
	return object.Annotations[ReconciliationLoopAnnotationName] == ReconciliationDisabledValue
}
