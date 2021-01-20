/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package specs

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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
