/*
Copyright 2019-2022 The CloudNativePG Contributors

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

// Package cache contains the constants and functions for reading/writing to the process local cache
// some specific supported objects
package cache

import (
	"sync"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
)

const (
	// ClusterKey is the key to be used to access the cached cluster
	ClusterKey = "cluster"
	// WALArchiveKey is the key to be used to access the cached envs for wal-archive
	WALArchiveKey = "wal-archive"
	// WALRestoreKey is the key to be used to access the cached envs for wal-restore
	WALRestoreKey = "wal-restore"
)

var cache sync.Map

// Store write an object into the local cache
func Store(c string, v interface{}) {
	cache.Store(c, v)
}

// Delete an object from the local cache
func Delete(c string) {
	cache.Delete(c)
}

// LoadEnv loads a key from the local cache
func LoadEnv(c string) ([]string, error) {
	value, ok := cache.Load(c)
	if !ok {
		return nil, ErrCacheMiss
	}

	if v, ok := value.([]string); ok {
		return v, nil
	}

	return nil, ErrUnsupportedObject
}

// LoadCluster loads a key from the local cache
func LoadCluster() (*apiv1.Cluster, error) {
	value, ok := cache.Load(ClusterKey)
	if !ok {
		return nil, ErrCacheMiss
	}

	if v, ok := value.(*apiv1.Cluster); ok {
		return v, nil
	}

	return nil, ErrUnsupportedObject
}
