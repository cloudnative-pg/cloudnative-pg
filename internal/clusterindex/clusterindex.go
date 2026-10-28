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


// Package clusterindex contains the field index that finds the resources
// referring to a Cluster, such as Database objects, in any namespace.
//
// The operator and the instance manager register the same index, so the
// lookup of the resources of a cluster has one definition.
package clusterindex

import (
	"context"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

// Key is the name of the field index holding the namespaced name of the
// cluster a resource refers to. The resources may refer to a cluster living in
// another namespace, so the name of the cluster alone would not identify it.
const Key = ".spec.cluster.namespacedName"

// clusterReferrer is a resource referring to a cluster, possibly living in
// another namespace
type clusterReferrer interface {
	GetClusterRef() apiv1.ClusterObjectReference
	GetClusterNamespace() string
}

// IndexByCluster extracts the value of the Key index from a resource, that is
// the namespaced name of the cluster it refers to
func IndexByCluster(rawObj client.Object) []string {
	referrer, ok := rawObj.(clusterReferrer)
	if !ok || referrer.GetClusterRef().Name == "" {
		return nil
	}

	return []string{Value(types.NamespacedName{
		Namespace: referrer.GetClusterNamespace(),
		Name:      referrer.GetClusterRef().Name,
	})}
}

// Value returns the value to look up in the Key index to get the resources
// referring to the cluster having the passed namespaced name
func Value(cluster types.NamespacedName) string {
	return cluster.String()
}

// MatchingCluster returns the list option selecting, through the Key index,
// the resources referring to the cluster having the passed namespaced name
func MatchingCluster(cluster types.NamespacedName) client.MatchingFields {
	return client.MatchingFields{Key: Value(cluster)}
}

// IndexedObjects returns an empty instance of every resource kind indexed by
// Key: the ones that can refer to a cluster in another namespace
func IndexedObjects() []client.Object {
	return []client.Object{
		&apiv1.Database{},
		&apiv1.DatabaseRole{},
		&apiv1.Publication{},
		&apiv1.Subscription{},
	}
}

// Register creates the Key index on every resource kind returned by
// IndexedObjects
func Register(ctx context.Context, indexer client.FieldIndexer) error {
	for _, obj := range IndexedObjects() {
		if err := indexer.IndexField(ctx, obj, Key, IndexByCluster); err != nil {
			return err
		}
	}

	return nil
}
