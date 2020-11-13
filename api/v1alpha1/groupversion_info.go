/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

// Package v1alpha1 contains API Schema definitions for the postgresql v1alpha1 API group
// +kubebuilder:object:generate=true
// +groupName=postgresql.k8s.enterprisedb.io
package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (
	// GroupVersion is group version used to register these objects
	GroupVersion = schema.GroupVersion{Group: "postgresql.k8s.enterprisedb.io", Version: "v1alpha1"}

	// ClusterGVK is the triple to reach Cluster resources in k8s
	ClusterGVK = schema.GroupVersionResource{
		Group:    GroupVersion.Group,
		Version:  GroupVersion.Version,
		Resource: "clusters",
	}

	// ClusterKind is the kind name of Clusters
	ClusterKind = "Cluster"

	// SchemeBuilder is used to add go types to the GroupVersionKind scheme
	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}

	// AddToScheme adds the types in this group-version to the given scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)
