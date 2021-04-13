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
	// OperatorVersionAnnotationName is the name of the annotation containing
	// the version of the operator that generated a certain object
	OperatorVersionAnnotationName = "k8s.enterprisedb.io/operatorVersion"
)

// SetOperatorVersion set inside a a certain object metadata the annotation
// containing the version of the operator that generated the object
func SetOperatorVersion(object *metav1.ObjectMeta, version string) {
	if object.Annotations == nil {
		object.Annotations = make(map[string]string)
	}

	object.Annotations[OperatorVersionAnnotationName] = version
}

// InheritAnnotations put into the object metadata the passed annotations if
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
