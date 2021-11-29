/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package utils

import (
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
func InheritAnnotations(object *metav1.ObjectMeta, annotations map[string]string, config *configuration.Data) {
	if object.Annotations == nil {
		object.Annotations = make(map[string]string)
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
func InheritLabels(object *metav1.ObjectMeta, labels map[string]string, config *configuration.Data) {
	if object.Labels == nil {
		object.Labels = make(map[string]string)
	}

	for key, value := range labels {
		if config.IsLabelInherited(key) {
			object.Labels[key] = value
		}
	}
}
