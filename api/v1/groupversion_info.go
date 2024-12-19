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

// Package v1 contains API Schema definitions for the postgresql v1 API group
// +kubebuilder:object:generate=true
// +groupName=postgresql.cnpg.io
package v1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (
	// GroupVersion is group version used to register these objects
	GroupVersion = schema.GroupVersion{Group: "postgresql.cnpg.io", Version: "v1"}

	// ClusterGVK is the triple to reach Cluster resources in k8s
	ClusterGVK = schema.GroupVersionResource{
		Group:    GroupVersion.Group,
		Version:  GroupVersion.Version,
		Resource: "clusters",
	}

	// BackupGVK is the triple to reach Backup resources in k8s
	BackupGVK = schema.GroupVersionResource{
		Group:    GroupVersion.Group,
		Version:  GroupVersion.Version,
		Resource: "backups",
	}

	// PoolerGVK is the triple to reach Pooler resources in k8s
	PoolerGVK = schema.GroupVersionResource{
		Group:    GroupVersion.Group,
		Version:  GroupVersion.Version,
		Resource: "poolers",
	}

	// ImageCatalogGVK is the triple to reach ImageCatalog resources in k8s
	ImageCatalogGVK = schema.GroupVersionResource{
		Group:    GroupVersion.Group,
		Version:  GroupVersion.Version,
		Resource: "imagecatalogs",
	}

	// ClusterImageCatalogGVK is the triple to reach ClusterImageCatalog resources in k8s
	ClusterImageCatalogGVK = schema.GroupVersionResource{
		Group:    GroupVersion.Group,
		Version:  GroupVersion.Version,
		Resource: "clusterimagecatalogs",
	}

	// PublicationGVK is the triple to reach Publication resources in k8s
	PublicationGVK = schema.GroupVersionResource{
		Group:    GroupVersion.Group,
		Version:  GroupVersion.Version,
		Resource: "publications",
	}

	// SubscriptionGVK is the triple to reach Subscription resources in k8s
	SubscriptionGVK = schema.GroupVersionResource{
		Group:    GroupVersion.Group,
		Version:  GroupVersion.Version,
		Resource: "subscriptions",
	}

	// DatabaseGVK is the triple to reach Database resources in k8s
	DatabaseGVK = schema.GroupVersionResource{
		Group:    GroupVersion.Group,
		Version:  GroupVersion.Version,
		Resource: "databases",
	}

	// ClusterKind is the kind name of Clusters
	ClusterKind = "Cluster"

	// BackupKind is the kind name of Backups
	BackupKind = "Backup"

	// PoolerKind is the kind name of Poolers
	PoolerKind = "Pooler"

	// ImageCatalogKind is the kind name of namespaced image catalogs
	ImageCatalogKind = "ImageCatalog"

	// ClusterImageCatalogKind is the kind name of the cluster-wide image catalogs
	ClusterImageCatalogKind = "ClusterImageCatalog"

	// PublicationKind is the kind name of publications
	PublicationKind = "Publication"

	// SubscriptionKind is the kind name of subscriptions
	SubscriptionKind = "Subscription"

	// DatabaseKind is the kind name of databases
	DatabaseKind = "Database"

	// SchemeBuilder is used to add go types to the GroupVersionKind scheme
	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}

	// AddToScheme adds the types in this group-version to the given scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)
